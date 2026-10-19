# 单节点学习环境

这是一套独立的 Kubernetes/k3s 清单，各服务一个 Pod、数据使用磁盘型 `emptyDir`，不使用 Longhorn、StorageClass、PVC 或 hostPath。父目录的原 Redis 三主三从及 Longhorn 清单完整保留，镜像构建文件继续共用父目录的 `images/`，不会覆盖原方案。

**两套方案使用相同的 `infrastructure`、`videoweb` 命名空间及服务名，是替代方案，不能同时应用。** 不要递归 apply 父目录；预览和部署时明确选择本目录。

以下命令均从 `video_web_backend/` 执行。本次只生成和离线校验配置，不连接集群、启动服务、构建镜像、迁移数据或删除旧资源。

## 服务与协议

| 文件 | 节点数 | 命名空间与用途 |
| --- | --- | --- |
| `mysql.yaml` | 1 | infrastructure，业务数据库 |
| `redis.yaml`、`redis-init.yaml` | 1，无副本 | infrastructure，单个节点承载全部槽 |
| `mongodb.yaml`、`mongodb-init.yaml` | 1 | infrastructure，单成员 rs0 |
| `consul.yaml` | 1 | infrastructure，服务发现，未配置 ACL/TLS |
| `dtm.yaml` | 1 | infrastructure，BoltDB 事务协调 |
| `jaeger.yaml` | 1 | infrastructure，内存追踪数据 |
| `rabbitmq.yaml` | 1 | infrastructure，消息队列及管理界面 |
| `elasticsearch.yaml` | 1 | infrastructure，ES 认证及 TLS |
| `kibana.yaml` | 1 | infrastructure，ES 管理界面 |
| `vw-user.yaml`、`vw-video.yaml`、`vw-gateway.yaml` | 各 1 | videoweb，业务服务 |
| `00-namespace.yaml`、`03-network-policy.yaml` | 不适用 | 命名空间和项目 Pod 入站策略 |

Redis **不是 standalone 协议**：现有 Go 应用使用 `redis.ClusterClient`，因此保留 `--cluster-enabled yes`，仅启动 redis-0，并由 `redis-single-init` 将 0 到 16383 全部槽分配给它。没有其他 Redis 成员、没有主从复制、没有自动故障转移；不是将原六节点集群直接缩容到一节点，也不自动迁移旧槽和数据。初始化只接受单成员的全空槽或完整槽状态，发现旧多成员/部分槽状态即报错，不执行 reset。

MongoDB 保留单成员 rs0 和 keyfile 认证，因为现有事务需要副本集语义；不创建额外副本，不进行跨节点复制。不要删除 `--replSet rs0` 或改成 standalone。其他基础设施在原 k8s 方案中也已是单实例，本方案继续保持一个 Pod。`async_service` 尚未完成，不创建其 Deployment，也不新增独立评论服务。

## 临时数据边界

`emptyDir` 在同一 Pod 内的容器重启后仍可保留，但 Pod 被删除、滚动更新、驱逐或重调度后数据丢失。StatefulSet 相同的名称不代表相同的卷。Secret/ConfigMap 不随数据 Pod 重建而自动消失。

各 Pod 的 `/workspace/resources` 不共享，即使它们在同一节点。用户服务存放头像，视频服务存放视频/封面/DASH，网关通过现有流式 RPC 将下载内容落到自己的临时目录；不新增静态文件服务。媒体 Pod 独立重建后，数据库可能仍引用已丢失的文件。默认头像由各业务 Pod 的 initContainer 从镜像补齐。

数据卷上限：MySQL/MongoDB/ES 各 10 GiB，Redis/Consul/DTM 各 1 GiB，RabbitMQ 2 GiB；网关 resources 5 GiB、用户 resources 2 GiB、视频 resources 20 GiB，各业务 tmp 1 GiB。默认磁盘介质，不使用 `medium: Memory`。sizeLimit 不预留磁盘，也不是调度器的 ephemeral-storage requests；节点磁盘压力仍可能触发驱逐，需监控实际 kubelet 数据目录所在文件系统的余量。

常驻容器 requests 合计 CPU 1450m、内存 3136 MiB，memory limits 合计 7808 MiB，不含初始化 Job、系统组件及其他项目。所有工作负载仍调度到 `workload=heavy` 工作节点，无需把全部服务放到同一个物理节点。单节点减少了副本开销，但不提供高可用，也不保证消除网络或磁盘超时。

## 切换旧环境

若已有原方案运行，先停止业务写入并备份需要保留的数据库、媒体、消息及 DTM 状态，检查目标 context、同名资源及旧 StatefulSet 的 PVC 保留策略。父目录和本目录的资源不能混用。

从 `volumeClaimTemplates` 切到 emptyDir 通常不能原地更新 StatefulSet。由操作者有选择地停止并重建本项目的 MySQL、Redis、MongoDB、Consul、DTM、RabbitMQ、ES StatefulSet，再应用本目录文件；Redis 旧六节点数据不能靠改 replicas=1 自动转换。不要使用整命名空间删除、`replace --force` 或 prune 来批量迁移。

