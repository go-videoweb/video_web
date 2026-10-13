# k3s 集群信息

> 信息快照时间：2026-10-13 08:31（Asia/Shanghai）  
> 数据来源：`kubectl get/describe/top nodes` 及节点标签、注解  
> 本文不记录登录密码、集群令牌等敏感信息。

## 1. 集群概览

当前集群由 3 个节点组成，所有节点均处于 `Ready` 状态，k3s/Kubernetes 版本一致。

| 项目 | 当前值 |
|---|---|
| 节点总数 | 3 |
| 控制平面节点 | 1 台：`zey-tencentcloud01` |
| 专用工作节点 | 2 台：`home-worker`、`k3s-worker-02` |
| k3s 版本 | `v1.35.5+k3s1` |
| 容器运行时 | `containerd://2.2.3-k3s1` |
| CPU 总量 | 22 核 |
| 可分配内存总量 | 约 20.4 GiB |
| 单节点 Pod 上限 | 110 |
| 网络 | 节点间使用 `100.x` 内部地址；Flannel VXLAN |
| 持久化存储 | Longhorn CSI |

## 2. 节点与角色

| Kubernetes 节点名 | 节点角色 | 状态 | Internal IP | PodCIDR | CPU | 可分配内存 | 操作系统 | 内核 |
|---|---|---|---|---|---:|---:|---|---|
| `zey-tencentcloud01` | 控制平面 | Ready | `100.78.108.15` | `10.42.1.0/24` | 2 核 | 约 3.5 GiB | Ubuntu 24.04.4 LTS | `6.8.0-124-generic` |
| `home-worker` | 工作节点 | Ready | `100.84.12.24` | `10.42.0.0/24` | 8 核 | 约 7.2 GiB | Ubuntu 26.04 LTS | `7.0.0-34-generic` |
| `k3s-worker-02` | 工作节点 | Ready | `100.101.240.101` | `10.42.2.0/24` | 12 核 | 约 9.7 GiB | Ubuntu 26.04.1 LTS | `7.0.0-31-generic` |

### 2.1 哪些节点是工作节点

专用工作节点共有两台：

1. `home-worker`
2. `k3s-worker-02`

这两台节点以 k3s `agent` 模式加入集群，负责承载应用工作负载。它们没有设置 `node-role.kubernetes.io/worker` 标签，所以 `kubectl get nodes` 的 `ROLES` 列显示为 `<none>`；这不表示节点不能运行工作负载。

`zey-tencentcloud01` 带有 `node-role.kubernetes.io/control-plane=true` 标签，是控制平面节点。该节点目前没有 `NoSchedule` 污点，因此除了运行控制平面和系统组件，也可以被调度普通 Pod。若希望控制平面只承担集群管理职责，需要另行添加控制平面污点。

## 3. 节点详细信息

### 3.1 `zey-tencentcloud01`

- 角色：k3s server / Kubernetes control-plane
- 创建时间：2026-07-06 10:52:54 +08:00
- 节点状态：Ready
- 是否允许调度：是（`Unschedulable=false`）
- 污点：无
- Internal IP：`100.78.108.15`
- PodCIDR：`10.42.1.0/24`
- 容量：2 CPU，约 3.5 GiB 可分配内存
- 当前资源使用：CPU `172m`（8%），内存 `2135Mi`（59%）
- 当前非终止 Pod：22 个
- 主要负载：CoreDNS、Metrics Server、Local Path Provisioner、Longhorn 控制组件及 CSI 控制组件

关键标签：

```text
node-role.kubernetes.io/control-plane=true
kubernetes.io/hostname=zey-tencentcloud01
node.kubernetes.io/instance-type=k3s
```

### 3.2 `home-worker`

- 角色：k3s agent / 工作节点
- 创建时间：2026-08-26 09:01:29 +08:00
- 节点状态：Ready
- 是否允许调度：是（`Unschedulable=false`）
- 污点：无
- Internal IP：`100.84.12.24`
- PodCIDR：`10.42.0.0/24`
- 容量：8 CPU，约 7.2 GiB 可分配内存
- 当前资源使用：CPU `79m`（不足 1%），内存 `2247Mi`（30%）
- 当前非终止 Pod：6 个
- 主要负载：Milvus Standalone、MinIO、Longhorn 节点组件

