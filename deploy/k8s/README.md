# VideoWeb 的 k3s 部署配置

本目录按服务拆分 Kubernetes 清单，面向 `../k3s-cluster-info.md` 描述的三节点 k3s 集群。基础设施放在 `infrastructure`，业务服务放在 `videoweb`。它是资源受限的开发部署基线，不是生产高可用方案；未执行集群部署、镜像构建、数据迁移或业务联调。

以下命令均从 `video_web_backend/` 执行。先逐节完成准备，再选择应用文件，不要直接整目录递归 apply。

## 命名空间与文件

| 文件 | 命名空间 | 内容 |
| --- | --- | --- |
| `00-namespace.yaml` | 集群级 | 两个命名空间 |
| `01-storage-class.yaml` | 集群级 | 专用 Longhorn StorageClass，两个数据副本、Retain |
| `02-shared-resources.yaml` | `videoweb` | 三个业务服务共用的 30 GiB RWX 媒体卷 |
| `03-network-policy.yaml` | 两者 | 仅选择本项目 Pod 的入站规则 |
| `mysql.yaml` | `infrastructure` | 单实例 MySQL、项目库和账号初始化 |
| `redis.yaml`、`redis-init.yaml` | `infrastructure` | 六个 Redis Pod，三个主分片、三个副本，首次建集群 Job |
| `mongodb.yaml`、`mongodb-init.yaml` | `infrastructure` | 带认证的单成员 rs0、副本集初始化 Job |
| `consul.yaml` | `infrastructure` | 单实例服务发现，开发模式未配置 ACL/TLS |
| `dtm.yaml` | `infrastructure` | 单实例 DTM、持久化 BoltDB |
| `jaeger.yaml` | `infrastructure` | 内存追踪存储，重启丢失追踪数据 |
| `rabbitmq.yaml` | `infrastructure` | 单实例 RabbitMQ、管理界面和持久卷 |
| `elasticsearch.yaml` | `infrastructure` | 单节点 ES 8.17.0、认证和 TLS |
| `kibana.yaml` | `infrastructure` | Kibana 8.17.0 |
| `vw-user.yaml` | `videoweb` | 用户服务、gRPC 9001 |
| `vw-video.yaml` | `videoweb` | 视频和评论服务、gRPC 9002 |
| `vw-gateway.yaml` | `videoweb` | 网关、HTTP 8001 和 gRPC 9000 |
| `config/*.example.yaml` | 不直接应用 | 三个应用配置 Secret 的源模板 |
| `secrets.example.yaml` | 不直接应用 | 基础设施凭据模板 |
| `images/Dockerfile` | 不直接应用 | 支持三个 Go 服务的多阶段构建 |
| `kustomization.yaml` | 两者 | 完整清单聚合，不包含真实 Secret |
| `Test-Manifests.py` | 本地 | YAML、Kustomize、引用和配置契约的离线检查 |

`local/`、`.tools/` 和 `.rendered*` 已在本目录忽略。真实密码、私钥和展开的 Secret 只放 `local/`，不要提交。Namespace 不是强安全边界，Kubernetes Secret 也不是天然加密的保险箱；实际共享集群还需 RBAC、Secret 静态加密、备份访问控制等管理措施。

基础设施放在公共命名空间，并不意味着可以直接覆盖已有服务。本套默认创建项目使用的实例，不自动接管集群现有 MySQL、Milvus、MinIO 等。若已有同名 Service、StatefulSet、Secret 或 PVC，先决定复用或改名，停止应用对应文件。复用时同时调整应用配置中的地址、账号、数据库和网络策略，不执行本套首次初始化 Job；不同命名空间无法直接引用对方的 Secret 或 PVC。

`async_service` 的 Wire 和消费骨架尚未完成，因此不创建其 Deployment；评论在 `vw_video` 中，不创建独立评论服务。部署 RabbitMQ 和 ES 不代表索引同步或搜索接口已实现。

## 节点、存储与容量

