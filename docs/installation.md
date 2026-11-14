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

## 5. 容器部署（Docker / Compose）

### 5.1 镜像与标签

镜像名为 `fakeyanss/jt808-server-go`，同时发布 `linux/amd64` 与 `linux/arm64`。版本标签取 Git tag（去掉前缀 `v`），并附带 `latest`；分支与 PR 构建只验证、不推送。

```sh
# 本机构建单架构镜像
make dockerbuild IMAGE_TAG=local

# 本机使用 buildx 构建多架构（需要 buildx 与 QEMU）
docker buildx build --platform linux/amd64,linux/arm64 \
  -f build/Dockerfile -t fakeyanss/jt808-server-go:local .
```

镜像内包含 `jt-server-standalone`、`jt-signal`、`jt-control`、`jt-media`、`jt-attachment`、`jt808-migrate` 六个静态二进制，以非 root 用户 `10001:10001` 运行，并带有 CA 证书与时区数据。运行阶段不会写入业务数据；配置、数据与日志都从挂载点读取或写入。

### 5.2 最短运行

默认启动 standalone + `memory` 后端，无需挂载配置：

```sh
docker run -d --name jt808 \
  -p 8080:8080 -p 8081:8081/udp -p 8008:8008 \
  -p 7200:7200 -p 7201:7201/udp \
  -v jt808-data:/app/data -v jt808-logs:/app/logs \
  fakeyanss/jt808-server-go:latest

curl -s http://localhost:8008/api/device
```

默认监听端口：

| 端口 | 用途 |
|---|---|
| `8080/tcp`、`8081/udp` | 808 终端接入 |
| `8008/tcp` | 业务 API |
| `7200/tcp`、`7201/udp` | 苏标附件 |
| `9105/tcp` | 探针（容器内 `127.0.0.1`，默认不发布） |

镜像自带 `HEALTHCHECK`，通过容器内 `http://127.0.0.1:9105/readyz` 判断就绪。手工检查：

```sh
docker inspect --format '{{.State.Health.Status}}' jt808
docker exec jt808 wget -qO- http://127.0.0.1:9105/healthz
docker exec jt808 wget -qO- http://127.0.0.1:9105/readyz
```

### 5.3 单镜像切换角色

默认角色为 `standalone`。首个参数或 `JT808_ROLE` 可切换角色，不需要为每个角色单独构建镜像：

```sh
# 首个参数
docker run --rm fakeyanss/jt808-server-go:latest signal -c /etc/jt808/signal.yaml

# 环境变量
docker run --rm -e JT808_ROLE=signal \
  -v "$PWD/configs/cluster/jt-signal.yaml:/etc/jt808/signal.yaml:ro" \
  fakeyanss/jt808-server-go:latest
```

支持的角色：`standalone`（默认）、`signal`（别名 `gateway`）、`control`、`media`、`attachment`、`migrate`。角色容器会自动读取 `/etc/jt808/<role>.yaml`；也可以显式传 `-c` 或设置 `JT808_CONFIG`。角色与配置中的 `server.mode`、`cluster.roles` 必须匹配，否则启动失败。

