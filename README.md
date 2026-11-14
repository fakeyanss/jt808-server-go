# jt808-server-go

[![Go Version](https://img.shields.io/github/go-mod/go-version/fakeyanss/jt808-server-go)](https://github.com/fakeyanss/jt808-server-go/blob/main/go.mod)
[![CI](https://img.shields.io/github/actions/workflow/status/fakeyanss/jt808-server-go/ci.yml?branch=main&label=CI)](https://github.com/fakeyanss/jt808-server-go/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/fakeyanss/jt808-server-go)](https://github.com/fakeyanss/jt808-server-go/blob/main/LICENSE)
[![Stars](https://img.shields.io/github/stars/fakeyanss/jt808-server-go?style=social)](https://github.com/fakeyanss/jt808-server-go/stargazers)

> JT/T 808 / JT/T 1078 / 苏标主动安全的终端接入服务。一套协议内核，支持 Standalone 单进程与 Cluster 多角色部署。

## 项目定位

jt808-server-go 是一个用 Go 编写的车载终端接入服务，面向自建车辆监管平台、设备接入网关和协议联调工具的使用者。

它把终端的注册鉴权、位置与报警、主动安全附件、音视频信令与媒体数据收敛到一套协议内核里，业务数据可按部署规模选择内存、SQLite 或 MySQL。接入量小时，一个进程即可接终端；接入量增长后，可按角色拆成 808 网关、管控、1078 媒体和附件节点，协议内核不变。

项目不提供前端控制台，只提供 JSON API；业务界面、账号体系与租户模型由使用者自建。

## 项目特点

- **协议内核与部署形态解耦**：同一套编解码、分包、应答配对与业务处理，既能在 Standalone 单进程运行，也能按角色拆分。
- **三版本 808 接入**：兼容 JT/T 808-2011/2013/2019 的消息头与消息体差异，按版本选择字段宽度与编码方式。
- **主动安全闭环**：接收苏标 `0x1210/0x1211/0x1212` 与文件码流，支持补传、落盘、对象存储、进度查询与文件下载。
- **1078 信令与媒体**：覆盖实时传输、终端回放、资源查询与 FTP 上传信令；独立 UDP/TCP 端口接收 RTP，还原 H.264 与 G.711A/U、ADTS AAC；支持 HTTP-FLV 播放、RTMP 转推、平台录像切片与覆盖查询。
- **集群角色拆分**：`jt-signal`、`jt-control`、`jt-media`、`jt-attachment`、`jt-server-standalone` 五个二进制，固定角色、独立探针；控制面独占元数据，数据面不连业务库。
- **会话目录与下行路由**：网关通过 `0x0102` 向 control claim 会话，`directory` 模式按手机号定位归属网关，跨节点下行不再依赖固定入口。
- **可选的可靠性语义**：上行按消息分类选择 `best_effort` 或 `at_least_once`；下行 outbox 支持幂等、租约、退避、过期与重投，语义为 at-least-once。
- **可验证的接入链路**：自带终端模拟器，支持分包乱序、丢片、异常帧注入与 UDP 接入；`make smoke` 用真实二进制跑端到端场景。

## 功能清单

| 能力域 | 状态 | 说明 |
|---|---|---|
| JT/T 808 接入 | 已实现 | 注册鉴权、心跳、位置/报警、参数与属性、文本/事件/提问、电话本、车辆控制、区域与路线、记录仪、司机身份/CAN/运单、多媒体、透传与压缩上报；注册表当前 82 个 MsgID |
| 协议版本 | 已实现 | 消息头区分 2019 与非 2019；2011 仅在 `0x0100` 注册时按消息体长度启发式识别，其余按 2013 处理 |
| 苏标主动安全 | 已实现 | `0x1210/0x1211/0x1212` 信令、`0x30/0x31/0x63/0x64` 码流帧、TCP/UDP 附件服务器、补传、保留期清理、元数据与文件下载 |
| 1078 信令 | 已实现 | `0x9101/0x9102/0x9105/0x9201/0x9202/0x9205/0x9206/0x9207/0x1205/0x1206`，含实时传输、终端回放、资源查询、FTP 上传控制 |
| 1078 媒体 | 已实现 | RTP over UDP/TCP、PS 解复用、H.264 Annex B、G.711A/U、ADTS AAC；HTTP-FLV 播放、RTMP 转推、MPEG-TS 切片录制 |
| 集群模式 | 已实现 | 四角色拆分、会话目录、`fixed`/`directory` 下行路由、媒体节点注册与负载分配、附件节点调度、共享对象存储、时钟偏差观测 |
| 存储后端 | 已实现 | `memory` 开箱即用；SQLite 单机持久化；MySQL 多实例共享；SQL 迁移由独立工具执行 |
| 下行 outbox | 已实现 | `off`/`memory`/`durable` 三档，幂等键、命令状态、租约、退避、过期与重投 |
| 上行可靠性 | 已实现 | 按定位、批量定位、报警等分类选择先应答或先落库 |
| 业务 API | 已实现 | 设备、位置、参数、报警、媒体、附件、播放会话、录像覆盖与 FTP 上传等 JSON 接口 |
| 管理面与可观测性 | 已实现 | `/healthz`、`/readyz`、Prometheus 指标、会话运维、历史分页、审计日志、保留期清理 |
| 终端模拟器 | 已实现 | 注册鉴权、心跳、定位、全量上行、分包乱序/丢片、异常帧注入、UDP 接入 |
| 前端控制台 | 不提供 | 只提供 JSON API，界面与账号体系由使用者自建 |
| 媒体转码 / 内置分发 | 不提供 | 不做转码；分发依赖 HTTP-FLV 客户端或外部 RTMP 服务器（SRS/lal 等） |

## 已知边界与未实现项

以下按当前 `main` 代码的实际行为列出，不要按“规划能力”理解：

| 领域 | 边界 |
|---|---|
| 808 版本 | 消息头无法区分 2011/2013；2011 仅在 `0x0100` 注册时按长度启发式识别 |
| 808 消息 | 注册表含 82 个 MsgID，同时包含服务端上行、平台下行与模拟器行为；并非全部已完整闭环，逐条状态见[协议覆盖](docs/protocol-coverage.md)。RSA 公钥交换（`0x8A00`/`0x0A00`）缺失；`0x8105`、`0x8702`、`0x9102`、`0x9202` 只实现基础链路 |
| 808 扩展 | 保留字段与厂商自定义消息没有通用分发；2019 年第 1 号修改单未做符合性评估 |
| 1078 媒体 | 视频限 H.264，不支持 H.265；不做转码。播放输出为 HTTP-FLV 或转推 RTMP，仓库不自带 RTMP 服务器；集群默认 `record.mode=off`，平台录像需显式开启 `segmented` |
| 1078 故障 | 媒体节点掉线或过期不迁移既有流，终端需重连后由新分配接管；无播放器 SDK/页面 |
| 苏标 | 支持附件接收、补传、下载与 FTP 录像上传；未实现粤标 `0x1FC4`，厂商扩展按原始或结构化模型处理，不做通用分发 |
| `memory` 后端 | 重启丢失全部业务数据；不保存定位历史，管理面 `/locations` 返回 503；outbox 默认 `memory`，重启丢失 pending 命令；`retention` 不生效 |
| 定位投影 | 最新定位 `Geo` 始终在进程内；SQL 后端重启后需等下一次上报才恢复，SQL 只持久化定位历史 |
| 集群存储 | 需要所有角色可见的共享卷（`sharedVolume`）或 S3 兼容对象存储；`sharedVolume` 只校验标记文件，不校验卷的真实隔离性 |
| 集群数据库 | control 使用共享 MySQL；`cluster.allowEmbeddedStorage` 仅限本地联调 SQLite，生产禁止开启 |
| 集群会话 | `session_directory` 没有 per-phone TTL，会话有效性由网关节点在线状态推出；网关宕机 30 秒后其会话不可路由 |
| 集群多实例 | 同一 `gatewayId` 多进程同时在线会互相顶替；control 以单写者维护媒体/附件节点注册表 |
| 投递保证 | 下行 outbox 为 at-least-once，可能重复执行；上行 `best_effort` 可能丢一条，`at_least_once` 依赖终端重传，不是 exactly-once |
| 附件 Hook | 生命周期回调尽力而为：队列满即丢弃，HTTP 失败只记日志，不重试、不保证送达 |
| 安全 | TCP/UDP 接入、业务 API、管理面、集群内部 HTTP 均未内建 TLS；业务 API 鉴权可关闭；管理面默认关闭，开启后要求 token |

## 架构概览

消息处理按“接入 → 协议 → 业务 → 存储/投递”分层，TCP 与 UDP 复用同一条 Pipeline，每个会话一个协程串行处理：

<img src="docs/.asset/pipeline_component.png" alt="消息处理组件" width="760">

Standalone 下所有角色同进程，零外部依赖；Cluster 下拆成四个角色，控制面独占业务库与元数据，数据面只持有连接、媒体流与附件暂存：

| 角色 | 二进制 | 职责 | 是否连业务库 |
|---|---|---|---|
| 808 网关 | `jt-signal` | 808 接入、编解码、分包、应答配对、上行投递与下行契约入口 | 否 |
| 管控 | `jt-control` | 业务处理、API、outbox、会话目录、媒体/附件节点调度、保留期清理 | 是（MySQL） |
| 1078 媒体 | `jt-media` | RTP 接入、播放会话、录像切片与对象存储写入 | 否 |
| 苏标附件 | `jt-attachment` | 附件 TCP/UDP 接入、暂存、切片上传、进度上报、1078 FTP | 否 |
| 单机 | `jt-server-standalone` | 上述全部角色同进程 | 可选 SQLite/MySQL |

更完整的模块边界、状态归属、集群故障语义见[架构与集群模式](docs/architecture.md)。

## 快速开始

前置条件：Go 1.26+、`make`、`bash`。默认配置使用 `memory` 后端，不需要数据库。

```sh
# 终端 1：编译并启动服务（内嵌默认配置）
make compile && make run

# 终端 2：启动自带模拟终端，完成注册、鉴权、心跳与位置上报
make run-client

# 终端 2：查出一条已注册设备
curl -s http://localhost:8008/api/device
```

设备列表中出现模拟终端的手机号，说明接入、协议处理与查询链路已打通。默认端口与开关：

| 入口 | 默认值 | 说明 |
|---|---|---|
| 终端接入 TCP/UDP | `:8080` / `:8081` | 808/1078 信令主连接 |
| 业务 API | `:8008` | `/api`，鉴权默认关闭 |
| 苏标附件 | `:7200` / `:7201/udp` | 默认开启 |
| 1078 RTP | 未启用 | 配置 `mediaServer.port`/`tcpPort` 后开启 |
| 管理面 | 未启用 | `admin.enable: true` 且配置 token 后监听 `127.0.0.1:9000` |

## 安装与运行

```sh
make compile            # target/debug/jt808-server-go（通用入口，按配置决定形态）
make release            # 交叉编译 darwin/linux amd64+arm64
make compile-migrate    # 数据库迁移工具
make compile-client     # 终端模拟器

# 按角色构建生产二进制
go build -o bin/jt-server-standalone ./cmd/jt-server-standalone
go build -o bin/jt-signal           ./cmd/jt-signal
go build -o bin/jt-control          ./cmd/jt-control
go build -o bin/jt-media            ./cmd/jt-media
go build -o bin/jt-attachment       ./cmd/jt-attachment
```

```sh
# 单机运行
./target/debug/jt808-server-go -c configs/default.yaml

# 数据库迁移（SQLite/MySQL 首次部署或升级后）
./target/debug/jt808-migrate -c configs/default.yaml
```

Docker 与 compose：

```sh
make dockerbuild
docker compose -f deploy/docker-compose.yml up --build
```

集群样例配置在 `configs/cluster/`，五个角色共用同一套共享存储与 MySQL。端口规划、启动顺序、迁移、备份恢复与故障排查见[安装部署](docs/installation.md)和[运维手册](docs/operations.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [安装部署](docs/installation.md) | 前置条件、编译、单机与集群部署、Docker、数据库迁移、端口规划 |
| [配置说明](docs/configuration.md) | 存储、接入、可靠性、附件、媒体、集群、探针与日志配置项 |
| [架构与集群模式](docs/architecture.md) | 消息流转、模块职责、状态归属、角色拆分、会话目录、节点调度与故障语义 |
| [协议覆盖](docs/protocol-coverage.md) | 808 逐消息状态、1078/苏标信令清单、注册表自动生成段 |
| [业务 API](docs/api.md) | 设备、位置、参数、报警、媒体、附件、播放与 FTP 接口 |
| [运维手册](docs/operations.md) | 探针、指标、保留期、备份恢复、回滚、压测基线与常见问题 |
| [贡献指南](CONTRIBUTING.md) | 分支、提交、检查命令、协议消息开发与迁移规范 |
| [安全策略](SECURITY.md) | 漏洞报告渠道与部署安全边界 |
| [历史文档](docs/archive/) | 早期设计、路线图与生态调研，仅作归档，不再维护 |

## 贡献者与致谢

感谢所有提交 Issue、Pull Request 和协议实测反馈的贡献者，完整名单见 [Contributors](https://github.com/fakeyanss/jt808-server-go/graphs/contributors)。

协议实现参考了以下公开标准与资料：

- JT/T 808-2011/2013/2019、JT/T 1078-2016、T/JSATL 12-2017、GB/T 19056-2021，标准文本见 `docs/协议文档/`；
- 生态调研参考了 go-jt808、xtream-codec、SmallChi 等开源项目的公开实现与讨论，见[归档的生态调研](docs/archive/jt808-ecosystem-research.md)。

欢迎通过 Issue 反馈协议联调结果；如果这个项目对你有帮助，欢迎 Star 和 Fork。

## 许可证

[MIT](LICENSE)
