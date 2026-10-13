# 本地中间件部署

Kubernetes/k3s 的独立服务清单见 [k8s/README.md](k8s/README.md)，采用 `infrastructure` 与 `videoweb` 两个命名空间，与下文 Compose 配置分别维护。

这份 Compose 是开发环境示例，只定义 MongoDB、DTM、Jaeger、RabbitMQ、三个 Elasticsearch 节点和 Kibana；不包含 MySQL、Redis、Consul，也没有接通异步消费或视频搜索业务。2026-10-12 已将第五步的 Compose 与根忽略规则恢复到工作树，与本目录的环境示例、bootstrap 文件和校验脚本配套；原 stash 保留，文件尚未提交。验证范围仅为静态配置，没有启动容器或访问数据。

以下命令均从 `video_web_backend/` 执行，需要 Docker Compose v2 或更新版本。

## 文件入口与维护范围

| 文件或目录 | 用途与当前状态 |
| --- | --- |
| `docker-compose.yaml` | 八个中间件容器的日常配置入口，不定义 Go 业务服务 |
| `.env.example` | 凭据占位、18 个发布端口、数据根目录、ES 版本与网络的示例 |
| `.env`、`.env.*` | 本机参数，根忽略规则保护；只有 `.env.example` 可跟踪 |
| `deploy/elasticsearch-bootstrap.yaml` | 仅首次创建空 ES 集群时合并的覆盖文件 |
| `deploy/Test-Compose.ps1` | 仅通过 `docker compose config` 检查展开后的模型 |
| `deploy/data/` | 默认运行数据、Mongo 配置与 ES 证书目录，已忽略 |
| `vw_gateway/Dockerfile`、`vw_user/Dockerfile`、`vw_video/Dockerfile` | 历史模板，尚未适配当前项目，见下文 |
| `vw_user/deploy/`、`vw_video/deploy/` | 本机旧部署材料，不作为本套 Compose 的配置入口；不读取、迁移或覆盖其中数据和凭据 |

本批部署范围是 Compose、根忽略规则、`.env.example` 和本目录三个配套文件。`async_service/`、未跟踪 ES 模型、常量草稿、图片以及网关已有的 `.gitignore` 改动属于其他工作，不随本批自动纳入提交。原 stash 同时包含的视频配置备份删除也没有恢复。

## 组件与连接关系

| 组件 | 当前镜像版本 | 默认宿主机端口 | 用途与外部准备 |
| --- | --- | --- | --- |
| MongoDB | `mongo:8.0` | `27017` | 业务文档数据；准备配置、keyfile，并核对 `rs0` 初始化 |
| DTM | `yedf/dtm:1.19.0` | HTTP `36789`、gRPC `36790` | Saga 协调；需能访问外部 Consul 和已注册的业务 RPC |
| Jaeger | `jaegertracing/all-in-one:1.67.0` | UI `16686`、collector `14250/14268`、OTLP `4317/4318`、Zipkin `9411` | 开发链路追踪，内存存储 |
| RabbitMQ | `rabbitmq:4.1.0-management` | AMQP `5672`、管理界面 `15672` | 消息基础设施；应用消费链路尚未实现 |
| Elasticsearch | `8.17.0`，三个节点 | HTTP `9201/9202/9203`、transport `9301/9302/9303` | 认证和 TLS 集群；准备证书、外部网络和数据目录 |
| Kibana | `8.17.0` | `5601` | 连接 ES；先设置 `kibana_system` 密码 |

网关、用户、视频服务仍需各自准备本机运行配置，`.env` 仅供 Compose 插值，不会自动写入或替换 Kratos YAML。MySQL、Redis Cluster 和 Consul 由已有外部环境提供；默认 localhost 连接方式以 Go 服务在宿主机运行作为前提。若将业务放进容器，须另行适配连接地址、服务发现、资源目录和镜像构建。

当前网关与用户示例都使用 gRPC `9001`，网关 HTTP 也为 `8001`；用户/视频进程目前只启动 gRPC，不能把示例中的 HTTP 段当成已监听端口。多进程同机运行时需要在各自本机配置中分配独立 gRPC 端口，并核对 Consul 公布地址。DTM 注册与回调地址需单独核对，修改 Compose 发布端口不会自动修改这些配置。

## 历史业务 Dockerfile

三个服务的 Dockerfile 仍使用 Go 1.19，而各 module 声明 Go 1.23 系列版本；单服务目录的构建上下文也不包含本地 `replace` 需要的兄弟模块。模板启动命令为 `./server`，与当前 `cmd/vw_gateway`、`cmd/vw_user`、`cmd/vw_video` 对应的构建产物未对齐。