所有工作负载使用 `nodeSelector: workload=heavy`，对应快照中的 `home-worker` 和 `k3s-worker-02`；不在资源较紧张的 `zey-tencentcloud01` 控制节点上运行业务，也不修改节点标签或污点。Redis 使用软反亲和分散 Pod，但两个物理节点不能提供三个独立故障域，不保证任一工作节点离线后集群仍可写。

常驻容器合计 requests 为 CPU 1700m、内存 3776 MiB，memory limits 合计 9088 MiB；不包含初始化 Job、Longhorn、系统组件和集群已有业务的额外消耗。视频转码可突发占用 CPU 和内存，512 MiB ES 堆、Redis 每节点 128 MiB maxmemory 等配置只适用于小规模开发。应以实时 Allocatable、实际用量和磁盘余量判断能否部署，而不是仅看节点总内存。

持久卷逻辑容量共 70 GiB，两个 Longhorn 副本约需 140 GiB 原始空间，另加快照、备份、文件系统和余量。RWX 媒体卷依赖 Longhorn share-manager/NFS；核对两个工作节点的 NFS 客户端、iSCSI、存储磁盘和集群已有 multipath 设置。不要通过本清单自动调整主机服务。

StorageClass 的副本数不等于 MySQL/MongoDB/ES/RabbitMQ 服务副本数；这些仍是单实例，节点故障会导致服务中断并等待重调度。Pod 的 nodeSelector 也不会约束 Longhorn 存储副本所在节点；需在 Longhorn 中确认至少两个独立、可调度且容量足够的存储节点及磁盘。清单不改变 Longhorn 全局默认设置。

部署前由操作者检查目标 context、资源名称和存储能力：

```sh
kubectl config current-context
kubectl get nodes -L workload,nodepool -o wide
kubectl get nodes -o custom-columns=NAME:.metadata.name,ARCH:.status.nodeInfo.architecture
kubectl top nodes
kubectl get storageclass
kubectl -n longhorn-system get pods
kubectl -n infrastructure get statefulset,deployment,service,secret,pvc
kubectl -n videoweb get statefulset,deployment,service,secret,pvc
```

命名空间尚不存在时最后两条报 NotFound 是正常的。还需核对 Longhorn 版本对本 StorageClass 参数的支持、卷可用空间及快照策略；节点快照不能证明当前仍满足要求。

## 构建与分发应用镜像

不要使用三个服务目录下的历史 Dockerfile。新 Dockerfile 从后端根目录复制兄弟模块以满足本地 `replace`，使用 Go 1.23.12，运行镜像包含 ffmpeg/ffprobe。保留编译时 `/workspace` 源码路径，不加 `-trimpath`，以适配现有 `runtime.Caller` 资源定位；媒体卷挂载到 `/workspace/resources`。独立 Dockerfile.dockerignore 排除本机配置、资源视频和已知未交付草稿，不修改原根忽略规则。

先核对节点架构。以下以所有工作节点均为 amd64 为例：

```sh
docker build --platform linux/amd64 -f deploy/k8s/images/Dockerfile --build-arg SERVICE=vw_user -t videoweb/vw_user:k3s .
docker build --platform linux/amd64 -f deploy/k8s/images/Dockerfile --build-arg SERVICE=vw_video -t videoweb/vw_video:k3s .
docker build --platform linux/amd64 -f deploy/k8s/images/Dockerfile --build-arg SERVICE=vw_gateway -t videoweb/vw_gateway:k3s .
docker save -o videoweb-images.tar videoweb/vw_user:k3s videoweb/vw_video:k3s videoweb/vw_gateway:k3s
```

将归档传到两个工作节点，分别执行 `sudo k3s ctr images import videoweb-images.tar`。本机 Docker 镜像不会自动出现在 k3s containerd 中。另一种方式是推送到可访问的私有仓库，同时修改 Deployment 的主容器和 initContainer 镜像，或通过 Kustomize images 统一替换；私有仓库凭据位于 `videoweb`。固定的 `k3s` 标签仅用于首次开发，升级应使用新的唯一标签或 digest，避免 IfNotPresent 继续使用旧镜像。

