"""离线检查部署清单和配置契约，不读取 local/ 或连接 Kubernetes API。"""

import argparse
import json
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT / ".tools"))
import yaml


class UniqueKeyLoader(yaml.SafeLoader):
    """拒绝重复 YAML 键，避免配置被后一个同名键静默覆盖。"""


def unique_mapping(loader, node, deep=False):
    """读取 YAML 映射并检查键唯一性，重复时报告源文件行号。"""
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in result:
            raise ValueError(f"Duplicate key {key} at line {key_node.start_mark.line + 1}")
        result[key] = loader.construct_object(value_node, deep=deep)
    return result


UniqueKeyLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping)


def load_documents(path):
    """按 UTF-8 解析 YAML 文档，忽略空文档，不执行任何配置内容。"""
    return [doc for doc in yaml.load_all(path.read_text(encoding="utf-8-sig"), Loader=UniqueKeyLoader) if doc]


def require(condition, message):
    """静态约束不满足时终止检查，保留可定位的错误原因。"""
    if not condition:
        raise ValueError(message)


def run_check(command, source=None):
    """运行离线解析子进程，失败时抛错；调用者只传入校验命令。"""
    result = subprocess.run(command, input=source, text=True, encoding="utf-8", capture_output=True)
    require(result.returncode == 0, result.stderr)
    return result.stdout