原 PVC/PV、StorageClass 和 Longhorn 不会因为新清单不再引用而自动删除，本次也没有清理它们。确认备份和新环境后另行决定旧资源的清理范围，不影响其他项目。切换后需重新初始化、导入 schema/演示数据、设置 Kibana 账号及导入所需媒体。

## 准备镜像和凭据

先检查目标 context、工作节点架构/磁盘和已有同名资源。业务镜像继续使用 [../images/Dockerfile](../images/Dockerfile)，构建上下文是后端根目录；构建和导入方式见 [父目录镜像说明](../README.md#构建与分发应用镜像)。三个镜像为 `videoweb/vw_user:k3s`、`videoweb/vw_video:k3s`、`videoweb/vw_gateway:k3s`，须在两个工作节点的 k3s containerd 中均可用，或改为可拉取的仓库地址。不要使用服务目录下的 Go 1.19 历史模板，也不要加 `-trimpath`。

在本目录被忽略的 `local/` 中准备：

- 从 `secrets.example.yaml` 准备 `local/secrets.yaml`，替换所有凭据占位符。
- 从 `config/*.example.yaml` 准备 `local/vw_user.yaml`、`local/vw_video.yaml`、`local/vw_gateway.yaml`，替换全部 CHANGE_ME，填写最终 JWT、SMTP 与数据库密码。
- 准备 `local/tls.key`、`local/tls.crt`、`local/ca.crt`，不要提交私钥。

配置中的基础设施地址均为 `*.infrastructure.svc.cluster.local`。MySQL 应用密码仅用字母、数字、下划线或连字符；MongoDB 密码推荐独立随机十六进制字符串，用户服务 mongo.host 内的密码和视频服务 username/password 必须与 mongodb-auth 一致。Kratos 不会对 YAML 做环境变量替换。

MongoDB keyfile 使用 `openssl rand -base64 756` 的完整输出，不是登录密码。ES 证书由同一 CA 签发，使用未加密私钥，支持服务端及节点间 TLS；SAN 包含 localhost、elasticsearch、elasticsearch.infrastructure.svc、elasticsearch.infrastructure.svc.cluster.local 和 elasticsearch-0.elasticsearch-headless.infrastructure.svc.cluster.local。不关闭证书校验；更详细的凭据约束见父目录说明。

由操作者在确认目标集群后创建真实 Secret，不应用示例文件：

```sh
kubectl apply -f deploy/k8s/single-node/00-namespace.yaml
kubectl apply -f deploy/k8s/single-node/local/secrets.yaml
kubectl -n infrastructure create secret generic elasticsearch-tls --from-file=tls.key=deploy/k8s/single-node/local/tls.key --from-file=tls.crt=deploy/k8s/single-node/local/tls.crt --from-file=ca.crt=deploy/k8s/single-node/local/ca.crt
kubectl -n videoweb create secret generic vw-user-config --from-file=config.yaml=deploy/k8s/single-node/local/vw_user.yaml
kubectl -n videoweb create secret generic vw-video-config --from-file=config.yaml=deploy/k8s/single-node/local/vw_video.yaml
kubectl -n videoweb create secret generic vw-gateway-config --from-file=config.yaml=deploy/k8s/single-node/local/vw_gateway.yaml
```

create 仅适用于首次创建，已有资源先核对归属和凭据，不盲目删除重建。更新 Secret 不会自动轮换已有账号密码或重新加载应用配置。

## 首次启动

先准备基础设施：

```sh
kubectl apply -f deploy/k8s/single-node/03-network-policy.yaml
kubectl apply -f deploy/k8s/single-node/consul.yaml
kubectl apply -f deploy/k8s/single-node/jaeger.yaml
kubectl apply -f deploy/k8s/single-node/dtm.yaml
kubectl apply -f deploy/k8s/single-node/mysql.yaml
kubectl apply -f deploy/k8s/single-node/redis.yaml
kubectl apply -f deploy/k8s/single-node/mongodb.yaml
kubectl apply -f deploy/k8s/single-node/rabbitmq.yaml
kubectl apply -f deploy/k8s/single-node/redis-init.yaml
kubectl apply -f deploy/k8s/single-node/mongodb-init.yaml
kubectl -n infrastructure wait --for=condition=complete job/redis-single-init --timeout=900s
kubectl -n infrastructure wait --for=condition=complete job/mongodb-single-rs-init --timeout=900s
kubectl -n infrastructure rollout status statefulset/mysql --timeout=600s
kubectl -n infrastructure rollout status statefulset/mongodb --timeout=600s
kubectl -n infrastructure rollout status statefulset/consul --timeout=600s
kubectl -n infrastructure rollout status statefulset/dtm --timeout=600s
```

MongoDB 成为 primary 后才 Ready，必须先运行初始化 Job，不能先等 Ready 再初始化。Redis PING 不代表槽已就绪，必须等待 Job 完成。两个 Job 使用新名称，避免直接沿用原方案已完成的初始化记录；它们不会破坏旧集群，遇到非预期成员会失败并等待人工处理。

MySQL 仅创建 vw_user、vw_video、dtm_barrier 数据库和应用账号，不创建业务表。启动应用前，导入审查过的业务 schema、种子数据及当前 SDK 所需的 dtm_barrier.barrier 表；不自动运行 GORM Gen、AutoMigrate 或实验测试。

然后启动 ES，交互设置 kibana_system 密码，与 Secret 中 kibana-password 保持一致，再启动 Kibana：

```sh
kubectl apply -f deploy/k8s/single-node/elasticsearch.yaml
kubectl -n infrastructure rollout status statefulset/elasticsearch --timeout=900s
kubectl -n infrastructure exec -it elasticsearch-0 -- bin/elasticsearch-reset-password -u kibana_system -i
kubectl apply -f deploy/k8s/single-node/kibana.yaml
kubectl -n infrastructure rollout status deployment/kibana --timeout=600s
```

ES 关闭 mmap 以避免自动改主机 sysctl；允许 yellow 不代表存在可用副本。正式对外提供服务前另行评估固定镜像版本的安全支持、容量和升级兼容性。

确认基础设施、schema 和镜像均已准备，再依次启动业务：

```sh
kubectl apply -f deploy/k8s/single-node/vw-user.yaml
kubectl -n videoweb rollout status deployment/vw-user --timeout=600s
kubectl apply -f deploy/k8s/single-node/vw-video.yaml
kubectl -n videoweb rollout status deployment/vw-video --timeout=600s
kubectl apply -f deploy/k8s/single-node/vw-gateway.yaml
kubectl -n videoweb rollout status deployment/vw-gateway --timeout=600s
kubectl -n videoweb port-forward service/vw-gateway 8001:8001
```

应用固定 Consul ID，保持单副本 Recreate；DTM 通过 discovery:///dtm 发现，CONSUL_HTTP_ADDR 指向 infrastructure 中的 Consul。网络策略放行业务到基础设施，以及 Consul 健康检查和 DTM 回调；所有 Service 为 ClusterIP，默认不对外暴露管理界面。TCP Ready 不等于业务成功，需要另行联调注册登录、上传、播放和 Saga。

## Pod 重建恢复

镜像升级或 rollout restart 会重建 Pod 并丢失对应数据。数据库记录、媒体和事务状态可能不再一致，不能承诺自动恢复。

- MySQL：重新导入业务 schema、barrier 表和演示数据。
- Redis/MongoDB：新空数据 Pod 需要重新分配槽或初始化 rs0，已完成的 Job 不会自动重跑。先保留日志并确认节点全新且无其他成员，再有选择地删除对应 Job 对象并重新应用本目录的 init 文件；不删除数据 Pod、命名空间或 PVC 代替修复。
- Consul：检查用户、视频及 DTM 注册，必要时依次重启业务服务，同时接受业务 Pod 媒体数据丢失。
- ES/Kibana：恢复所需索引和对象，重新设置 kibana_system 密码。RabbitMQ 消息/队列、DTM 未完成事务也会随各自 Pod 重建丢失。
- 用户/视频：按需重新导入对应媒体文件，或重建匹配的演示数据，不能把缺失媒体当作接口完整可用。

仅重复 apply -k 不会重跑完成的 Job，也不会恢复数据。部分旧数据/多成员状态需要人工检查，不执行 reset、reconfig 或自动清库。

## 离线校验

```sh
kubectl kustomize deploy/k8s/single-node
python deploy/k8s/single-node/Test-Manifests.py
python deploy/k8s/single-node/test_manifests.py
```

脚本需要 Python、kubectl 和 PyYAML；优先读取本目录 `.tools/`，也可复用父目录 `.tools/` 中已安装的 PyYAML/jsonschema。依赖安装方法见父目录。可选增加 `--schema deploy/k8s/.tools/kubernetes-1.35-openapi.json` 和 `--shell /path/to/bash`，只执行 OpenAPI 校验和 bash -n，不访问集群或运行脚本内容。

2026-10-19 离线验证覆盖 16 个清单、39 个资源、14 个工作负载、3 份配置、15 段 shell；包括 Kustomize、官方 v1.35 OpenAPI、shell 语法和 10 项存储/单节点约束测试。原方案仍单独通过 18 个清单、41 个资源的离线检查。没有验证真实 API 准入、单节点 Redis 实际运行、数据恢复、镜像构建、证书、节点磁盘和完整业务联调。

## 参考

- [Kubernetes emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir)
- [Kubernetes StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
- [Redis CLUSTER ADDSLOTSRANGE](https://redis.io/docs/latest/commands/cluster-addslotsrange/)