业务媒体卷首次启动仅补齐缺失的默认头像和 tmp 目录，不迁移原资源。历史媒体需停写、备份并保持目录结构另行导入；确认 uid/gid 1000 可读写。当前没有验证镜像构建、目标架构执行或实际媒体权限。

## 准备凭据和配置

在被忽略的 `deploy/k8s/local/` 中准备以下文件，所有 `CHANGE_ME_*` 必须替换：

- 从 `secrets.example.yaml` 准备 `local/secrets.yaml`。
- 从三个 `config/*.example.yaml` 准备 `local/vw_user.yaml`、`local/vw_video.yaml`、`local/vw_gateway.yaml`。
- 准备 ES 证书 `local/tls.key`、`local/tls.crt`、`local/ca.crt`。

MySQL app-password 仅允许字母、数字、下划线和连字符；应用数据库名固定为 `vw_user`、`vw_video`，barrier 库为 `dtm_barrier`。推荐各密码使用独立随机十六进制字符串。MongoDB app-password 尤其需要 URI 安全字符：当前用户服务只拼接 host/port，模板将 `videoweb:密码@主机` 放在 mongo.host，不能只修改未被代码使用的 password 字段。视频服务则读取 username/password，两份配置必须与 mongodb-auth 中的账号密码一致。MongoDB app 用户位于 admin，授权 video_web 和 dtm_barrier。

MongoDB keyfile 使用 `openssl rand -base64 756` 生成，填为完整字符串或 YAML 多行块，不是管理员密码；RabbitMQ cookie 也应独立随机生成。网关还需真实 JWT secret 和 SMTP 参数。Kratos 不会替换 YAML 中的环境变量占位符，应用配置 Secret 中必须是最终值。

ES 使用同一 CA 签发、未加密私钥的开发证书，证书同时适用于服务端和节点间 TLS，SAN 至少包含 `localhost`、`elasticsearch`、`elasticsearch.infrastructure.svc`、`elasticsearch.infrastructure.svc.cluster.local` 和 `elasticsearch-0.elasticsearch-headless.infrastructure.svc.cluster.local`。不关闭证书校验；未来扩容需重新设计节点证书与发现配置，不能只增加 replicas。

下列创建操作由操作者在确认目标集群后执行，不应用示例 Secret：

```sh
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/local/secrets.yaml
kubectl -n infrastructure create secret generic elasticsearch-tls --from-file=tls.key=deploy/k8s/local/tls.key --from-file=tls.crt=deploy/k8s/local/tls.crt --from-file=ca.crt=deploy/k8s/local/ca.crt
kubectl -n videoweb create secret generic vw-user-config --from-file=config.yaml=deploy/k8s/local/vw_user.yaml
kubectl -n videoweb create secret generic vw-video-config --from-file=config.yaml=deploy/k8s/local/vw_video.yaml
kubectl -n videoweb create secret generic vw-gateway-config --from-file=config.yaml=deploy/k8s/local/vw_gateway.yaml
```

这些 create 命令用于首次创建，已有 Secret 时不要盲目删除重建；先核对现有数据和凭据轮换方案。Secret 更新不等于数据库账号更新，环境变量及 Kratos 配置也不会自动重新加载。

## 首次部署顺序

### 1. 公共资源与基础设施

StorageClass 需要集群级权限。确认没有同名资源冲突后执行：

```sh
kubectl apply -f deploy/k8s/01-storage-class.yaml
kubectl apply -f deploy/k8s/02-shared-resources.yaml
kubectl apply -f deploy/k8s/03-network-policy.yaml
kubectl apply -f deploy/k8s/consul.yaml
kubectl apply -f deploy/k8s/jaeger.yaml
kubectl apply -f deploy/k8s/dtm.yaml
kubectl apply -f deploy/k8s/mysql.yaml
kubectl apply -f deploy/k8s/redis.yaml
kubectl apply -f deploy/k8s/mongodb.yaml
kubectl apply -f deploy/k8s/rabbitmq.yaml
kubectl apply -f deploy/k8s/redis-init.yaml
kubectl apply -f deploy/k8s/mongodb-init.yaml
kubectl -n infrastructure wait --for=condition=complete job/redis-cluster-init --timeout=900s
kubectl -n infrastructure wait --for=condition=complete job/mongodb-rs-init --timeout=900s
kubectl -n infrastructure rollout status statefulset/mysql --timeout=600s
kubectl -n infrastructure rollout status statefulset/mongodb --timeout=600s
kubectl -n infrastructure rollout status statefulset/consul --timeout=600s
kubectl -n infrastructure rollout status statefulset/dtm --timeout=600s
```

