# 行业协议 API 与模拟器

HTTP API 向在线设备下发 JT808 指令。异步命令返回
`202 Accepted`；产生的终端上报可通过
`GET /device/:phone/reports/:kind` 读取。

## 区域与路线管理

- `POST /device/:phone/regions/set` 接受 `messageId`（`0x8600`、`0x8602`、
  `0x8604` 或 `0x8606`）和 `bodyHex`。`bodyHex` 是标准 JT808 消息体，
  不含 JT808 消息头。服务端校验设置属性和非零条目数后原样发送。
- `POST /device/:phone/regions/delete` 接受 `messageId`（`0x8601`、`0x8603`、
  `0x8605` 或 `0x8607`）和一个 `ids` 数组。空数组表示删除该类型的全部对象；
  单条消息最多支持 125 个 ID。
- `POST /device/:phone/regions/query` 接受 `queryType`（1 圆形、2 矩形、
  3 多边形、4 路线）和可选的 `ids`。最多等待 10 秒以获得 `0x0608`。
  响应封装与类型相关记录（圆形、矩形、多边形、路线）都会被解码为
  类型化结果。模拟器从由此前设置/删除命令填充的
  每设备区域/路线目录中应答。

## 司机、CAN 与行驶记录仪

- `POST /device/:phone/driver/query` 发送 `0x8702`。
- `POST /device/:phone/recorder/command` 接受 `messageId`（`0x8700` 或
  `0x8701`）、`command`，以及可选的 `dataHex`（GB/T 19056 数据块）。
- 用 `GET /device/:phone/reports/driver`、`/can` 或
  `/recorder` 读取最新报告。

客户端模拟器对这些请求返回合成的司机与记录仪数据。
通过 `go run ./cmd/jt808-client --industry-reports`，它可以在鉴权后发送一条
示例司机、CAN、记录仪、多媒体事件和多媒体上传报告。

## 多媒体

- `POST /device/:phone/media/capture` 接受 `0x8801` 命令字段并异步返回；
  模拟器以 `0x0805` 应答。
- `POST /device/:phone/media/search` 接受 `0x8802` 的媒体类型、通道、
  事件、开始时间与结束时间。模拟器过滤其内存上传目录，
  返回携带类型化媒体元数据与定位数据的 `0x0802` 匹配项。
  全零的开始/结束时间表示不做时间过滤。
- `POST /device/:phone/media/command` 以 `bodyHex` 发送
  `0x8803`、`0x8804` 或 `0x8805` 的标准消息体；
  每个消息体在发送前都会按其类型化命令字段解码并校验。
- 用 `/reports/media-event`、`/reports/media-capture`、
  `/reports/media-search` 和 `/reports/media-uploads` 读取收到的报告。

上传处理器解码 8 字节媒体描述、28 字节基础定位报告和剩余的
媒体分片字节。内存缓存每设备最多保留 100 条上传，
并忽略单个大于 1 MiB 的分片。媒体负载与报告尚未持久化。
`0x8800` 包 ID 重传是双向实现的：客户端模拟器保留已发送帧并重放
被请求的 ID；服务端在空闲超时后检测缺失的媒体包并发出
`0x8800` 请求（当携带媒体 ID 的首个分片缺失时降级为 `0x8003`）。
`0x0802` 结果解码器解析每条标准的 35 字节媒体记录。
