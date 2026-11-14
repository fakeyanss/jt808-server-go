# 业务 API

业务 API 挂在 `server.port.httpPort`（默认 `8008`），公共前缀 `/api`。所有接口在 `api.auth.token` 非空时都要求：

```http
Authorization: Bearer <token>
```

同时按来源 IP 做令牌桶限流，变更类请求与鉴权失败会写入审计日志。下行命令接口支持 `Idempotency-Key` 请求头（最长 128 字符）：同一键重复提交返回已有命令记录，不会二次下发。

## 1. 约定

- 请求与响应均为 JSON，文件下载与录像切片接口除外。
- 下行命令成功受理返回 `202`，幂等命中返回 `200`；响应中的 `commandId` 可用于 `GET /api/commands/:commandId` 查询最终状态。
- 命令状态：`pending`（待投递/待应答）、`acked`（终端已确认）、`failed`（达到重试上限或过期）。
- 终端上报的 BCD 时间统一按固定 GMT+8 解析为真实瞬时值，不随服务器时区变化。
- 业务 API 中不带时区的时间参数（如 `start`/`end`、`startTime`/`endTime`）按同一 GMT+8 口径解释；播放与录像接口使用 RFC3339。
- 管理面 `/locations`、`/alarms` 的 `from`/`to` 使用带时区的 RFC3339，不受上述规则影响。

## 2. 查询接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/commands/:commandId` | 查询下行命令状态；outbox 关闭时返回 404 |
| GET | `/api/device` | 列出已注册设备 |
| GET | `/api/device/:phone/reports/:kind` | 查询行业上报最新快照 |
| GET | `/api/device/:phone/geo` | 设备档案与最新定位 |
| GET | `/api/device/:phone/params` | 本地保存的终端参数 |
| GET | `/api/device/:phone/properties` | 本地保存的终端属性 |
| GET | `/api/device/:phone/location` | 实时查询位置：下发 `0x8201`，等待 `0x0201`，10 秒超时 |
| GET | `/api/device/:phone/alarms` | 查询报警记录 |

`reports/:kind` 支持：

| kind | 内容 |
|---|---|
| `driver` | 驾驶员身份 |
| `can` | CAN 总线 |
| `recorder` | 记录仪数据 |
| `media-event` | 多媒体事件 |
| `media-capture` | 拍摄应答 |
| `media-search` | 存储多媒体检索应答 |
| `media-uploads` | 多媒体上传记录 |
| `transparent` | 上行透传 |
| `peripheral` | 外设/透传（与 `transparent` 同源） |
| `upgrade-result` | 终端升级结果 |

无记录时返回 `404 {"err":"no report received"}`。

## 3. 下行控制

| 方法 | 路径 | 对应消息 |
|---|---|---|
| POST | `/api/device/:phone/control` | `0x8105` 终端控制 |
| PUT | `/api/device/:phone/params` | `0x8103` 设置终端参数 |
| POST | `/api/device/:phone/regions/delete` | 删除区域/路线 |
| POST | `/api/device/:phone/regions/set` | 设置区域/路线 |
| POST | `/api/device/:phone/regions/query` | 查询区域/路线 |
| POST | `/api/device/:phone/driver/query` | `0x8702` 司机身份查询 |
| POST | `/api/device/:phone/recorder/command` | `0x8700` / `0x8701` 记录仪命令 |
| POST | `/api/device/:phone/media/capture` | `0x8801` 摄像头立即拍摄 |
| POST | `/api/device/:phone/media/search` | `0x8802` 存储多媒体检索 |
| POST | `/api/device/:phone/media/resources` | `0x9205` 音视频资源列表 |
| POST | `/api/device/:phone/media/command` | `0x8803` / `0x8804` / `0x8805` 上传与录音控制 |
| POST | `/api/device/:phone/text/issue` | `0x8300` 文本信息下发 |
| POST | `/api/device/:phone/events/set` | `0x8301` 事件设置 |
| POST | `/api/device/:phone/vehicle/control` | `0x8500` 车辆控制 |
| POST | `/api/device/:phone/question/issue` | `0x8302` 提问下发 |
| POST | `/api/device/:phone/info/menu/set` | `0x8303` 信息菜单设置 |
| POST | `/api/device/:phone/info/service` | `0x8304` 信息服务 |
| POST | `/api/device/:phone/call/callback` | `0x8400` 电话回拨 |
| POST | `/api/device/:phone/phonebook/set` | `0x8401` 电话本设置 |
| POST | `/api/device/:phone/terminal/upgrade` | `0x8108` 终端升级 |
| POST | `/api/device/:phone/transparent/down` | `0x8900` 数据下行透传 |
| POST | `/api/device/:phone/alarms/confirm` | `0x8203` 人工确认报警 |

请求体字段直接复用协议模型，字段定义见 `internal/protocol/model/`，路由与校验见 `internal/api/`。

文本下发示例：

```sh
curl -s -X POST http://localhost:8008/api/device/13000000000/text/issue \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $API_TOKEN" \
  -H 'Idempotency-Key: demo-1' \
  -d '{"flag":0,"textType":1,"text":"hello"}'
```

```json
{
  "commandId": "0f8c2f6d9a1e4b7c8d3e5f60718293a4",
  "duplicate": false,
  "messageId": "0x8300"
}
```