Redis Job 只对全新空节点建集群；完整集群直接退出，部分建成的集群拒绝自动重置。MongoDB Job 只初始化新的 rs0 或验证已有单成员配置，不执行 reconfig。失败时查 Job 日志并人工排查，不能通过删 PVC 或重建 Job 代替修复。完成的 Job 保留用于审计，Job 模板具有不可变字段，更新清单前需明确是否需要另一个版本的 Job。

MongoDB readiness 在成为 primary 后才成功，因此必须先提交初始化 Job，再等待 MongoDB Ready，避免循环等待。Redis 的 PING 探针仅证明进程可访问，不能替代 cluster_state 与槽分配检查。

### 2. 导入业务表

MySQL 首次启动仅创建三个数据库和应用账号，没有创建用户表、视频表或 barrier 表。仓库尚无可直接执行的完整 SQL 迁移基线；需要从可信环境导出并审查 schema/种子数据，导入 `vw_user`、`vw_video`，并按当前 DTM SDK 版本准备 `dtm_barrier.barrier`。未完成之前不要启动业务服务，不自动运行 GORM Gen、AutoMigrate 或实验测试代替迁移。

MongoDB/MySQL 官方初始化目录仅在空数据目录首次启动执行。更改 Secret 不会修改已有账号，导入旧数据必须先核对版本、账号、副本集配置和备份；本套配置不自动挂接现有数据库卷。

### 3. Elasticsearch 与 Kibana

```sh
kubectl apply -f deploy/k8s/elasticsearch.yaml
kubectl -n infrastructure rollout status statefulset/elasticsearch --timeout=900s
kubectl -n infrastructure exec -it elasticsearch-0 -- bin/elasticsearch-reset-password -u kibana_system -i
```

交互设置 kibana_system 密码，使其与 elasticsearch-auth 的 kibana-password 一致，再执行：

```sh
kubectl apply -f deploy/k8s/kibana.yaml
kubectl -n infrastructure rollout status deployment/kibana --timeout=600s
```

elastic-password 仅用于全新 ES 数据目录的引导；已有数据的账号密码不会因改 Secret 自动改变。ES 禁用 mmap 以避免修改主机 vm.max_map_count，适用于本开发基线，不代表生产性能建议。单节点索引若配置了副本可能长期 yellow；探针允许 yellow，但不代表副本可用。正式对外提供服务前需单独评估当前固定镜像版本的安全支持情况，并进行升级与兼容验证。

### 4. 业务服务

确认镜像已在两个工作节点可用、数据库 schema 已准备、基础设施健康后，先用户、后视频、最后网关：

```sh
kubectl apply -f deploy/k8s/vw-user.yaml
kubectl -n videoweb rollout status deployment/vw-user --timeout=600s
kubectl apply -f deploy/k8s/vw-video.yaml
kubectl -n videoweb rollout status deployment/vw-video --timeout=600s
kubectl apply -f deploy/k8s/vw-gateway.yaml
kubectl -n videoweb rollout status deployment/vw-gateway --timeout=600s
```

应用均为单副本 Recreate：当前源码使用固定 Consul 实例 ID，不能直接增加副本，否则注册会互相覆盖。注册的用户/视频服务名保留 `videoweb.user.service`、`videoweb.video.service`，与 Kubernetes Service 名不同。网关将 DTM 注册为 `dtm`，两条 Saga 地址对齐 `discovery:///dtm`；CONSUL_HTTP_ADDR 同时覆盖公共 resolver 的默认地址。DTM 通过服务发现回调用户/视频 RPC，Consul 也会主动进行健康检查。