关键标签：

```text
nodepool=home
workload=heavy
node.longhorn.io/create-default-disk=true
kubernetes.io/hostname=home-worker
```

### 3.3 `k3s-worker-02`

- 角色：k3s agent / 工作节点
- 创建时间：2026-09-04 10:48:16 +08:00
- 节点状态：Ready
- 是否允许调度：是（`Unschedulable=false`）
- 污点：无
- Internal IP：`100.101.240.101`
- PodCIDR：`10.42.2.0/24`
- 容量：12 CPU，约 9.7 GiB 可分配内存
- 当前资源使用：CPU `72m`（不足 1%），内存 `3708Mi`（37%）
- 当前非终止 Pod：9 个
- 主要负载：TeamCity Server、TeamCity Agent、MySQL、Milvus Attu、Milvus Etcd、Longhorn 节点组件

关键标签：

```text
nodepool=home
workload=heavy
location=node2
node.longhorn.io/create-default-disk=true
kubernetes.io/hostname=k3s-worker-02
```

## 4. 调度与工作负载说明

### 4.1 重型工作负载

两台工作节点均带有以下标签：

```text
workload=heavy
```

需要运行在大资源节点上的 Deployment、StatefulSet 或 Pod，可以使用：

```yaml
spec:
  template:
    spec:
      nodeSelector:
        workload: heavy
```

该选择器会允许调度到 `home-worker` 或 `k3s-worker-02`。如果需要固定到某一台节点，应使用更加具体的标签，例如 `location=node2`，或为节点添加专用业务标签。

### 4.2 当前调度特征

- 3 个节点当前都没有污点，均允许调度 Pod。
- 两台工作节点承担主要业务与数据服务。
- 控制平面节点主要运行集群系统组件和 Longhorn 控制组件，但当前没有强制禁止业务 Pod。
- `k3s-worker-02` 的 CPU 和内存容量最大，当前承载 TeamCity、MySQL 和部分 Milvus 组件。
- `home-worker` 主要承载 Milvus Standalone、MinIO 和 Longhorn 节点组件。

## 5. 网络与存储

### 5.1 网络

- 节点内部地址均为 `100.x` 网段地址。
- Flannel 后端类型为 `vxlan`。
- 各节点分配独立 `/24` Pod 网段：
  - `home-worker`：`10.42.0.0/24`
  - `zey-tencentcloud01`：`10.42.1.0/24`
  - `k3s-worker-02`：`10.42.2.0/24`

### 5.2 存储

- 集群安装了 Longhorn CSI。
- 两台工作节点均带有 `node.longhorn.io/create-default-disk=true` 标签，可创建 Longhorn 默认磁盘。
- 涉及 Longhorn 数据卷的业务应持续关注卷的 `robustness`、副本数量和副本分布，避免单副本运行。
- 节点上的 `multipathd` 应排除 Longhorn 创建的 `sd*` iSCSI 设备，防止设备被错误映射后导致卷挂载失败。

## 6. 常用检查命令

```bash
# 查看节点、角色、版本和 IP
kubectl get nodes -o wide

# 查看节点标签
kubectl get nodes --show-labels

# 查看节点资源实时使用情况
kubectl top nodes

# 查看节点详细状态、容量、污点和已调度 Pod
kubectl describe node <node-name>

# 查看每个 Pod 所在节点
kubectl get pods -A -o wide

# 查看 Longhorn 卷状态
kubectl -n longhorn-system get volumes.longhorn.io
```

## 7. 快速结论

- 当前 3 个节点全部健康，状态均为 `Ready`。
- `zey-tencentcloud01` 是唯一控制平面节点。
- `home-worker` 和 `k3s-worker-02` 是两台专用工作节点。
- 两台工作节点的 `ROLES` 显示 `<none>` 是因为未显式设置 worker 角色标签，不影响其实际工作节点身份和调度能力。
- 控制平面节点目前也可调度 Pod；是否隔离业务负载应根据后续运维策略决定。