def main():
    """检查引用、调度、持久卷、配置契约和可选 shell 语法，输出检查数量。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kubectl", default="kubectl")
    parser.add_argument("--shell", help="可选的 bash 可执行文件路径，仅通过 -n 检查语法")
    parser.add_argument("--schema", type=Path, help="可选的官方 Kubernetes OpenAPI v2 JSON 文件，完全离线校验")
    args = parser.parse_args()
    customization = load_documents(ROOT / "kustomization.yaml")[0]
    files = [ROOT / item for item in customization["resources"]]
    documents = [doc for path in files for doc in load_documents(path)]
    identities = [(doc["kind"], doc["metadata"].get("namespace"), doc["metadata"]["name"]) for doc in documents]
    require(len(identities) == len(set(identities)), "Duplicate resource identity")
    rendered = list(yaml.load_all(run_check([args.kubectl, "kustomize", str(ROOT)]), Loader=UniqueKeyLoader))
    require(set(identities) == {(doc["kind"], doc["metadata"].get("namespace"), doc["metadata"]["name"]) for doc in rendered}, "Kustomize namespace/resource mismatch")
    if args.schema:
        from jsonschema import Draft4Validator

        schema = json.loads(args.schema.read_text(encoding="utf-8-sig"))
        definitions = schema["definitions"]
        kinds = {
            (gvk["group"], gvk["version"], gvk["kind"]): key
            for key, definition in definitions.items()
            for gvk in definition.get("x-kubernetes-group-version-kind", [])
        }
        for doc in rendered + load_documents(ROOT / "secrets.example.yaml"):
            group, version = doc["apiVersion"].split("/") if "/" in doc["apiVersion"] else ("", doc["apiVersion"])
            key = kinds[(group, version, doc["kind"])]
            Draft4Validator({"$ref": "#/definitions/" + key, "definitions": definitions}).validate(doc)
    secret_documents = load_documents(ROOT / "secrets.example.yaml")
    secrets = {doc["metadata"]["name"]: set(doc["stringData"]) for doc in secret_documents}
    secret_namespaces = {doc["metadata"]["name"]: doc["metadata"]["namespace"] for doc in secret_documents}
    secrets.update({"elasticsearch-tls": {"tls.key", "tls.crt", "ca.crt"}})
    secret_namespaces["elasticsearch-tls"] = "infrastructure"
    for name in ("vw-gateway", "vw-user", "vw-video"):
        secrets[name + "-config"] = {"config.yaml"}
        secret_namespaces[name + "-config"] = "videoweb"
    config_maps = {doc["metadata"]["name"] for doc in documents if doc["kind"] == "ConfigMap"}
    pvcs = {doc["metadata"]["name"] for doc in documents if doc["kind"] == "PersistentVolumeClaim"}
    workloads = [doc for doc in documents if doc["kind"] in ("Deployment", "StatefulSet", "Job")]
    scripts = []
    for doc in documents:
        name = doc["metadata"]["name"]
        if doc["kind"] not in ("Namespace", "StorageClass"):
            expected = "videoweb" if name.startswith("vw-") or name in ("videoweb-resources", "videoweb-internal") else "infrastructure"
            require(doc["metadata"].get("namespace") == expected, f"Namespace: {name}")
        if doc["kind"] == "ConfigMap":
            scripts.extend(value for key, value in doc["data"].items() if key.endswith(".sh"))
        if doc["kind"] == "Service":
            require(doc["spec"].get("type", "ClusterIP") == "ClusterIP", f"Unexpected public service: {name}")
            matches = [w for w in workloads if w["metadata"]["namespace"] == doc["metadata"]["namespace"] and all(w["spec"]["template"]["metadata"]["labels"].get(k) == v for k, v in doc["spec"]["selector"].items())]
            require(matches, f"Service has no workload: {name}")
            ports = {p["name"] for w in matches for c in w["spec"]["template"]["spec"]["containers"] for p in c.get("ports", [])}
            require(all(p["targetPort"] in ports for p in doc["spec"]["ports"]), f"Service target port: {name}")
        if doc["kind"] == "NetworkPolicy":
            require(doc["spec"]["podSelector"] == {"matchLabels": {"app.kubernetes.io/part-of": "videoweb"}}, f"Network policy affects unrelated pods: {name}")
            peers = [peer for rule in doc["spec"]["ingress"] for peer in rule["from"]]
            expected_peer = "infrastructure" if doc["metadata"]["namespace"] == "videoweb" else "videoweb"
            require(any(peer.get("namespaceSelector", {}).get("matchLabels", {}).get("kubernetes.io/metadata.name") == expected_peer and "podSelector" in peer for peer in peers), f"Missing cross-namespace access: {name}")
    for doc in workloads:
        name = doc["metadata"]["name"]
        spec = doc["spec"]
        pod = spec["template"]["spec"]
        require(pod.get("nodeSelector") == {"workload": "heavy"}, f"Worker placement: {name}")
        require(pod.get("automountServiceAccountToken") is False, f"Unnecessary API token: {name}")
        require(not pod.get("hostNetwork") and not pod.get("hostPID"), f"Host namespace: {name}")
        if name.startswith("vw-"):
            require(spec["replicas"] == 1 and spec["strategy"]["type"] == "Recreate", f"Fixed Consul ID: {name}")
            require(any(env.get("name") == "CONSUL_HTTP_ADDR" and env.get("value") == "http://consul.infrastructure.svc.cluster.local:8500" for env in pod["containers"][0]["env"]), f"Default Consul resolver: {name}")
        if doc["kind"] == "StatefulSet":
            require(("Service", doc["metadata"]["namespace"], spec["serviceName"]) in identities, f"Headless service: {name}")
            for claim in spec.get("volumeClaimTemplates", []):
                require(claim["spec"]["storageClassName"] == "videoweb-longhorn", f"StorageClass: {name}")
        volumes = {volume["name"]: volume for volume in pod.get("volumes", [])}
        mount_names = set(volumes) | {v["metadata"]["name"] for v in spec.get("volumeClaimTemplates", [])}
        for volume in volumes.values():
            require("hostPath" not in volume, f"Unexpected hostPath: {name}")
            if "secret" in volume:
                reference = volume["secret"]
                require(reference["secretName"] in secrets, f"Secret volume: {name}")
                require(secret_namespaces[reference["secretName"]] == doc["metadata"]["namespace"], f"Cross-namespace Secret volume: {name}")
                require(all(item["key"] in secrets[reference["secretName"]] for item in reference.get("items", [])), f"Secret item: {name}")
            if "configMap" in volume:
                require(volume["configMap"]["name"] in config_maps, f"ConfigMap volume: {name}")
            if "persistentVolumeClaim" in volume:
                require(volume["persistentVolumeClaim"]["claimName"] in pvcs, f"PVC reference: {name}")
        for container in pod["containers"] + pod.get("initContainers", []):
            require(":" in container["image"] and not container["image"].endswith(":latest"), f"Image tag: {name}")
            require(all(part in container["resources"] for part in ("requests", "limits")), f"Resource budget: {name}")
            require(not container.get("securityContext", {}).get("privileged"), f"Privileged container: {name}")
            require(all(mount["name"] in mount_names for mount in container.get("volumeMounts", [])), f"Volume mount: {name}")
            for env in container.get("env", []):
                reference = env.get("valueFrom", {}).get("secretKeyRef")
                if reference:
                    require(reference["name"] in secrets and reference["key"] in secrets[reference["name"]], f"Secret env: {name}/{env['name']}")
                    require(secret_namespaces[reference["name"]] == doc["metadata"]["namespace"], f"Cross-namespace Secret env: {name}")
            command = container.get("command", [])
            if command[:2] == ["sh", "-ec"]:
                scripts.append(command[2])
            for key in ("startupProbe", "readinessProbe", "livenessProbe"):
                command = container.get(key, {}).get("exec", {}).get("command", [])
                if command[:2] == ["sh", "-ec"]:
                    scripts.append(command[2])
    configs = {name: load_documents(ROOT / "config" / f"{name}.example.yaml")[0] for name in ("vw_gateway", "vw_user", "vw_video")}
    for name, config in configs.items():
        redis = config["data"]["redis_cluster"]
        require(redis["host"] == "redis-0.redis-headless.infrastructure.svc.cluster.local" and redis["port"] == ["6379"], f"Redis client contract: {name}")
        require(config["registry"]["consul"]["address"] == "consul.infrastructure.svc.cluster.local:8500", f"Consul config: {name}")
        require(config["trace"]["endpoint"] == "http://jaeger.infrastructure.svc.cluster.local:14268/api/traces", f"Tracing config: {name}")
    require("dtm_config" in configs["vw_gateway"]["dtm"], "Nested DTM protobuf config")
    require(configs["vw_gateway"]["dtm"]["dtm_config"]["target"] == "discovery:///dtm", "Registered DTM service name")
    require(configs["vw_user"]["data"]["mongo"]["host"].startswith("videoweb:CHANGE_ME_MONGO_APP@"), "Existing user Mongo URI contract")
    require(configs["vw_video"]["data"]["mongo"]["username"] == "videoweb", "Video Mongo credentials")
    if args.shell:
        for script in scripts:
            run_check([args.shell, "-n"], script)
    print(f"Passed: {len(files)} manifests, {len(documents)} resources, {len(workloads)} workloads, {len(configs)} app configs, {len(scripts)} shell fragments. No cluster API calls.")


if __name__ == "__main__":
    main()