集群角色不是开箱即用：需要共享 MySQL、共享存储（`sharedVolume` 或对象存储）以及一致的内部 token，详见[集群角色部署](#6-集群角色部署)与 `configs/cluster/` 下的样例配置。

| 角色 | 主要端口 | 探针示例 |
|---|---|---|
| `signal` | 8080/8081、8008、9101 | 9106 |
| `control` | 8008、9000、9102 | 9108 |
| `media` | 7300、7301 | 9107 |
| `attachment` | 7400/7401、7402、7500、7501-7510 | 9109 |

角色容器如果修改了 `probes.addr`，需要同步设置 `JT808_PROBE_ADDR`，否则镜像内置健康检查仍会请求 `127.0.0.1:9105`：

```sh
docker run -e JT808_ROLE=media \
  -e JT808_PROBE_ADDR=127.0.0.1:9107 \
  -v "$PWD/configs/cluster/jt-media.yaml:/etc/jt808/media.yaml:ro" \
  fakeyanss/jt808-server-go:latest
```

### 5.4 挂载点

| 容器路径 | 内容 | 是否必须挂载 |
|---|---|---|
| `/etc/jt808/standalone.yaml` | 镜像内置 standalone 默认配置 | 覆盖配置时挂载 |
| `/etc/jt808/<role>.yaml` | 角色配置 | 角色容器必须提供 |
| `/app/data` | SQLite 库、附件、媒体、上传文件、对象存储本地目录 | 建议 |
| `/app/logs` | 滚动日志 | 建议 |

容器工作目录为 `/app`，配置中的相对路径按该目录解析。进程以 UID/GID `10001` 运行；Linux 上使用 bind mount 时需保证目录可写：

```sh
mkdir -p ./jt808-data ./jt808-logs
sudo chown -R 10001:10001 ./jt808-data ./jt808-logs
```

### 5.5 配置覆盖与环境变量

配置优先级从高到低：

1. 命令行 `-c <path>` / `--config <path>`；
2. 环境变量 `JT808_CONFIG`；
3. 默认路径 `/etc/jt808/<role>.yaml`（standalone 为 `/etc/jt808/standalone.yaml`）。

因此覆盖配置最直接的方式是把宿主机 YAML 挂载到默认路径：

```sh
docker run -d --name jt808 \
  -v "$PWD/my-standalone.yaml:/etc/jt808/standalone.yaml:ro" \
  -p 8080:8080 -p 8008:8008 \
  -v jt808-data:/app/data -v jt808-logs:/app/logs \
  fakeyanss/jt808-server-go:latest
```

镜像读取的环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `JT808_ROLE` | `standalone` | 运行角色；首个参数优先于该变量 |
| `JT808_CONFIG` | 空 | 配置文件路径；显式 `-c` 优先 |
| `JT808_PROBE_ADDR` | `127.0.0.1:9105` | 健康检查请求的探针地址 |
| `TZ` | `Asia/Shanghai` | 进程时区 |

配置项本身仍以 YAML 为准，环境变量不逐项覆盖 YAML 字段；需要改端口、数据库或集群地址时请挂载完整配置文件。

### 5.6 Compose 示例

```sh
# memory 后端：开箱即跑
docker compose -f deploy/docker-compose.yml up -d

# SQLite 后端：先执行 migrate，再启动主服务
docker compose -f deploy/docker-compose.sqlite.yml up -d
```

两份 compose 都映射 standalone 默认端口并挂载 `jt808-data`、`jt808-logs`。SQLite 示例使用 `deploy/docker/sqlite.yaml`，迁移服务以 `migrate` 角色运行，成功后主服务才启动。

使用 MySQL 时，复制 `deploy/docker/sqlite.yaml`，把 `database` 段改为：

```yaml
database:
  type: "mysql"
  mysql:
    dsn: "jt808:jt808@tcp(mysql:3306)/jt808?parseTime=true&charset=utf8mb4"
```

先执行迁移，再以同一份配置启动：

```sh
docker run --rm \
  -v "$PWD/mysql.yaml:/etc/jt808/migrate.yaml:ro" \
  fakeyanss/jt808-server-go:latest migrate

docker run -d --name jt808-mysql \
  -e JT808_CONFIG=/etc/jt808/mysql.yaml \
  -v "$PWD/mysql.yaml:/etc/jt808/mysql.yaml:ro" \
  -p 8080:8080 -p 8008:8008 \
  -v jt808-data:/app/data -v jt808-logs:/app/logs \
  fakeyanss/jt808-server-go:latest
```

Compose 文件同时声明了 `build`，本地没有镜像时会从源码构建；CI 发布后也可以直接 `docker pull fakeyanss/jt808-server-go:latest`。

### 5.7 集群 Compose 示例

`deploy/docker-compose.cluster.yml` 编排 gateway、control、media、attachment 四个角色，加 MySQL 与共享卷：

```sh
docker compose -f deploy/docker-compose.cluster.yml up -d
docker compose -f deploy/docker-compose.cluster.yml ps
```

启动顺序由 compose 依赖保证：

1. `mysql` 健康检查通过；
2. `shared-volume-init` 在 `jt808-blobs` 卷写入 `.jt808-shared-volume` 标记；
3. `migrate` 使用 `deploy/docker/cluster/control.yaml` 执行迁移，成功退出；
4. `jt-control` 在迁移完成后启动；
5. `jt-signal`、`jt-media`、`jt-attachment` 在 control 启动后启动，并向 control 注册。

只有 `jt-control` 的配置包含 `database`；`jt-signal`、`jt-media`、`jt-attachment` 不配置数据库。迁移使用 control 配置，修改 MySQL DSN 时只需改 `deploy/docker/cluster/control.yaml`。

挂载点：

| 容器路径 | 卷 | 说明 |
|---|---|---|
| `/app/data/blobs` | `jt808-blobs` | 四角色共享的 `sharedVolume` 根目录与标记文件 |
| `/app/data` | `jt808-<角色>-data` | 各角色本地数据、附件暂存、媒体目录 |
| `/app/logs` | `jt808-logs` | 各角色滚动日志 |
| `/etc/jt808/*.yaml` | `deploy/docker/cluster/*.yaml` | 只读角色配置 |
| `/var/lib/mysql` | `jt808-mysql` | MySQL 数据目录 |

角色端口与探针：

| 服务 | 宿主机端口 | 容器内端口 | 探针 | 业务库 |
|---|---|---|---|---|
| `jt-signal` | `8080/tcp`、`8081/udp` | `8080/8081`、内部下行 `9101` | `9106` | 否 |
| `jt-control` | `8008/tcp` | `8008`、内部上行 `9102` | `9108` | MySQL |
| `jt-media` | `7300/tcp`、`7300/udp`、`7301/tcp` | `7300/7301` | `9107` | 否 |
| `jt-attachment` | `7400/tcp`、`7401/udp`、`7500/tcp`、`7501-7510/tcp` | `7400/7401/7402`、FTP `7500/7501-7510` | `9109` | 否 |
| `mysql` | 不发布 | `3306` | `mysqladmin ping` | - |

验证节点注册与迁移：

```sh
docker compose -f deploy/docker-compose.cluster.yml logs migrate | tail -5
docker compose -f deploy/docker-compose.cluster.yml exec mysql \
  mysql -ujt808 -pjt808 jt808 -e \
  "select gateway_id,status from gateway_nodes; select node_id,status from media_nodes; select node_id,status from attachment_nodes;"
```

预期看到 `gateway-1`、`media-1`、`attachment-1` 均为 `online`，迁移日志包含 `applied=1`。示例中的 MySQL 密码、`cluster.gateway.token`、`cluster.control.token`、`uplink.http.token` 仅用于本地联调，部署前必须替换。

容器内 `mediaServer.advertiseIp`、`alarmServer.uploadIp`、`cluster.attachment.ftp1078.advertiseIp` 目前是 `127.0.0.1`，只验证容器间启动与注册；终端级冒烟需要改成宿主机可达地址并发布对应端口。

扩容角色时不要直接对同一服务执行 `--scale`：同一份 YAML 会复用 `nodeId` 与 `advertiseUrl`，副本会互相覆盖注册。正确做法是为副本准备独立配置与端口：

- **gateway**：复制 `signal.yaml`，改 `cluster.gateway.nodeId` 和 `advertiseUrl`，新增一个 compose 服务并映射不同宿主机端口。control 使用 `directory` 路由，会按 `gateway_nodes` 自动发现新网关。
- **media**：复制 `media.yaml`，改 `cluster.media.nodeId` 和 `advertiseUrl`，共享 `jt808-blobs`，新增独立媒体数据卷与端口；可在 control 的 `cluster.media.nodes` 增加静态兜底节点。
- **attachment**：复制 `attachment.yaml`，改 `cluster.attachment.nodeId`、`advertiseUrl` 和 FTP 端口，共享 `jt808-blobs`，使用独立暂存卷；同时把新节点加入 control 的 `cluster.attachment.nodes`。
- **control**：control 的上行邮箱是进程内结构，日志明确提示单副本推荐；不要直接水平扩容，需单独评估 outbox、节点注册与下行路由的一致性。

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
