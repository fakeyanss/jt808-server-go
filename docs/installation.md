# 安装与部署

本文覆盖从源码构建、单机运行、数据库初始化到集群角色部署的完整路径。默认配置使用 `memory` 后端，不需要任何外部依赖。

## 1. 前置条件

| 依赖 | 版本 / 说明 |
|---|---|
| Go | 1.26+ |
| make / bash | 构建脚本依赖 |
| SQLite | 可选；`database.type=sqlite` 时需要可写的库文件路径，驱动为纯 Go，无需 cgo |
| MySQL | 可选；集群模式必须提供共享 MySQL |
| Docker | 可选；验证镜像与 compose |
| NTP/chrony | 集群模式必须；节点判死、会话路由与租约都依赖进程时钟 |

## 2. 获取代码与构建

```sh
git clone https://github.com/fakeyanss/jt808-server-go.git
cd jt808-server-go

make compile            # target/debug/jt808-server-go（通用入口）
make release            # darwin/linux × amd64/arm64 交叉编译
make compile-migrate    # target/debug/jt808-migrate
make compile-client     # target/debug/jt808-client-go
```

按角色构建生产二进制：

```sh
go build -o bin/jt-server-standalone ./cmd/jt-server-standalone
go build -o bin/jt-signal            ./cmd/jt-signal
go build -o bin/jt-control           ./cmd/jt-control
go build -o bin/jt-media             ./cmd/jt-media
go build -o bin/jt-attachment        ./cmd/jt-attachment
go build -o bin/jt808-migrate        ./cmd/jt808-migrate
```

`jt808-server` 是保留的通用入口，完全按配置文件的 `server.mode` 与 `cluster.roles` 运行，适合同机调试任意角色组合；生产部署应使用固定角色的二进制，避免配置与二进制职责错配。

## 3. 单机运行

```sh
# 使用编译进二进制的内嵌默认配置
./target/debug/jt808-server-go

# 或读取 YAML
./target/debug/jt808-server-go -c configs/default.yaml
```

默认监听：

| 入口 | 默认 | 配置 |
|---|---|---|
| 终端接入 TCP | `:8080` | `server.port.tcpPort` |
| 终端接入 UDP | `:8081` | `server.port.udpPort` |
| 业务 API | `:8008` | `server.port.httpPort` |
| 苏标附件 TCP/UDP | `:7200` / `:7201/udp` | `alarmServer.port` / `udpPort` |
| 1078 RTP | 未启用 | `mediaServer.port` / `tcpPort` |
| 管理面 | 未启用 | `admin.enable` + `admin.addr` |
| 探针 | 未启用 | `probes.addr` |

启动横幅由 `server.banner.bannerPath` 指定，默认 `configs/banner.txt`；`server.banner.enable: false` 可关闭。

### 3.1 验证接入链路

```sh
# 终端 2：启动自带模拟终端
make run-client

# 查询已注册设备
curl -s http://localhost:8008/api/device
```

设备列表出现模拟终端手机号，说明接入、协议处理与查询链路已打通。业务 API 鉴权默认关闭；生产环境必须配置 `api.auth.token`。

## 4. 数据库初始化与迁移

SQLite 与 MySQL 都**不会**在服务启动时自动执行 DDL。首次部署或升级后先运行独立迁移工具：

```sh
# 在配置文件中设置 database.type 与连接参数
./target/debug/jt808-migrate -c configs/default.yaml
```

迁移 SQL 分两套目录：

- `internal/repository/sqlite/migrations/`
- `internal/repository/mysql/migrations/`

两套目录版本号必须同步推进。`jt808-migrate` 按 `database.type` 只执行对应目录。主服务启动只校验 `schema_migrations` 版本：版本落后会提示先迁移，版本超前会拒绝启动；检测到旧的 8 位数字版本时会提示重建 dev 库。

MySQL 场景也可以由 DBA 按文件名顺序直接执行 `internal/repository/mysql/migrations/` 下的 SQL，但必须保证 `schema_migrations` 记录与二进制预期一致。

## 5. Docker 与 Compose

```sh
make dockerbuild
docker compose -f deploy/docker-compose.yml up --build
```

镜像入口为 `/app/output/jt808-server-go -c /app/output/configs/default.yaml`，同时包含 `/app/output/jt808-migrate`。compose 映射 `8080`、`8081/udp`、`8008`、`7200`、`7201/udp`，挂载只读配置与 `data`/`logs` 卷。

注意：

- 管理面默认只监听容器内 `127.0.0.1:9000`，不发布到宿主机。
- compose 的 healthcheck 依赖 `/healthz`，因此需要开启 `admin` 并配置非空 token，否则容器会一直不健康。也可以改用独立的 `probes.addr`。
- 容器内工作目录为 `/app/output`，配置中的相对路径按该目录解析。

```sh
# 容器内检查探针
docker exec jt808 wget -qO- http://127.0.0.1:9000/healthz
docker exec jt808 wget -qO- http://127.0.0.1:9000/readyz
```

## 6. 集群角色部署

### 6.1 角色与二进制