视频运行镜像尚未安装 ffmpeg/ffprobe，网关、用户、视频还通过 `runtime.Caller` 推导源码位置下的资源目录，模板没有配套处理这些路径。因此这些文件只保留为历史材料，本批没有据此构建或发布业务镜像；准备应用容器化时需一起处理工具链、构建上下文、产物名称、资源挂载与媒体依赖。

## 配置与数据保护

从 `.env.example` 准备本机 `.env`，替换四个 `CHANGE_ME_*` 占位密码。`.env`、`.env.*` 和 `deploy/data/` 已忽略，`.env.example` 是唯一例外；不要提交真实凭据、私钥和数据。含 `$` 的密码在 `.env` 中使用单引号，避免插值；进程环境变量优先于文件。不要将 `docker compose config` 的完整输出发到日志中，它会展开密码。

`MONGO_ROOT`、`DTM_ROOT`、`RABBITMQ_ROOT`、`ELASTICSEARCH_ROOT`、`KIBANA_ROOT` 控制各自挂载根目录。默认在 `deploy/data/`；自定义目录不受这条默认数据忽略规则保护，需单独检查。使用原有数据时，必须在 `.env` 中改为对应旧目录，例如 `MONGO_ROOT='D:/your existing path/MongoDB'`。切换路径不会搬迁数据，不能把空目录启动成功理解为旧数据已经迁移。先备份并核对镜像版本、目录权限和现有账号；不要直接在旧集群的数据上尝试首次初始化。

所有宿主机发布端口默认绑定 `127.0.0.1`。可通过 `BIND_HOST` 和 Compose 中列出的 `*_PORT` 变量调整；暴露到非回环地址前需另行配置防火墙、认证和 TLS。默认端口：MongoDB 27017；DTM 36789/36790；Jaeger UI 16686，collector 14250/14268，OTLP 4317/4318，Zipkin 9411；RabbitMQ 5672/15672；ES HTTP 9201/9202/9203、transport 9301/9302/9303；Kibana 5601。未检查这些端口是否已被本机进程占用。

## 启动前提

### MongoDB

保留原有挂载用途：`logs/`、`data/`、`config/mongod.conf`、`config/mongo-keyfile`。后两个文件必须先准备，不能由 Docker 自动创建成目录。核对 `mongod.conf` 中的日志、数据路径与挂载一致；不要配置与命令行不同的副本集名称或 keyFile 路径。

启动命令将 keyfile 复制到容器的 `/tmp` 后调整权限和属主，不修改宿主机原文件；再调用官方 entrypoint，以保留空数据目录的管理员初始化行为。`MONGO_INITDB_*` 只用于新数据初始化，不会替换已有账号密码。`rs0` 的初始化仍需管理员手工完成；不要在已有副本集上重复执行 `rs.initiate()`。副本集成员公布的地址必须能被实际 Go 客户端访问，不能直接照抄容器内部主机名。

### Elasticsearch 与 Kibana

保留外部 `elastic` 网络，ES 与 Kibana 使用映射形式声明静态 IP。先检查 Docker 网络和本机/VPN 网段是否冲突；仅当不存在且网段可用时创建：

```powershell
docker network create --driver bridge --subnet 172.18.0.0/16 elastic
```

已有同名网络也必须具有合适子网，四个 IP 不得已被占用。换网段时同步修改 `.env` 的 `ES01_IP`、`ES02_IP`、`ES03_IP`、`KIBANA_IP` 和节点证书 SAN。Compose 不会创建或删除该外部网络。

ES 与 Kibana 共用 `ELASTIC_VERSION=8.17.0`，沿用本批原稿版本，没有借此升级。ES 每节点默认 512 MiB 堆和 1 GiB 容器上限，实际运行需给 Docker VM 预留足够内存，并按 Elasticsearch 文档配置 `vm.max_map_count`。这是旧版开发基线，面向外网或生产前应单独评估版本支持、安全更新和资源容量。

ES 启用认证、HTTP TLS 与节点间 TLS，不挂载整个旧 `config/` 覆盖镜像默认配置。准备同一 CA 签发的 PEM 证书，放到 `${ELASTICSEARCH_ROOT}/certs/`：

```text
certs/
  ca/ca.crt
  es01/es01.crt  es01/es01.key
  es02/es02.crt  es02/es02.key
  es03/es03.crt  es03/es03.key
```

三个节点目录另需 `data/`、`plugins/`、`logs/` 并允许容器用户读写；证书目录须允许容器用户读取。每个节点证书的 SAN 包含该节点 DNS 名（如 `es01`）、`localhost`、`127.0.0.1` 和实际静态 IP；若 Go 客户端使用其他地址，也需包含该地址。使用同版本 `elasticsearch-certutil` 的 `ca --pem`、`cert --pem --in <instances.yml> --ca-cert <ca.crt> --ca-key <ca.key>` 生成，解压后按上面的结构放置。输入文件的实例名使用 `es01/es02/es03`，分别列出对应 DNS/IP。此示例使用未加密节点 PEM key，私钥仅用于本地开发，必须限制读权限；CA 私钥离线保存，不要放到三个节点共用的挂载目录。