后续协议升级仍先视频服务、后网关，尤其游客详情和 DASH 路由兼容。TCP readiness 仅检查监听，不证明业务表完整、事务补偿成功或播放器可用；部署后需要另行验证注册登录、上传、游客详情、DASH 播放和 Saga。

## 网络访问与日常维护

所有 Service 为 ClusterIP，不开放 NodePort、LoadBalancer 或公网 Ingress。策略只限制标签 `app.kubernetes.io/part-of=videoweb` 的 Pod，不接管基础设施命名空间内的其他应用；允许本项目基础设施内部通信、videoweb 访问基础设施，以及 Consul/DTM 访问业务服务。不限制 egress，以保留 DNS、SMTP、服务发现和现有回调链路。真正的租户隔离需要更细的账号、RBAC 和网络规划。

本地访问示例，port-forward 默认只监听回环地址，分别在终端运行：

```sh
kubectl -n videoweb port-forward service/vw-gateway 8001:8001
kubectl -n infrastructure port-forward service/kibana 5601:5601
kubectl -n infrastructure port-forward service/jaeger 16686:16686
kubectl -n infrastructure port-forward service/rabbitmq 15672:15672
```

网关入口为 `http://127.0.0.1:8001`。公共 Ingress、TLS 和跨命名空间入口控制器放行规则需另外配置，不要直接加 `--address 0.0.0.0` 暴露管理界面。若集群额外配置了 egress/default-deny 策略，还需对应放行；本清单不会取消其他策略。

可通过 `kubectl kustomize deploy/k8s` 离线预览全部资源。首次应按上述顺序分批应用；仅在凭据、证书、镜像、schema 和初始化状态均确认后才考虑 `kubectl apply -k deploy/k8s`。聚合清单刻意不设置全局 namespace，防止 Kustomize 把基础设施重新映射到 videoweb。

StorageClass 使用 Retain，但不等于备份。删除 StatefulSet 通常保留 PVC，删除命名空间会删除其中 PVC；保留的 PV 也不自动完成恢复绑定。禁止用删除命名空间/PVC、Redis reset 或 MongoDB reconfig 作为常规重启操作。升级镜像、轮换凭据和调整存储前先备份并验证恢复流程。

## 离线验证

校验器不读取 `local/`，不执行 apply，不连接 Kubernetes API，也不启动容器。依赖 Python、kubectl 和 PyYAML；可将依赖装到被忽略的目录：

```sh
python -m pip install --target deploy/k8s/.tools PyYAML==6.0.2 jsonschema==4.23.0
python deploy/k8s/Test-Manifests.py
```

可选下载 Kubernetes v1.35.0 官方 OpenAPI v2 JSON 到 `.tools/kubernetes-1.35-openapi.json`，通过 `--schema deploy/k8s/.tools/kubernetes-1.35-openapi.json` 校验标准资源结构；通过 `--shell /path/to/bash` 执行内嵌 shell 的 `bash -n`。下载由操作者明确执行，脚本不自动联网。

本次验证：18 个清单文件、41 个资源、14 个工作负载、3 份应用配置、15 段 shell 片段；Kustomize 渲染、官方 v1.35 OpenAPI 结构检查和 shell 语法检查通过。未执行真实 API admission、Longhorn 参数验证、证书验证、镜像构建、基础设施集成、浏览器播放或集群容量实测。v1.35.0 结构验证也不能替代目标 k3s v1.35.5 的实际服务端准入。

## 参考

- [Kubernetes StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
- [Kustomize](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/)
- [Kubernetes v1.35.0 OpenAPI](https://raw.githubusercontent.com/kubernetes/kubernetes/v1.35.0/api/openapi-spec/swagger.json)
- [Longhorn StorageClass 参数](https://longhorn.io/docs/archives/1.10.0/references/storage-class-parameters/)
- [Longhorn RWX 卷](https://longhorn.io/docs/archives/1.10.0/nodes-and-volumes/volumes/rwx-volumes/)
- [Redis 7.4 配置](https://raw.githubusercontent.com/redis/redis/7.4/redis.conf)
- [Elasticsearch 虚拟内存配置](https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/virtual-memory)