| 二进制 | 固定角色 | 样例配置 | 探针示例端口 | 是否连业务库 |
|---|---|---|---|---|
| `jt-server-standalone` | `mode=standalone` | `configs/cluster/jt-server-standalone.yaml` | 9105 | 可选 |
| `jt-signal` | `cluster.roles=[gateway]` | `configs/cluster/jt-signal.yaml` | 9106 | 否 |
| `jt-media` | `cluster.roles=[media]` | `configs/cluster/jt-media.yaml` | 9107 | 否 |
| `jt-control` | `cluster.roles=[control]` | `configs/cluster/jt-control.yaml` | 9108 | 是 |
| `jt-attachment` | `cluster.roles=[attachment]` | `configs/cluster/jt-attachment.yaml` | 9109 | 否 |

配置中的角色与二进制不匹配时启动失败，错误信息会提示应使用的二进制。

### 6.2 前置准备

1. **共享 MySQL**：只有 `jt-control` 连库。生产禁止用 SQLite 冒充共享库；`cluster.allowEmbeddedStorage` 仅限本地联调，开启后 `/readyz` 返回 `degraded`。
2. **共享存储**：媒体切片、`0x0801` 图片、苏标附件、1078 FTP 录像共用一套存储。
   - `sharedVolume`：所有角色指向同一 `root`，并创建标记文件 `data/blobs/.jt808-shared-volume`；
   - `objectStore`：S3 兼容对象存储，配置 endpoint、bucket、accessKey、secretKey；`pathStyle` 默认 `true`，兼容 MinIO。
3. **时间同步**：所有主机启用 NTP/chrony，`chronyc tracking` 的 `System time` 偏差应在 100ms 内。节点判死、会话路由、outbox 与任务租约都使用进程时钟。
4. **共享密钥**：`cluster.gateway.token`（control → gateway）、`cluster.control.token`（gateway/media/attachment → control）、`uplink.http.token` 必须一致且使用独立强随机值。
5. **端口规划**：按[配置说明](configuration.md#10-集群配置)核对，避免同机部署撞车。

### 6.3 启动顺序

```sh
# 1) 初始化共享库（使用 control 配置）
./bin/jt808-migrate -c configs/cluster/jt-control.yaml

# 2) 启动 control
./bin/jt-control -c configs/cluster/jt-control.yaml

# 3) 启动媒体与附件节点
./bin/jt-media      -c configs/cluster/jt-media.yaml
./bin/jt-attachment -c configs/cluster/jt-attachment.yaml

# 4) 启动 808 网关
./bin/jt-signal -c configs/cluster/jt-signal.yaml
```

网关可以在 control 之前启动，但设备鉴权会因查不到档案而拒绝；建议先 control，再数据面节点，最后网关。

### 6.4 集群端口

| 用途 | 默认 | 角色 / 配置项 |
|---|---|---|
| 808 主连接 TCP/UDP | 8080 / 8081 | signal，`server.port.*` |
| 业务 HTTP API | 8008 | control，`server.port.httpPort` |
| admin | 9000 | control，`admin.addr` |
| gateway 下行契约 | 9101 | signal，`cluster.gateway.addr` |
| control 上行入口 | 9102 | control，`cluster.control.addr` |
| 1078 媒体 TCP/UDP | 7300 | media，`mediaServer.port` |
| 1078 媒体 HTTP / FLV | 7301 | media，`mediaServer.httpPort` |
| 附件 TCP/UDP | 7400 / 7401 | attachment，`alarmServer.port/udpPort` |
| 附件内部 HTTP | 7402 | attachment，`alarmServer.httpPort` |
| 1078 FTP 控制 | 7500 | attachment，`cluster.attachment.ftp1078.port` |
| 1078 FTP 被动端口 | 7501-7510 | attachment，`passivePorts` |
| 探针 | 9105–9109 | 每角色独立，`probes.addr` |

防火墙需要放行终端可达的接入端口、FTP 被动端口段，以及 control 与数据面节点之间的内部 HTTP。

### 6.5 集群验证

```sh
# 每角色探针
curl -s http://<node>:9106/healthz
curl -s http://<node>:9106/readyz

# control 管理面
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" http://<control>:9000/metrics

# 发起实时播放（control）
curl -s -X POST http://<control>:8008/api/device/<phone>/playback \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $API_TOKEN" \
  -d '{"channel":1,"kind":"live"}'
```

`/readyz` 会按角色检查 `database`、`outbox_backlog`、`control_endpoint`、`media_nodes`、`attachment_nodes`、`attachment_object_store`、磁盘目录等项；未启用的组件不参与判定。

## 7. 升级与回滚

1. 备份数据库与 `data/` 目录；
2. 替换二进制或镜像；
3. 执行 `jt808-migrate` 升级 schema；
4. 启动并检查 `/readyz`；
5. 如需回滚，恢复到升级前的数据库备份；数据库迁移默认只向前，不能只回退二进制。

配置级回滚与保留期调整见[运维手册](operations.md)。

## 8. 相关文档

- [配置说明](configuration.md)
- [架构与集群模式](architecture.md)
- [运维手册](operations.md)
- [业务 API](api.md)
