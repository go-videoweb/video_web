"""验证学习环境的临时存储约束，不调用 kubectl 或连接外部服务。"""

import copy
import importlib.util
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("manifest_checks", ROOT / "Test-Manifests.py")
CHECKS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKS)


class EphemeralStorageTests(unittest.TestCase):
    """从真实清单构造回归场景，避免仅检查不存在业务约束的空模板。"""

    def setUp(self):
        """读取本地 MySQL 和网关清单，不执行其中的命令。"""
        self.mysql = next(doc for doc in CHECKS.load_documents(ROOT / "mysql.yaml") if doc["kind"] == "StatefulSet")
        self.gateway = next(doc for doc in CHECKS.load_documents(ROOT / "vw-gateway.yaml") if doc["kind"] == "Deployment")

    def test_current_manifests_use_ephemeral_storage(self):
        """当前所有清单满足约束，包括数据目录挂载和 Secret/ConfigMap。"""
        resources = CHECKS.load_documents(ROOT / "kustomization.yaml")[0]["resources"]
        for resource in resources:
            for document in CHECKS.load_documents(ROOT / resource):
                with self.subTest(resource=resource, name=document["metadata"]["name"]):
                    CHECKS.validate_ephemeral_storage(document)

    def test_rejects_persistent_storage_resources(self):
        """拒绝在聚合清单中重新引入 StorageClass、PV 或 PVC。"""
        for kind in ("StorageClass", "PersistentVolume", "PersistentVolumeClaim"):
            with self.subTest(kind=kind), self.assertRaisesRegex(ValueError, "Persistent storage resource"):
                CHECKS.validate_ephemeral_storage({"kind": kind, "metadata": {"name": "old-storage"}})

    def test_rejects_volume_claim_templates(self):
        """拒绝 StatefulSet 重新创建声明模板，即使模板列表为空。"""
        self.mysql["spec"]["volumeClaimTemplates"] = []
        with self.assertRaisesRegex(ValueError, "Persistent claim templates"):
            CHECKS.validate_ephemeral_storage(self.mysql)

    def test_rejects_pvc_hostpath_and_csi_sources(self):
        """拒绝 PVC、hostPath 和 CSI，不能绕过 Pod 局部存储边界。"""
        for source in ("persistentVolumeClaim", "hostPath", "csi"):
            document = copy.deepcopy(self.mysql)
            document["spec"]["template"]["spec"]["volumes"] = [{"name": "data", source: {}}]
            with self.subTest(source=source), self.assertRaisesRegex(ValueError, "Non-ephemeral volume"):
                CHECKS.validate_ephemeral_storage(document)

    def test_rejects_memory_backed_data(self):
        """数据库卷必须使用磁盘型 emptyDir，不能意外改为占用内存的 tmpfs。"""
        volumes = self.mysql["spec"]["template"]["spec"]["volumes"]
        next(volume for volume in volumes if volume["name"] == "data")["emptyDir"]["medium"] = "Memory"
        with self.assertRaisesRegex(ValueError, "Memory-backed volume"):
            CHECKS.validate_ephemeral_storage(self.mysql)

    def test_rejects_missing_database_volume(self):
        """数据卷缺失时失败，避免数据库静默写入容器可写层。"""
        pod = self.mysql["spec"]["template"]["spec"]
        pod["volumes"] = [volume for volume in pod["volumes"] if volume["name"] != "data"]
        with self.assertRaisesRegex(ValueError, "Missing data emptyDir"):
            CHECKS.validate_ephemeral_storage(self.mysql)

    def test_rejects_missing_media_volume(self):
        """网关资源目录也必须显式使用本 Pod 的 emptyDir。"""
        pod = self.gateway["spec"]["template"]["spec"]
        pod["volumes"] = [volume for volume in pod["volumes"] if volume["name"] != "resources"]
        with self.assertRaisesRegex(ValueError, "Missing data emptyDir"):
            CHECKS.validate_ephemeral_storage(self.gateway)

    def test_rejects_unmounted_data_volume(self):
        """只声明而不挂载数据卷不能算完成存储切换。"""
        container = self.mysql["spec"]["template"]["spec"]["containers"][0]
        container["volumeMounts"] = [mount for mount in container["volumeMounts"] if mount["name"] != "data"]
        with self.assertRaisesRegex(ValueError, "Unmounted data emptyDir"):
            CHECKS.validate_ephemeral_storage(self.mysql)

    def test_rejects_multiple_replicas(self):
        """单节点方案拒绝增加 StatefulSet 或业务 Deployment 副本。"""
        for document in (self.mysql, self.gateway):
            document["spec"]["replicas"] = 2
            with self.subTest(kind=document["kind"]), self.assertRaisesRegex(ValueError, "Single-node replicas"):
                CHECKS.validate_ephemeral_storage(document)

    def test_single_node_protocol_contracts(self):
        """Redis 保留 Cluster 协议且只分配一个节点，MongoDB 保留单成员 rs0。"""
        redis = next(doc for doc in CHECKS.load_documents(ROOT / "redis.yaml") if doc["kind"] == "StatefulSet")
        command = redis["spec"]["template"]["spec"]["containers"][0]["command"][2]
        self.assertIn("--cluster-enabled yes", command)
        init = CHECKS.load_documents(ROOT / "redis-init.yaml")[0]
        self.assertEqual(init["metadata"]["name"], "redis-single-init")
        script = init["spec"]["template"]["spec"]["containers"][0]["command"][2]
        self.assertIn("CLUSTER ADDSLOTSRANGE 0 16383", script)
        self.assertIn("cluster_known_nodes:1", script)
        self.assertNotIn("--cluster create", script)
        self.assertNotIn("CLUSTER RESET", script.upper())
        mongodb = next(doc for doc in CHECKS.load_documents(ROOT / "mongodb.yaml") if doc["kind"] == "StatefulSet")
        args = mongodb["spec"]["template"]["spec"]["containers"][0]["args"]
        self.assertEqual(args[args.index("--replSet") + 1], "rs0")
        mongo_init = CHECKS.load_documents(ROOT / "mongodb-init.yaml")[0]
        self.assertEqual(mongo_init["metadata"]["name"], "mongodb-single-rs-init")


if __name__ == "__main__":
    unittest.main()