旧 PKCS#12 文件和工作区的 `http_ca.crt` 不会自动转换，也不能假定与这套 CA 匹配。复用旧数据、插件或证书前需核对兼容性；IK 等插件不是本批自动安装的内容，如后续需要，应在三个节点安装相同且匹配 ES 的版本。

**首次创建空集群**才使用 bootstrap 覆盖文件：

```powershell
docker compose -f docker-compose.yaml -f deploy/elasticsearch-bootstrap.yaml up -d es01 es02 es03
```

健康检查会验证 CA、账号和集群至少为 yellow；yellow 不代表副本已全部就绪。确认集群形成后，日常启动只用基础 Compose（不再加 bootstrap），不要对现有集群、重启或节点重加入重复设置 `cluster.initial_master_nodes`。

ES 就绪后，以交互方式设置 `kibana_system` 密码，与本机 `.env` 的 `KIBANA_PASSWORD` 保持一致：

```powershell
docker compose exec es01 bin/elasticsearch-reset-password -u kibana_system -i
docker compose up -d kibana
```

Kibana 环境变量不会自动创建/修改这个账户；浏览器登录使用实际用户（本地可用 `elastic`），不是 `kibana_system`。Kibana 到 ES 验证 TLS，浏览器到 Kibana 仍是本机 HTTP。

### 其他中间件

RabbitMQ 使用正确的 `RABBITMQ_DEFAULT_VHOST`，默认 `video_web`；账号和 vhost 只作用于新数据初始化。固定 `hostname: rabbitmq` 以稳定节点身份。此示例不再挂载未知本机 `rabbitmq.conf`，使用镜像默认配置；若旧实例有定制设置，先审查并用本机 override 只读挂载，不能认为本配置等价于原有运行实例。

DTM 固定 `1.19.0`，boltdb 的工作目录和挂载均为 `/var/lib/dtm`。网关现有启动逻辑会向 Consul 注册 DTM；服务示例配置和注册地址仍需人工核对。DTM 容器调用 Go 服务时必须能访问 Consul 中公布的地址，容器内 `localhost` 不是宿主机；普通 RPC 与 Saga 的发现路径也不同，本批不修改它们。

Jaeger all-in-one 固定 `1.67.0`，保留项目原有 collector 端口并打开 OTLP/Zipkin。使用内存存储，重启会丢失 trace，不能当作持久化监控。完成上述前提后，可按需单独启动非 ES 组件：

```powershell
docker compose up -d mongodb dtm jaeger-alo rabbitmq
```

## 静态验证

```powershell
docker compose --env-file .env.example config --quiet
pwsh -NoProfile -File deploy/Test-Compose.ps1
```

脚本兼容 PowerShell 7 和 Windows PowerShell 5.1，使用 UTF-8 BOM 以支持两者读取中文注释。若本机执行策略阻止这个已核对的本地脚本，可仅对检查子进程执行 `powershell -NoProfile -ExecutionPolicy Bypass -File deploy/Test-Compose.ps1`，无需修改系统执行策略。

示例文件仅用于语法验证，不用于启动。脚本检查环境示例的参数完整性、服务集合、18 个发布端口的数量与冲突、回环绑定、ES/Kibana 版本与 TLS、外部网络声明、只读挂载、首次启动覆盖、四项必填凭据和 Windows 带空格路径/端口/版本覆盖；通过 JSON 解析 Compose 模型，不输出展开后的密码，不依赖 Docker daemon，不创建目录或网络。不会验证镜像拉取、挂载是否存在、证书有效性、实际网络、服务健康或历史数据兼容性。

## 官方参考

- [Compose 变量插值](https://docs.docker.com/reference/compose-file/interpolation/)与[服务挂载声明](https://docs.docker.com/reference/compose-file/services/)
- [Elasticsearch 8.17 Docker 部署](https://www.elastic.co/guide/en/elasticsearch/reference/8.17/docker.html)、[证书工具](https://www.elastic.co/guide/en/elasticsearch/reference/8.17/certutil.html)、[集群首次引导](https://www.elastic.co/guide/en/elasticsearch/reference/8.17/modules-discovery-bootstrap-cluster.html)
- [Kibana 8.17 Docker 部署](https://www.elastic.co/guide/en/kibana/8.17/docker.html)
- [RabbitMQ 镜像配置](https://hub.docker.com/_/rabbitmq/)、[Jaeger 1.67 部署](https://www.jaegertracing.io/docs/1.67/deployment/)、[DTM 部署](https://en.dtm.pub/deploy/base.html)