## 4. 多媒体文件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/device/:phone/media/files` | 查询 `0x0801` 文件；参数 `channel`、`type`、`start`、`end`、`alarmOnly`、`limit` |
| GET | `/api/device/:phone/media/files/:mediaId` | 单个文件元数据 |
| GET | `/api/device/:phone/media/files/:mediaId/content` | 下载文件内容，支持 HTTP Range |
| GET | `/api/device/:phone/media/captures` | `0x0805` 拍摄记录及各 mediaId 的文件到达状态 |
| GET | `/api/device/:phone/media/search-results` | `0x0802` 检索应答，参数 `limit` |

`0x0801` 位置体只有报警标志位、没有报警流水号，因此 `alarmSign` 与 `alarmOnly` 只能表达/筛选报警类别，不能与某一条具体报警精确关联。

## 5. 播放与录像

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/device/:phone/playback` | 申请实时播放或终端回放会话，并下发 `0x9101`/`0x9201` |
| DELETE | `/api/playback/:sessionId` | 停止会话并下发 `0x9102`/`0x9202`；幂等 |
| GET | `/api/device/:phone/recordings/coverage` | 查询平台录像覆盖区间；参数 `start`、`end`（RFC3339）、`channel` |
| GET | `/api/device/:phone/recordings/:segmentId/content` | 读取录像切片，支持单段 `Range` |

申请播放：

```sh
curl -s -X POST http://localhost:8008/api/device/13000000000/playback \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $API_TOKEN" \
  -d '{"channel":1,"kind":"live","ttlSeconds":600}'
```

```json
{
  "messageId": "0x9101",
  "sessionId": "ms-1",
  "channel": 1,
  "kind": "live",
  "endpoints": [{"protocol": "flv", "url": "http://127.0.0.1:7301/media/ms-1.flv"}],
  "playbackUrl": "http://127.0.0.1:7301/media/ms-1.flv",
  "expiresAt": "2026-11-14T15:30:00+08:00",
  "commandId": "…",
  "duplicate": false
}
```

终端回放需要额外传 `kind=terminal_playback`、`startTime`、`endTime`（RFC3339）以及 `mediaType`、`streamType`、`storageType`、`playbackMode`、`playbackSpeed`。`playbackMode > 4` 或 `playbackSpeed > 5` 返回 400。

错误与冲突：

| 状态 | 场景 |
|---|---|
| 409 `session_conflict` | 同一 `(phone, channel)` 已有会话；`existing` 带已有 sessionId、kind 与过期时间 |
| 503 `no_media_node_configured` | 注册表与静态清单都为空 |
| 503 `no_media_node_available` | 节点全部不可达、满载或创建失败 |
| 503 `media playback is not available on this role` | 当前进程没有媒体能力 |

覆盖查询在区间不完整时仍返回 `200`，响应包含 `requested`、`available`、`missing`、`playable`、`segments`，不是 409。

## 6. 附件与 1078 FTP

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/device/:phone/attachments/upload` | 下发 `0x9208` 报警附件上传指令 |
| GET | `/api/device/:phone/attachments` | 列出已接收附件及进度 |
| GET | `/api/device/:phone/attachments/:fileName` | 查询单个附件元数据 |
| GET | `/api/device/:phone/attachments/:fileName/content` | 下载附件内容（仅已完成附件） |
| POST | `/api/device/:phone/ftp1078/upload` | 下发 `0x9206`，登记 FTP 上传任务 |
| POST | `/api/device/:phone/ftp1078/control` | 下发 `0x9207`，暂停/继续/取消 |
| GET | `/api/device/:phone/ftp1078/task/:serial` | 查询 FTP 上传任务状态 |

FTP 上传请求示例：

```json
{
  "uploadPath": "video/20261114/channel1.mp4",
  "channel": 1,
  "startTime": "2026-11-14T10:00:00+08:00",
  "endTime": "2026-11-14T10:30:00+08:00",
  "alarmSign": 0,
  "mediaType": 0,
  "streamType": 0,
  "storageType": 0,
  "taskCondition": 0
}
```

控制请求体 `{"serial": 12, "action": 0}`，`action` 取值：`0` 暂停、`1` 继续、`2` 取消。FTP 未启用时相关接口返回 503；没有可用附件节点时返回 `no_attachment_node_configured` / `no_attachment_node_available`。

## 7. 管理面

管理面独立于业务 API，默认不启动。开启后监听 `admin.addr`，除探针外要求 Bearer token：

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/healthz` | 否 | 存活探针 |
| GET | `/readyz` | 否 | 就绪探针，失败返回 503 |
| GET | `/ping` | 是 | 连通性检查 |
| GET | `/metrics` | 是 | Prometheus 文本指标 |
| GET | `/sessions` | 是 | 当前实例会话列表 |
| GET | `/sessions/:phone` | 是 | 当前实例会话详情 |
| DELETE | `/sessions/:phone` | 是 | 强制断开当前实例上的会话 |
| GET | `/locations` | 是 | 历史分页查询，必须带 `phone`、`from`、`to` |
| GET | `/alarms` | 是 | 历史分页查询，必须带 `phone`、`from`、`to` |

会话类接口只作用于当前进程；集群下跨实例无效。`/locations` 在 `memory` 后端不可用。

## 8. 相关文档

- [配置说明](configuration.md)
- [架构与集群模式](architecture.md)
- [运维手册](operations.md)
- [安装与部署](installation.md)
