# JT/T 808 与 JT/T 1078 开源生态深度调研

> 调研对象：`yezhihao/jt808-server`、`SmallChi/JT808`、`SmallChi/JT808Gateway`、`SmallChi/JT1078`
> 对照对象：`/Users/fakeyanss/code/jt808-server-go`（Go 实现的 JT808-2019 服务端，只读核查，未做任何修改与 git 操作）
> 调研时间：2026-11-02

---

## 0. 调研方法与可信度声明

**方法**：四个仓库均通过 GitHub REST API 拉取递归目录树，再按文件从 `raw.githubusercontent.com` / `cdn.jsdelivr.net` 下载源码到 `/tmp/jt808-research/<repo>/files/`，逐个读 README、`pom.xml`/`csproj`、核心类源码。下载完成度：

| 仓库 | 已下载源码/文档文件数 | 说明 |
| --- | --- | --- |
| yezhihao/jt808-server | 195 | 全量（含 README、pom、application.yml、全部 Java 源） |
| SmallChi/JT808 | 670 | 全量（含全部消息体、4 个协议扩展包） |
| SmallChi/JT808Gateway | 219 | 全量（含 simples、Benchmark、Tests） |
| SmallChi/JT1078 | 294 | 全量（已排除 `doc/video`、`doc/tools` 等二进制大文件） |
| yezhihao/protostar（外部依赖） | 52 | 全量 |
| yezhihao/netmc（外部依赖） | 59 | 全量 |

**元数据核实**（GitHub API，2026-11-02 抓取）：

| 仓库 | Star | Fork | 语言 | License | 最近 push | 归档 | 体积 | 创建 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| yezhihao/jt808-server | **829** | 311 | Java | Apache-2.0 | 2026-11-02 | 否 | 11.3 MB | 2019-04-09 |
| SmallChi/JT808 | **613** | 277 | C# | MIT | 2026-09-16 | 否 | 6.3 MB | 2018-10-13 |
| SmallChi/JT808Gateway | **200** | 135 | C# | MIT | 2026-09-07 | 否 | 9.3 MB | 2018-10-16 |
| SmallChi/JT1078 | **234** | 150 | C# | MIT | **2022-12-31** | 否 | 91.1 MB | 2019-07-01 |
| yezhihao/protostar | 43 | — | Java | — | 2026-06-23 | 否 | 211 KB | — |
| yezhihao/netmc | 79 | — | Java | — | 2026-06-23 | 否 | 128 KB | — |

**可信度声明**：
1. 所有结论以「我读到的源码」为准，README 声称但代码里没有的，一律在文中标注「README 与代码不符」。
2. 我没有运行这四个项目（Java 需 Maven 拉依赖、C# 需 NuGet 还原，本机网络受限），因此**运行时行为（吞吐、内存、并发正确性）没有实测数据**，相关判断为静态阅读推断，文中会标注「推断」。
3. SmallChi 三个仓库均为「协议库 + 网关骨架」，不包含任何数据库、Redis、流媒体落地实现——这不是缺陷，是定位，但容易被 README 的高 star 误导为「开箱即用的平台」。文中会反复强调这一点。

---

## 1. 速览：一句话定位

| 项目 | 真实定位 | 能直接拿来用吗 | 最值得看的部分 |
| --- | --- | --- | --- |
| yezhihao/jt808-server | **服务端 + 协议 SDK**（Spring Boot + Netty；1078 仅信令；苏标附件服务器） | 编解码 SDK 可；服务端只能当脚手架（业务层是演示骨架） | 注解式编解码模型、苏标附件服务器（分片落盘 + `.log` 完整性校验）、`Session.request` 请求-应答关联 |
| SmallChi/JT808 | **纯协议编解码 SDK**（无任何网络/会话/服务端代码） | 可以，作为编解码库 | 消息 ID 工厂 + 外部程序集注册 + 附加信息子工厂（`IJT808_0x0200_Factory`）、`IJT808Config` 可替换策略 |
| SmallChi/JT808Gateway | **网关骨架**（Socket + `System.IO.Pipelines`；Kafka 解耦；无分包重组、无鉴权） | 可作为「网关 + MQ」架构参考，不建议直接用 | 双索引会话表 + 同号踢旧连接、Producer/Consumer 抽象与 Kafka 落地方案、WebApi 统一下发 |
| SmallChi/JT1078 | **RTP/PS 解析 + H264 解析 + FLV/HLS/fMP4 封装库**（不是媒体服务器，无 808 信令） | 协议解析可，媒体服务要自己搭 | `JT1078Package` 字段模型、信令字段条件序列化、H264 NALU 状态机、FLV/TS 封装 |

---

## 2. yezhihao/jt808-server

### 2.1 设计目标与定位

README 的卖点（原文归纳）：

- 基于 **Netty**，TCP/UDP 同一端口；
- **Spring WebFlux** 高并发 Web 接口；
- **不依赖 Spring 也能独立运行**，编解码模块可下沉到 Android；
- **注解描述协议**（`@Message` / `@Field`），协议即代码；
- `@AsyncBatch` 批量入库、**报文解释器**、全覆盖单元测试；
- 同时支持 **JT808-2011/2013/2019**、**JT/T 1078**（信令）、**苏标 T/JSATL 12-2017**、**粤标 T/GDRTA 002-2020**。

**关键结构事实（README 没写，但决定了怎么读这个仓库）**：4.x 版本已经把网络层与编解码框架**外置**成两个独立 Maven 包，仓库本体只剩协议定义与业务示例。

`jtt808-protocol/pom.xml` 中的关键依赖：

```xml
<dependency>
    <groupId>io.github.yezhihao</groupId>
    <artifactId>protostar</artifactId>   <!-- 注解驱动序列化 -->
    <version>4.0.6</version>
</dependency>
<dependency>
    <groupId>io.github.yezhihao</groupId>
    <artifactId>netmc</artifactId>       <!-- Netty + Dispatcher + Session -->
    <version>4.0.6</version>
</dependency>
```

也就是说：
- 仓库里**没有** `SessionManager` 的实现、**没有** `DispatcherHandler`、**没有** 帧解码器；
- 仓库里只有 `org.yzh.protocol`（消息定义 + `JTMessage` 基类 + `JTMessageAdapter` 适配器）和 `org.yzh.web`（Spring Boot 示例）。

**维护状态**：829 star，最近 push 就在今天（2026-11-02），open issues 仅 2 个，说明作者仍在高频维护、issue 处理积极。这是四个项目里维护状态最好的。

### 2.2 工程结构

```
jtt808-server/
├── jtt808-protocol/                       # 协议定义 + 编解码（可独立发布）
│   └── src/main/java/org/yzh/protocol/
│       ├── basics/JTMessage.java           # 消息基类 + 位运算
│       ├── codec/JTMessageDecoder.java     # 转义/校验/分包合并
│       ├── codec/JTMessageEncoder.java
│       ├── codec/JTMessageAdapter.java     # 实现 netmc 的 Encoder/Decoder
│       ├── codec/MultiPacket.java          # 分包容器
│       ├── codec/MultiPacketDecoder.java   # 分包缓存 + 超时清理
│       ├── codec/DataFrameMessageDecoder.java  # 苏标附件数据帧（0x30316364）
│       ├── commons/JTUtils.java            # BCC / headerLength
│       ├── commons/MessageId.java          # ID ↔ 类名注册表
│       ├── t808/ …  t1078/ …  jsatl12/ …   # 各版本消息体
│       └── yuebiao/ …
├── jtt808-server/                          # Spring Boot 3 + WebFlux 示例
│   └── src/main/java/org/yzh/web/
│       ├── config/JTConfig.java            # 3 个 Netty Server Bean
│       ├── config/JTProperties.java        # 配置前缀 jt-server.jt808
│       ├── endpoint/JT808Endpoint.java     # @Endpoint + @Mapping 分发
│       ├── endpoint/JT1078Endpoint.java
│       ├── endpoint/JSATL12Endpoint.java   # 苏标附件
│       ├── controller/JT1078Controller.java # HTTP 下发信令
│       ├── service/FileService.java        # 附件/媒体写盘
│       └── endpoint/MessageManager.java    # 同步等待应答
└── commons/
```

### 2.3 协议编解码（重点）

#### 2.3.1 注解 + 反射生成 Schema

消息体不是手写 `decode`，而是**注解描述**：

```java
@Message(JT808.终端注册)      // 0x0100
public class T0100 extends JTMessage {

    @Field(length = 2, desc = "省域ID")                       private int provinceId;
    @Field(length = 2, desc = "市县域ID")                     private int cityId;

    // 同一个字段在三个版本下的不同长度：-1=2011/2013(老版式), 0=2013, 1=2019
    @Field(length = 5,  desc = "制造商ID", version = {-1, 0}) private String makerId;
    @Field(length = 11, desc = "制造商ID", version = 1)       private String makerId;

    @Field(length = 8,  desc = "终端型号", version = -1)      private String deviceModel;
    @Field(length = 20, desc = "终端型号", version = 0)       private String deviceModel;
    @Field(length = 30, desc = "终端型号", version = 1)       private String deviceModel;

    @Field(length = 7,  desc = "终端ID", version = {-1, 0})   private String deviceId;
    @Field(length = 30, desc = "终端ID", version = 1)         private String deviceId;

    @Field(length = 1, desc = "车牌颜色：0.未上车牌 1.蓝色 2.黄色 3.黑色 4.白色 9.其他")
    private int plateColor;

    /** 车牌颜色为0时表示车辆VIN，否则为公安交管部门颁发的号牌 */
    @Field(desc = "车辆标识")                                  private String plateNo;

    /** 关键：用「消息体长度 < 37」反推是 2011 版 0x0100（无协议版本号字段可读，只能靠长度猜） */
    @Override
    public int getProtocolVersion() {
        int bodyLength = getBodyLength();
        if (bodyLength > 0 && bodyLength < 37)
            return -1;
        return super.getProtocolVersion();
    }
}
```

三个要点：

1. **同一字段可以写多个 `@Field`**（`@Repeatable`），靠 `version = {-1, 0}` / `version = 1` 分流。`version` 的取值直接对应消息头里的「协议版本号」字节：`-1` 表示老版式（无版本号字段，解码器按 2011/2013 兜底），`0` 是 2013，`1` 是 2019（也包含粤标扩展，见 `commons/transform/attribute/AlarmDSM.java` 里 `desc = "终端ID(粤标)", version = 1`）。
2. **版本判定在 `JTUtils.headerLength()` 只用一个 `version > 0` 分支**：`version > 0` → 2019（17 字节 / 分包 21 字节），否则 → 12 字节 / 分包 16 字节。所以整个 2019 与 2011/2013 的差异，落到的就是「多一个 1 字节协议版本号 + 手机号从 6 字节变 10 字节」。
3. **`getProtocolVersion()` 被实体自己重写**，用 `getBodyLength()` 猜版本——这是「没有版本号字段时的兜底手段」，也说明这类老协议适配是**逐条报文手工写特例**，不是纯注解能自动覆盖的。

`protostar` 的 `@Field` 属性（`io/github/yezhihao/protostar/annotation/Field.java`）值得逐条记下，因为它基本就是「一份可复用的协议字段 DSL」：

| 属性 | 作用 |
| --- | --- |
| `index` | 字段序号 |
| `length` | 固定长度；`-1` 表示「读到剩余」 |
| `lengthUnit` | 长度域本身的宽度：1=BYTE，2=WORD，3=MEDIUM(3字节)，4=DWORD |
| `lengthExpression` | 长度由表达式推导（如 `#{a}*2`） |
| `totalUnit` | 集合元素个数域宽度 |
| `charset` | `GBK`（默认）/ `BCD` / `HEX` / `UTF-8` |
| `version` | `int[]`，同一字段在 2011/2013/2019 下的不同定义，`@Repeatable` |
| `converter` | 自定义转换器 |

运行时由 `SchemaManager` 扫描 `@Message` 类，构造 `RuntimeSchema`，登记到两张表：

```java
// io/github/yezhihao/protostar/schema/SchemaManager.java
Map<Integer, ArrayMap<RuntimeSchema>> typeIdMapping;     // 消息ID → Schema（多版本）
Map<String, ArrayMap<RuntimeSchema>> typeClassMapping;   // 类名 → Schema
```

`RuntimeSchema.mergeFrom` 按字段顺序遍历，边读边判 `input.isReadable()`，异常统一包装成 `"Read failed i className fieldName"`。

**评价**：这是四个项目里**最优雅的协议建模方式**。协议改动 = 改注解，不用改解析代码；同一个类可挂多个 `@Message`（如 `T1211` 同时是「文件信息上传」和「文件上传完成消息」）。

**代价**：反射 + 每次字段访问的 `Field` 元数据查表，在 2 万 QPS 级别会有可观开销（推断，未实测）；且 Schema 构造依赖类扫描，Android/ProGuard 场景需要额外 keep 规则。

#### 2.3.2 消息头解析（位运算）

`basics/JTMessage.java` 把「消息体属性」WORD 拆成 5 个位域常量：

```java
private static final int BODY_LENGTH = 0b0000_0011_1111_1111; // bit0-9   消息体长度
private static final int ENCRYPTION  = 0b0001_1100_0000_0000; // bit10-12 加密方式
private static final int SUBPACKAGE  = 0b0010_0000_0000_0000; // bit13    分包
private static final int VERSION     = 0b0100_0000_0000_0000; // bit14    版本标识
private static final int RESERVED    = 0b1000_0000_0000_0000; // bit15    保留
```

头部长度由版本 + 分包决定（`commons/JTUtils.java`）：

```java
public static int headerLength(int version, boolean isSubpackage) {
    if (version > 0) return isSubpackage ? 21 : 17;   // 2019：+协议版本1 +终端ID 10
    else             return isSubpackage ? 16 : 12;   // 2011/2013
}
```

注意 `version > 0` 用的是「消息头里的版本标识位」，而不是「平台配置的版本」——这意味着**2013 终端发来的包按 12/16 解析，2019 终端按 17/21 解析，同一进程内天然兼容两种版本**，不需要按连接区分版本。这是与 SmallChi 的 `JT808Version.JTT2013Force` 不同的思路，更省配置但少了「强制版本」的兜底能力（老设备发 2019 头但版本位=0 时会解析错）。

#### 2.3.3 转义与校验

`JTMessageDecoder.decode` 的顺序是：

1. `unescape`：基于 `CompositeByteBuf`，把 `0x7d 0x01` 还原成 `0x7d`、`0x7d 0x02` 还原成 `0x7e`；
2. `verify`：`JTUtils.bcc(buf, -1)` 与最后一个字节比对，不等则丢包（不抛异常，避免污染 Channel）；
3. 读 `messageId`、`properties`；
4. bit14 为 1 才读 `protocolVersion`（1 字节）；
5. 按 `headerLength` 选 `headSchema` 并 `mergeFrom`；
6. 若头部声明版本与 `ProtocolVersion` 字段实际值不同，**重新取 bodySchema**；
7. 分包时把每片 body 交给 `MultiPacket.addAndGet`，齐了以后 `Unpooled.wrappedBuffer(packages)` 合并，再 `bodySchema.mergeFrom`。

#### 2.3.4 分包重组（`MultiPacket` + `MultiPacketDecoder`）

```java
// MultiPacket.java
private final ByteBuf[] packets;      // 下标 = 包序号 - 1
private final int count;
private long creationTime, lastAccessedTime;
private int retryCount;

public synchronized boolean addAndGet(int packetNo, ByteBuf packetData) {
    int index = packetNo - 1;
    if (packets[index] == null) {
        packets[index] = packetData;
        return ++arrived == count;
    }
    packetData.release();             // 重复包直接释放
    return false;
}
public int[] getNotArrived() { … }    // 返回缺失包序号
```

`MultiPacketDecoder` 的缓存键是 **`clientId/messageId/packageTotal`**（注意：不含流水号），用 `ConcurrentHashMap`；`packetNo == 1` 时记录首包流水号；重组完成后 `remove`。

超时清理靠 `MultiPacketListener`（默认 20 秒）+ 一个守护线程扫描；**不配置 listener 时退化为 `WeakHashMap`**（靠 GC 兜底，行为不确定，属于「能用但不精确」的设计）。

**评价**：
- 优点：`getNotArrived()` 为「缺失分包重传」留了接口（虽然开源版没有用它真正发 `0x8003`）。
- 短板：键里没有流水号，**同一终端同一消息 ID 连续两批分包（不同流水号）会互相污染**；容量无上限，恶意终端可用大量分包头撑爆堆内存。

#### 2.3.5 消息 ID 注册与分发

两层注册：

**(1) ID → 名称**（`commons/MessageId.java` 静态块）：反射遍历 `JT808.class` / `JT1078.class` / `JSATL12.class` 的 `public static int` 常量建 `id → 中文名`，再扫描 `org.yzh.protocol` 包里带 `@Message` 的类建 `id → Class`。启动时一次性完成，运行时只查表。

**(2) ID → Handler**（netmc `DefaultHandlerMapping`）：扫描 `@Endpoint` 类，对每个 `@Mapping(types = {...})` 方法注册到 `handlerMapping`；`DispatcherHandler` 按 `messageId` 取 Handler 执行：

```java
// io/github/yezhihao/netmc/handler/DispatcherHandler.java
Handler handler = handlerMapping.getHandler(messageId);
if (handler != null) {
    if (handler.async) businessPool.execute(() -> handler.invoke(...));
    else handler.invoke(message, session, ...);
    // 返回 null 且 returnVoid 时，由 interceptor.successful 生成默认应答
    ctx.writeAndFlush(packet.replace(response));
} else {
    interceptor.notSupported(message, session);
}
```

**与你的项目对照**：你的 `msg_processor.go` 是「表驱动 `map[uint16]*action{genData, process}`」，yezhihao 是「注解扫描 + 反射调用」。前者编译期可见、零反射、易调试；后者声明式、加消息不改分发代码。二者思路不同但都成立，**没有必须切换的理由**。

### 2.4 终端会话管理

**绑定流程**（`JT808Endpoint`）：

```java
@Mapping(types = 终端注册)
public T8100 T0100(T0100 message, Session session) {
    session.register(message);                 // sessionId = clientId = 手机号
    DeviceDO device = new DeviceDO(); …        // 塞进 session attribute
    session.setAttribute(SessionKey.Device, device);
    return new T8100().setToken(deviceId + "," + plateNo)…
}
```

`netmc` 的 `Session.register` 语义（`session/Session.java`）：

```java
public void register(String sessionId, Message message) {
    this.sessionId = sessionId;
    this.clientId  = message.getClientId();
    if (sessionManager != null) sessionManager.add(this);
}
```

`SessionManager` 就是一个 `ConcurrentHashMap<String, Session>`，键为 `sessionId`；另有 `Caffeine<String,Object> offlineCache`（`expireAfterAccess(10, MINUTES)`）用于「设备离线时的业务上下文」。

**这里有一个必须点名的缺陷**：`SessionManager.add` 是

```java
Session oldSession = sessionMap.put(newSession.getId(), newSession);
```

**同一手机号重新注册时，旧 Session 只是被覆盖出 Map，Channel 没有关闭**。旧连接要等 `IdleStateHandler`（默认 180 秒）超时才断。后果：设备网络抖动重连时，平台会同时存在两条活连接，下行指令可能发到旧连接上（`MessageManager` 从 Map 里取到的是新 Session，所以下行是「新连接」；但旧连接上的上行仍会被处理并覆盖会话状态）。SmallChi Gateway 用 `TryLink` 显式 `cacheSession.Close()` 解决了这个问题——这是可直接借鉴的点（见第 8 节 P0-3）。

**鉴权（`0x0102`）**：

```java
@Mapping(types = 终端鉴权)
public T0001 T0102(T0102 message, Session session) {
    session.register(message);
    String token = message.getToken();  // deviceId + "," + plateNo
    … // 仅按逗号拆分，不查库、不校验
    return new T0001();  // 无条件成功
}
```

**README 与代码不符（明确标注）**：文档演示了完整的注册/鉴权流程，但开源代码里 `0x0102` **完全不校验鉴权码**，任意终端注册后即可鉴权成功。同时 `pom.xml` 引了 `mybatis-plus-spring-boot3-starter`、`mariadb-java-client`、`h2`、`HikariCP`，但**全仓库找不到一个 Mapper / Service 实现**，`0x0200` 的批量入库方法体是空的：

```java
@AsyncBatch(poolSize = 2, maxElements = 4000, maxWait = 1000)
@Mapping(types = 位置信息汇报)
public void T0200(List<T0200> list) {
    // 空实现
}
```

结论：**这是「协议 + 框架能力」的演示仓库，不是可生产的业务服务端**。

**心跳与离线判定**：`0x0002` 是空方法，**完全依赖 Netty 的 `IdleStateHandler`**：

```java
// JTConfig.java
Server jt808TCPServer = Server.builder()
    .idleStateTime(jtProperties.getIdleTimeout())   // 默认 180 秒
    .maxFrameLength(2 + 21 + 1023 * 2 + 1 + 2)
    .delimiter(new byte[]{0x7e})
    .decoder(new JTMessageAdapter("org.yzh.protocol"))
    …
```

`netmc` 的 `TCPMessageAdapter.userEventTriggered(IdleStateEvent)` → `ctx.close()`，日志「终端心跳超时」。**没有独立的「心跳登记表」，也没有按设备配置的心跳周期**——所有连接用同一个 180s 阈值。这对 2019 协议里「按 `0x8103` 参数配置心跳间隔」的场景不够精细。

**UDP**：同一个 `Server` 加 `enableUDP(true)`，`netmc` 的 `Packet.of` 会把 `ByteBuf` 包成 `DatagramPacket(byteBuf, session.remoteAddress())`。

### 2.5 指令下行与应答（同步等待）

这是 yezhihao 最值得抄的一段设计——**用 Reactor `Mono` 把「等待应答」变成一个可 `timeout` 的异步结果**。

`netmc/session/Session.java`：

```java
private final AtomicInteger serialNo = new AtomicInteger();
private static final IntUnaryOperator UNARY_OPERATOR = prev -> prev >= 0xFFFF ? 0 : prev + 1;

public int nextSerialNo() {
    return serialNo.getAndUpdate(UNARY_OPERATOR);
}

public <T> Mono<T> request(Message request, Class<T> responseClass) {
    requestInterceptor.accept(this, request);
    String key = requestKey(request, responseClass);   // 应答类名 + '.' + 请求流水号
    return Mono.<T>create(sink -> {
        if (subscribers.putIfAbsent(key, sink) != null) {
            sink.error(new IllegalStateException("等待应答中，请勿重复发送"));
        } else {
            sink.onDispose(() -> subscribers.remove(key, sink));
            channel.writeAndFlush(Packet.of(this, request))
                   .addListener(f -> { if (!f.isSuccess()) sink.error(f.cause()); });
        }
    }).timeout(Duration.ofSeconds(30L));
}

public boolean response(Message message) {
    MonoSink<Message> sink = subscribers.get(responseKey(message));
    if (sink != null) { sink.success(message); return true; }
    return false;
}
```

业务层（`MessageManager`）：

```java
return session.request(request, responseClass)
              .timeout(Duration.ofSeconds(10), TIMEOUT)      // TIMEOUT = APIException(4002, "客户端响应超时")
              .onErrorResume(t -> Mono.error(
                  new APIException(4001, "消息发送失败").setDetails(...)));  // 统一包成 4001
```

`JTSessionListener` 在 `sessionCreated` 时注册 `requestInterceptor`，负责：
- 把下行消息 `clientId` 设成 `session.clientId`（注册后用手机号，注册前用临时 ID）；
- `serialNo = session.nextSerialNo()`；
- `messageId == 0` 时用 `reflectMessageId()` 从注解回填；
- 从 `SessionKey.Device` 取 `DeviceDO`，若 `protocolVersion > 0` 则 `setVersion(true)`。

**这套机制的能力边界（要看清）**：
- ✅ 天然支持「一个请求等待一个应答」，超时、重复请求检测、Channel 关闭自动清理（`onDispose`）。
- ❌ **没有重试**。超时就是超时，调用方自己决定重发。
- ❌ **配对键是「应答类 + 流水号」**，多实例/多进程场景下 `subscribers` 是进程内 `Map`，无法跨节点。
- ❌ `0x0001`（终端通用应答）的配对依赖「应答里的流水号字段」，如果终端回错流水号就永远等不到（30s 超时兜底）。

**与你的项目对照**：你的 `param_response_broker.go` / `property_response_broker.go` / `location_response_broker.go` / `region_response_broker.go` 是「按消息类型各写一个 Broker」，本质相同但**类型安全更好、可读性更好**；yezhihao 是「一个通用 `subscribers` Map + `Mono`」，**扩展性更好**（加一种应答不用写新 Broker）。你的 `server.ackTimeoutMs` + `maxRetransmissions` 配置是 yezhihao 没有的（它有超时无重试），这是你的优势，不应为了「对齐」而丢掉。

### 2.6 JT1078 音视频（信令）

先给结论：**yezhihao 开源版没有 RTP 接入、没有媒体流转发、没有流媒体服务器**。README 明确写「需自建流媒体服务」。

**下行信令（平台 → 终端）** 通过 HTTP Controller 组装消息，再用 `sendClient` 下发（`JT1078Controller`）：

| HTTP 接口 | 生成的消息 | 说明 |
| --- | --- | --- |
| `/9003` | `T1003` | 查询音视频资源 |
| `/9101`、`/9102` | `T0001` | 实时音视频传输请求、关闭 |
| `/9201` | `T1205` | 回放请求（等待终端回 `0x1205` 资源列表） |
| `/9202` | `T0001` | 回放控制 |
| `/9205` | `T1205` | 下载请求 |
| `/9206`、`/9207` | `T0001` | 上传完成/文件上传控制 |
| `/9208` | `T0001` | 苏标附件上传指令（下发前会 `jtProperties.configure(request)` 填服务器地址） |
| `/9301`~`/9306` | `T0001` | 录像/下载等控制 |

**上行信令（终端 → 平台）**：`JT1078Endpoint` 只注册 4 个映射，其中 3 个仅做 `session.response()` 唤醒等待者：

```java
@Endpoint
@Component
public class JT1078Endpoint {

    @Mapping(types = 终端上传音视频资源列表)        // 0x1205
    public void T1205(T1205 message, Session session) { session.response(message); }

    @Mapping(types = 终端上传音视频属性)            // 0x1003
    public void T1003(T1003 message, Session session) { session.response(message); }

    @Mapping(types = 文件上传完成通知)              // 0x1206（JT1078 命名空间）
    public void T1206(T1206 message, Session session) { session.response(message); }

    @Mapping(types = 终端上传乘客流量)              // 0x1005
    public void T1005(T1005 message, Session session) { /* 空实现 */ }
}
```

`0x1005`（终端上传乘客流量）是**空实现**。

**字段定义**（可直接对照你的 `msg` 包）：

```java
// T9101.java
@Message(JT1078.实时音视频传输请求)      // 0x9101
public class T9101 extends JTMessage {
    @Field(lengthUnit = 1, desc = "服务器IP地址")            private String ip;
    @Field(length = 2, desc = "实时视频服务器TCP端口号")      private int tcpPort;
    @Field(length = 2, desc = "实时视频服务器UDP端口号")      private int udpPort;
    @Field(length = 1, desc = "逻辑通道号")                  private int channelNo;
    @Field(length = 1, desc = "数据类型：0.音视频 1.视频 2.双向对讲 3.监听 4.中心广播 5.透传")
    private int mediaType;
    @Field(length = 1, desc = "码流类型：0.主码流 1.子码流")  private int streamType;
}
```

`T9201`（回放）在此基础上多 `storageType / playbackMode / playbackSpeed / startTime(BCD6) / endTime(BCD6)`。
`T9205`（下载）是 `channelNo / startTime / endTime / warnBit1 / warnBit2 / mediaType / streamType / storageType`。

**苏标 `T9208`（平台下发附件服务器地址）** 字段：`ip / tcpPort / udpPort / deviceId / dateTime(BCD6) / sequenceNo / fileTotal / reserved / platformAlarmId(32) / reserves(16)`。

**部署模型（这点很重要）**：`JTProperties` 里把服务器地址写进配置，再在下发时回填：

```yaml
jt-server:
  jt808:
    tcp-port: 7100
    udp-port: 7100
    idle-timeout: 180
    message-package: org.yzh.protocol
    t0801.path: D:/jt_data/media_file
    t9208:
      host: 127.0.0.1
      port: 7200
      enabled: true
      path: D:/jt_data/alarm_file
    t9206:
      host: 127.0.0.1
      port: 21
      username: test
      password: test
```

即：**平台自己就是「附件服务器」（7200 端口）和「媒体文件服务器」（`t0801.path`），但音视频流（RTP）要另外找人接**。`t9206` 是 FTP（多媒体文件上传）。

### 2.7 苏标主动安全（T/JSATL 12-2017）

这是 yezhihao 最有工程价值的开源部分（完整可跑）。

#### 2.7.1 独立端口 + 魔数帧

苏标附件不走 808 协议栈，而是**独立的裸数据通道**，靠固定帧头区分：

```java
// JTConfig.java
Server alarmFileServer = Server.builder()
    .port(7200)
    .lengthField(new byte[]{0x30, 0x31, 0x63, 0x64}, 1024 * 65, 58, 4)  // 魔数 + 最大长度 + 头长 + 长度域偏移
    .decoder(new DataFrameMessageDecoder())
    .build();
```

`DataFrameMessageDecoder` 判断 ByteBuf 是否以 `{0x30,0x31,0x63,0x64}` 开头，是则按 `DataPacket` schema 解析：

```java
// jsatl12/DataPacket.java
@Message
public class DataPacket {
    private int flag;          // 4 字节帧头
    private String name;       // length = 50，文件名
    private int offset;        // 4 字节，偏移
    private int length;        // 4 字节，长度
    private ByteBuf data;      // 剩余即数据
    public String getClientId() { return session.clientId; }   // 复用连接对应终端
    public int getMessageId()  { return flag; }
}
```

#### 2.7.2 三段式流程：0x1210 → 数据帧 → 0x1211

```
终端 ──0x1210（报警附件信息：终端ID、报警标识、附件列表[文件名,大小]）──▶ 平台
终端 ──0x30316364 数据帧（filename + offset + length + data）*N ────────▶ 平台 7200
终端 ──0x1211（文件上传完成：name/type/size）───────────────────────────▶ 平台
平台 ──0x9212（结果 0=成功；1=失败并附缺失偏移列表）──────────────────────▶ 终端
```

`JSATL12Endpoint` 的实现细节：

```java
// 文件信息缓存：key = 手机号，value = 文件名 → 文件大小；20 分钟访问过期
private final Cache<String, Map<String, T1210.Item>> cache =
        Caffeine.newBuilder().expireAfterAccess(20, TimeUnit.MINUTES).build();

@Mapping(types = JSATL12.报警附件信息消息)
public void alarmFileInfoList(T1210 message, Session session) {
    session.register(message.getDeviceId(), message);   // 苏标用 deviceId 注册会话
    …
}

@Mapping(types = {JSATL12.文件信息上传, JSATL12.文件上传完成消息})
public void alarmFileComplete(T1211 message, Session session) {
    if (!session.isRegistered()) session.register(message);
    byte[] missing = fileService.checkFile(message);     // 返回缺失块偏移
    if (missing == null) return new T9212().setResult(0);
    return new T9212().setResult(1).setItems(missing);
}
```

#### 2.7.3 文件落盘：`.tmp` + `.log` 的「随机写 + 完整性校验」

`FileService` 是这套设计的核心，值得完整理解：

```
目录：{t9208.path}/{clientId}_{yyMMddHHmmss}_{serialNo}/
  ├─ fs.txt        # 文件清单
  ├─ xxx.mp4.tmp   # 数据按 offset 随机写入
  └─ xxx.mp4.log   # 每写完一块追加 8 字节（4B offset + 4B length）
```

```java
// 写：随机偏移 + 记录块日志
int offset = dataPacket.getOffset();
int length = dataPacket.getLength();
data.readBytes(file, offset, data.readableBytes());   // FileChannel 定位写
log.write(ByteBuffer.wrap(new byte[]{offset, length}));

// 校验：读 .log 所有块 → 排序 → 找间隙
long[][] ranges = …;                 // size+2 行，首尾各加一个哨兵
Arrays.sort(ranges, Comparator.comparingLong(a -> a[0]));
for (int i = 0; i < ranges.length - 1; i++) {
    if (ranges[i][1] != ranges[i + 1][0]) { … 记录 missing offset/length … }
}
if (missing.isEmpty()) {
    tmp.renameTo(final);   // 完成后重命名
    log.delete();          // 删除块日志
}
```

**Channel 缓存**：`channelsCache = Caffeine.expireAfterAccess(10s)`，value 是 `Tuple2<FileChannel, FileChannel>`（`.tmp` 与 `.log` 都保持打开，避免每片都 `open/close`）。

**优点**：并发接收多片、乱序到达、断点续传（终端重传缺失片）都能正确工作；`0x9212` 能把缺失偏移精确告诉终端，是「协议层可恢复」的典范。
**短板**：单机本地盘；`maxFrameLength = 1024*65` 说明单帧上限 64KB；目录名用手机号 + 时间 + 流水号，**同名重复上传（补传）会新建目录，不做去重**；`fs.txt` 清单只是写文件，没有用于查询。

### 2.8 缓存与持久化

| 类型 | 实现 | 缓存内容 | 过期 |
| --- | --- | --- | --- |
| 会话 | `ConcurrentHashMap`（netmc `SessionManager`） | sessionId → Session | 无（连接关闭移除） |
| 离线上下文 | Caffeine | clientId → Object | `expireAfterAccess(10min)` |
| 分包 | `ConcurrentHashMap` 或 `WeakHashMap` | clientId/msgId/packageTotal → MultiPacket | 20s（有 listener 时）；否则靠 GC |
| 苏标文件信息 | Caffeine | 手机号 → (文件名 → 大小/类型) | `expireAfterAccess(20min)` |
| 苏标文件句柄 | Caffeine | clientId → (tmp, log) 两个 FileChannel | `expireAfterAccess(10s)` |
| 业务数据 | **无** | —— | —— |

**数据库**：`pom.xml` 有 MyBatis-Plus + MariaDB + H2 + HikariCP，`application.yml` 有 `mybatis-plus` 配置段，但**没有任何 Mapper 接口、没有 `@MapperScan`、没有建表 SQL**。可以判定：开源版**没有落库实现**，数据库依赖只是「给你接自己的库用」的脚手架。

**评价**：缓存设计是「够用但不严谨」——`expireAfterAccess` 对「上传完成后等 0x1211」这种场景是对的（访问即续期），但**没有容量上限**（Caffeine 未调 `maximumSize`），极端情况下附件句柄/文件信息会撑爆内存。

### 2.9 分布式与部署模式

**结论：无分布式设计。** 具体：

- 会话是进程内 `ConcurrentHashMap`，多实例之间不可见；
- 下行指令靠「连接到哪个实例」决定，没有「按手机号路由到持有连接的实例」的机制；
- 没有 Redis、没有 MQ、没有注册中心；
- `jtt808-server` 是单 Spring Boot 进程，`controller` 层的 HTTP 接口必须打到「持有该终端连接的实例」，否则 `MessageManager` 拿不到 Session，直接返回 `APIException(4000)`。

**能水平扩展的部分**：HTTP 查询类接口（无状态）；编解码（纯函数）。
**瓶颈点**：会话；下行；苏标文件写入（本地盘、本地 FileChannel）。

它的「分布式」只体现在架构分层上：`jtt808-protocol` 可以单独发布成 SDK，被别的服务引用——这算「代码级复用」，不算部署级分布式。

### 2.10 架构分层与可扩展性

分层清晰：

```
网络层(netmc: Netty + FrameDecoder + Dispatcher)
   ↓ MessageEncoder/Decoder 适配
编解码层(protostar: Schema + 注解)
   ↓
协议层(org.yzh.protocol: 消息定义)
   ↓
业务层(@Endpoint + @Mapping)
   ↓
服务层(FileService / MessageManager)
```

扩展方式：

| 需求 | 做法 | 成本 |
| --- | --- | --- |
| 加一个消息 | 写一个 `@Message` 类 + 在 `@Endpoint` 加 `@Mapping` 方法 | 极低 |
| 加一个私有协议字段 | `@Field(version = {…})` 或新 `@Message` 类 | 低 |
| 换网络框架 | 实现 netmc 的 `MessageEncoder/Decoder` 接口 | 中 |
| 换存储 | 目前没有存储抽象，得自己加 | 中高 |
| 接自己的流媒体 | 只拿信令，`0x9101/0x9201` 的 `ip/port` 指到自己服务器 | 低 |

**二次开发成本**：加消息几乎零成本；**做生产级业务（鉴权、落库、分布式、流媒体）全部要自己写**——这部分开源版没有。

### 2.11 优点与明显短板

**做得好的地方（具体）**：
1. **注解即协议**。`@Message` + `@Field(lengthUnit/totalUnit/lengthExpression/version)` 这套 DSL，把 2011/2013/2019 三个版本的差异收敛到同一个类里，维护成本极低。
2. **请求-应答关联**。`Session.request(...).timeout()` 用 Reactor 把「异步等待」表达得非常干净，`onDispose` 自动清理，比手写 `Map<Long, chan>` 少一大截样板代码。
3. **苏标附件服务器的完整性设计**。`offset` 随机写 + `.log` 块记录 + 间隙检测 + `0x9212` 回报缺失偏移，是**能真正支持断点续传**的实现，而不是「按顺序收、错了就整包重来」。
4. **`getNotArrived()`**。分包容器预留了「查询缺失分包序号」的能力，是 0x8003 补传协议的正确基础设施。
5. **不依赖 Spring 也能跑**。`jtt808-protocol` 是纯 Java 库，可以在 Android 做终端模拟器；这一点对测试工具开发很有价值。
6. **报文解释器**。能把 hex 报文翻译成带位含义的 JSON，调试 808 报文时非常省时间。

**明显短板（具体）**：
1. **框架外置导致「读仓库读不到网络层」**。想知道帧解码、分发、会话怎么实现，必须再去读 `netmc`；README 完全没有提这件事，容易让人误判「这是完整的服务端」。
2. **业务层是空壳**。`0x0102` 不校验鉴权码；`0x0200` 的 `@AsyncBatch` 方法体为空；MyBatis 依赖存在但零 Mapper。**照抄不能上生产**。
3. **同号重连不踢旧连接**。`SessionManager.add` 直接 `put` 覆盖，旧 Channel 要等 180s idle 超时才断，双活期间状态可能被旧连接的上行覆盖。
4. **分包键缺流水号**。`clientId/messageId/packageTotal` 的键在同一终端连续两批分包时会冲撞；`WeakHashMap` 兜底方案不可预测。
5. **缓存无容量上限**。Caffeine 全部只设时间不设 `maximumSize`，附件场景有内存风险。
6. **1078 只有信令**。`0x9101/0x9201` 下发后，终端把 RTP 推到 `ip:port`，这个 `ip:port` 指向的媒体服务器**不在本项目内**。
7. **`0x1005`（终端上传音视频资源列表）空实现**，`0x1205` 只做应答唤醒不解析资源列表。

---

## 3. SmallChi/JT808

### 3.1 设计目标与定位

**这是一个纯编解码库，不是服务端**。README 的第一段就是「需要掌握进制转换、BCD/Hex、位移/异或、反射、`JObject`、`Span<T>`」——这是给「要改协议库的人」看的，不是给「要部署服务的人」看的。

仓库里的项目：

```
src/
├── JT808.Protocol/                        # 核心：消息体 + 包 + 序列化器 + 配置 + 内部默认实现
│   ├── Interfaces/                        # IJT808Config / IJT808MsgIdFactory / IJT808SplitPackageStrategy / IMerger / IJT808MsgSNDistributed / IJT808ExternalRegister …
│   ├── Internal/                          # 上述接口的默认实现（Default*Impl）
│   ├── MessageBody/                       # JT808_0x0200 / 0x8103 / 0x8300 … 全部消息体
│   ├── JT808Package.cs                    # 7E + Header + Body + BCC + 7E
│   ├── JT808Header.cs
│   ├── JT808HeaderMessageBodyProperty.cs
│   ├── JT808Serializer.cs
│   └── DependencyInjectionExtensions.cs
├── JT808.Protocol.Extensions/
│   ├── JT808.Protocol.Extensions.JT1078/  # 9101/9102/9201/9202/9205/9206/9207/9301-9306/1003/1005/1205/1206
│   ├── JT808.Protocol.Extensions.SuBiao/  # 1210/1211/1212/9208/9212/0200_0x64-0x67/0900_0xF7-F8
│   ├── JT808.Protocol.Extensions.YueBiao/ # 粤标：1FC4/0200_0xF1-F2/8103_0xF370 …
│   └── JT808.Protocol.Extensions.GPS51/
└── JT808.Protocol.Test/                  # 每个消息体都有编解码往返测试
```

**维护状态**：613 star，最近 push 2026-09-16，MIT，无 open issue。活跃但节奏慢（偏「协议补全」而非「功能演进」）。

### 3.2 协议编解码（重点）

#### 3.2.1 消息体属性位解析

```csharp
// JT808HeaderMessageBodyProperty.cs —— 构造时按位拆
public JT808HeaderMessageBodyProperty(ushort value) {
    Reserve     = value & 0x8000;                    // bit15（注意存的是 int，不是 bool）
    VersionFlag = (value >> 14 & 0x01) == 1;         // bit14
    IsPackage   = ((value >> 13) & 0x001) == 1;      // bit13
    switch ((value & 0x400) >> 10) {                 // bit10-12：0=不加密，1=RSA（其余当不加密）
        case 0: Encrypt = JT808EncryptMethod.None; break;
        case 1: Encrypt = JT808EncryptMethod.RSA;  break;
        default: Encrypt = JT808EncryptMethod.None; break;
    }
    DataLength  = value & 0x3FF;                     // bit0-9
}

// 组包时回填
public ushort Wrap() =>
    (ushort)(Reserve | versionFlag | tmpIsPacke | tmpEncrypt | DataLength);
```

`Wrap()` 是**回填**用的——序列化时先 `Skip(2)` 占位，写完数据体再回填。这是「消息体长度依赖内容长度」时的标准做法（yezhihao 也是先算 body 再组头，思路一致）。

#### 3.2.2 组包流程（`JT808Package.Serialize`）

```csharp
WriteStart();                                  // 7E
WriteUInt16(MsgId);
writer.Skip(2, out msgBodiesPropertyPosition);  // 消息体属性占位
// 版本与手机号
if (writer.Version == JTT2019 || MessageBodyProperty.VersionFlag) {
    WriteByte(ProtocolVersion); WriteBCD(phone, 20);   // 2019：1 字节版本 + 10 字节手机号
} else {
    WriteBCD(phone, config.TerminalPhoneNoLength);      // 默认 12（BCD 位数，即 6 字节）
}
WriteUInt16(MsgNum);                            // 流水号
if (IsPackage) { WriteUInt16(PackgeCount); WriteUInt16(PackageIndex); }
WriteBodies();                                  // 数据体（分包时只写当前片）
if (dataLength > 1023) throw new JT808Exception(JT808ErrorCode.MessageBodyLengthTooLong);
backfill MessageBodyProperty.Wrap() → msgBodiesPropertyPosition;
WriteXor();                                     // BCC
WriteEnd();                                     // 7E
WriteEncode();                                  // 7D02/7D01 转义
```

**流水号分配**是策略化的：

```csharp
// 优先级：手动指定 > 配置的分布式计数器
MsgNum = ManualMsgNum ?? config.MsgSNDistributed.Increment(TerminalPhoneNo);
```

#### 3.2.3 解包流程（`JT808Package.Deserialize`）

```csharp
// 1. 先校验 XOR（config.SkipCRCCode 可跳过，调试用）
// 2. ReadStart() → 7E
// 3. MsgId (2B) → MessageBodyProperty (2B)
// 4. 版本判断：
//      JT808Version.JTT2013Force          → 强制 2013
//      JTT2019 || MessageBodyProperty.VersionFlag → 2019（先读 ProtocolVersion，再 BCD20 手机号）
//      否则                                → 2013（读 BCD config.TerminalPhoneNoLength，默认 12 位）
// 5. MsgNum (2B)
// 6. 分包头 PackgeCount/PackageIndex
// 7. 数据体：config.MsgIdFactory.TryGetValue(msgId) 拿到 IJT808Bodies 实例 → DeserializeExt
// 8. 分包：ReadArray(DataLength)；若 config.EnableAutoMerge 则 config.Jt808PackageMerger.TryMerge(...)
// 9. CheckCode / End
```

`IJT808Config` 是整个库的「可插拔中心」：

| 接口 | 默认实现 | 作用 |
| --- | --- | --- |
| `IJT808MsgIdFactory` | `JT808MsgIdFactory` | msgId → `IJT808Bodies` 实例（注册表） |
| `IJT808SplitPackageStrategy` | `DefaultSplitPackageStrategyImpl` | 大包切分，固定 `N = 256*3 = 768` 字节 |
| `IMerger` | `DefaultMerger` | 分包合并，`AutoMergeTimeoutSecond` 默认 300s，后台每 60s 清理 |
| `IJT808MsgSNDistributed` | `DefaultMsgSNDistributedImpl` | 按手机号独立自增流水号 |
| `IJT808ExternalRegister` | — | `Register(Assembly)` 扫描外部程序集注册消息体 |

**外部扩展机制（关键）**：1078/苏标/粤标不是编进核心库，而是单独程序集 + 一行注册：

```csharp
// JT808.Protocol.Extensions.JT1078/DependencyInjectionExtensions.cs
public static IJT808Builder AddJT1078Configure(this IJT808Builder jT808Builder) {
    jT808Builder.Config.Register(Assembly.GetExecutingAssembly());   // 扫描本程序集里所有 JT808Bodies
    return jT808Builder;
}
```

这就是「SPI/插件」的标准做法：**核心库不认识 1078/苏标，但允许外部程序集把自己注册进 `MsgIdFactory`**。你的项目如果要做「粤标/苏标可选编译」，可以借鉴这个「按程序集注册」的分层思路（Go 里对应的是「按包 `init()` 注册」或显式 `Register(config)`）。

#### 3.2.4 附加信息（0x0200）的二级工厂

0x0200 的附加信息是 `附加信息ID(1B) + 长度(1B) + 数据` 的 TLV 列表。SmallChi 用**两层字典**：

```csharp
// IJT808_0x0200_Factory：附加信息ID → 解析器
// IJT808_0x0200_Custom_Factory：业务方自定义扩展，优先级更高
// 苏标在 SuBiao 程序集里注册：
JT808_0x0200_0x64  // ADAS 报警
JT808_0x0200_0x65  // DSM 报警
JT808_0x0200_0x66  // TPMS 报警
JT808_0x0200_0x67  // 盲区/其它
```

**对比你的项目**：`internal/protocol/model/msg_0200.go` 目前把附加信息读成**原始 TLV**：

```go
type LocationExtItem struct {
    ID     uint8  `json:"id"`
    Length uint16 `json:"length"`
    Data   []byte `json:"data"`
}
```

这**在协议层是正确且安全的**（不认识的 ID 不会丢包），但业务层拿不到结构化字段（比如苏标的 `AlarmId / FlagState / AlarmOrEventType / Speed / 高程`）。SmallChi 的做法是「按 ID 注册 typed parser，未注册的退回 raw」——这个折中方案值得借鉴（见第 8 节 P0-1）。

#### 3.2.5 分包合并的默认实现（含一个疑似 bug）

`DefaultMerger` 的缓存结构：`ConcurrentDictionary<手机号, ConcurrentDictionary<消息ID, List<(index, data)>>>`，超时键是 `手机号-消息ID`，`AutoMergeTimeoutSecond` 到期后由后台任务每 60s 清理。

但合并那几行代码值得警惕：

```csharp
packages.Add((header.PackageIndex, data));          // 当前片先加进去
if (packages.Count != header.PackgeCount) return false;
…
var mateData = packages.OrderBy(x => x.index)
                       .SelectMany(x => x.data)
                       .Concat(data)                // ← 又把当前片拼了一次
                       .ToArray();
```

按代码阅读，**最后一个分包的数据会被拼接两次**（`packages` 里已经 `Add` 过一次，末尾 `Concat(data)` 又加一次）。这会直接导致消息体尾部多出一段重复字节。我没有找到对应的测试用例覆盖这个分支（`JT808.Protocol.Test` 里分包测试只覆盖「未齐」分支的可能性较大）。**标注为「疑似 bug，需实测确认」**，不建议把这个实现照搬到 Go。

### 3.3 缓存与持久化 / 分布式

不存在。
- 没有会话、没有连接、没有服务端；
- `DefaultMsgSNDistributedImpl` 是 `ConcurrentDictionary<string,int>`，进程内；
- `DefaultMerger` 是进程内静态单例，**多实例部署时会话/分包状态不共享**；
- 无 Redis / 无 MySQL / 无 MQ。

### 3.4 架构分层与可扩展性

```
IJT808Config（配置中心）
  ├── MsgIdFactory        消息体注册表（Extern 程序集注册）
  ├── SplitPackageStrategy 分包策略
  ├── Merger              合并策略
  ├── MsgSNDistributed    流水号策略
  └── Compress            压缩策略（GZip 实现）
```

所有策略都是接口 + 默认实现 + DI 覆盖。这是**四个项目里「可替换性」做得最彻底的**：你可以在不改库代码的前提下换掉流水号算法、分包大小、合并超时、压缩方式。

**二次开发成本**：加消息体 = 写一个 `JT808Bodies` 子类 + 注册；改行为 = 换接口实现。非常低。

### 3.5 优点与明显短板

**优点**：
1. **协议覆盖最全**。2011/2013/2019 + 1078 信令 + 苏标 + 粤标 + GPS51，几乎所有地方标准都能找到对应消息体，且每条都有单测。
2. **策略全部接口化**。`IJT808SplitPackageStrategy` / `IMerger` / `IJT808MsgSNDistributed` / `IJT808ExternalRegister` 这套抽象，是设计得最干净的部分。
3. **`Analyze` 报文解释器**。`JT808AnalyzeExtensions` 能把 hex 翻译成带位含义的 JSON（和 yezhihao 的「报文解释器」同一思路，两个项目都有，说明这是刚需）。
4. **`Span<T>` + `ArrayPool`**。编解码零分配倾向明显（`JT808ArrayPool`），比 yezhihao 的反射方案在高吞吐下更稳。
5. **`record` + 不可变 `JT808HeaderPackage`**，值语义清晰，便于并发传递。

**短板**：
1. **没有任何服务端能力**。会话、鉴权、心跳、下行等待、离线判定全无；README 容易被误读为「平台」。
2. **`DefaultMerger` 疑似重复拼接**（见 3.2.5），且缓存无容量上限。
3. **全局静态单例**。`DefaultMerger.Instance`、`JT808Serializer.Instance` 是静态单例，测试隔离差；多租户/多配置场景不好办。
4. **`DefaultSplitPackageStrategyImpl` 固定 768 字节**。不是错误，但 768 不是 808 协议的最优值（转义会放大到接近 1023 上限），实际平台多按「转义后不超 1023」动态切。
5. **没有「消息 ID 名字表」**。调试时只能看到 `0x0200` 数字，不像 yezhihao 有 `MessageId` 名称映射。

---

## 4. SmallChi/JT808Gateway

### 4.1 设计目标与定位

README 第一句：**「支持 TCP/UDP 通用消息业务处理」**。它要解决的是「设备接入层与业务层解耦」，而不是「业务平台」。

三种使用形态（README 明确列出）：

| 形态 | 特征 | 适用 |
| --- | --- | --- |
| **网关集成**（`AddMessageHandler<T>`） | 业务代码编进网关进程，改业务要重启网关 | 设备少、开发能力弱、允许丢点数据 |
| **队列方式**（`AddMsgProducer` + `AddMsgConsumer`） | 网关只做接入，原始字节入 Kafka，业务在独立服务消费 | 设备多、关注点分离 |
| **转发方式**（`JT808TransmitService`） | 把指定终端的数据原样转发到另一个网关 | 多级平台/级联 |

**一个重要的历史事实（代码里有痕迹）**：`JT808Configuration.WebApiPort` 上挂着

```csharp
[Obsolete("因移除dotnetty后,此属性无意义，即将被移除，请使用自带的webapi类型项目替代控制台")]
```

说明这个网关**曾经用 DotNetty，现在已改为裸 `Socket` + `System.IO.Pipelines`**。所以「SmallChi Gateway 基于 DotNetty」这个流传很广的说法，**在 2026 年的 master 上已经不成立**。

**维护状态**：200 star，最近 push 2026-09-07，MIT，0 open issue。活跃但低频。

### 4.2 协议与消息处理（重点）

#### 4.2.1 网络层：裸 Socket + Pipe，手工切帧

```csharp
// JT808TcpServer.StartAsync
var socket = await server.AcceptAsync(cancellationToken);
JT808TcpSession session = new JT808TcpSession(socket);
SessionManager.TryAdd(session);
await Task.Factory.StartNew(async state => {
    var pipe = new Pipe();
    Task writing = FillPipeAsync(session, pipe.Writer);   // ReceiveAsync → Pipe
    Task reading = ReadPipeAsync(session, pipe.Reader);   // 切帧 → 处理
    await Task.WhenAny(reading, writing);
    SessionManager.RemoveBySessionId(session.SessionID);
}, session);
```

切帧是**手写的双标记状态机**（`ReaderBuffer`）：

```csharp
if (seqReader.IsNext(JT808Package.BeginFlag, advancePast: true)) {
    if (mark == 1) {
        data = seqReader.Sequence.Slice(totalConsumed, seqReader.Consumed - totalConsumed).ToArray();
        if (data.Length > 14) {                       // 最短合法包长度
            var package = Serializer.HeaderDeserialize(data);   // 只解头
            if (BlacklistManager.Contains(package.Header.TerminalPhoneNo)) { session.ReceiveTimeout.Cancel(); }
            SessionManager.TryLink(package.Header.TerminalPhoneNo, session);
            Processor(session, package);
        }
        totalConsumed += …; seqReader.Advance(1); mark = 0;
    }
    mark++;
} else seqReader.Advance(1);
```

**必须点名的短板**：
- 只做「按 `0x7E` 切段」，**没有字节级转义还原**（`0x7D02/0x7D01` 的处理在 `JT808.Protocol` 的完整 `Deserialize` 里，而这里只调用 `HeaderDeserialize`）；也就是说**消息体里如果出现 `7D 02`，会被误判为帧边界**。这是「手工切帧」的经典坑。
- `data.Length > 14` 只是长度下限，**没有上限**；恶意超长包会撑大内存。
- `AcceptAsync` 的 `catch (Exception) { break; }`——**任何一次 accept 异常都会让整个监听循环退出**，且不记日志（只 `throw` 前一轮的 `Logger.LogError` 都没有）。这是可用性问题。
- `Serializer.HeaderDeserialize(data)` 只解头部，**分包重组不在网关做**（`config.EnableAutoMerge` 是 `JT808Package.Deserialize` 的路径，这里没走）。分包数据以「原始字节 + 手机号」入队，**合并的责任推给了下游消费者**。

#### 4.2.2 应答生成：一张 19 项的死表

`JT808.Gateway.Abstractions/JT808MessageHandler`（注意：在 Abstractions 包里，不是 Gateway 包里）用 `Dictionary<ushort, MsgIdMethodDelegate>` 注册：

```csharp
{0x0001, Msg0x0001},   // 终端通用应答 → 不回复
{0x0102, Msg0x0102},   // 鉴权 → CommonReply（无条件成功）
{0x0002, Msg0x0002},   // 心跳 → CommonReply
{0x0003, Msg0x0003},   // 注销 → CommonReply
{0x0100, Msg0x0100},   // 注册 → 0x8100，Code = "J" + 手机号，结果码成功
{0x0200, Msg0x0200},   // 位置 → CommonReply
{0x0704, Msg0x0704},   // 批量位置 → CommonReply
{0x0900, Msg0x0900},   // 上行透传 → CommonReply
{0x0004, Msg0x0004},   // 查询服务器时间 → 0x8004
{0x0104/0x0107/0x0108, …},
{0x0201/0x0500/0x0705/0x0800, …},
{0x8204, Msg0x8204},   // 返回 default（不回复）
{0x8801, Msg0x8801},   // throw NotImplementedException("0x0805摄像头立即拍摄命令应答")
{0x0801, Msg0x0801},   // throw NotImplementedException("0x8800多媒体数据上传应答")
};
```

`CommonReply` 会按 `request.Version` 选 2019 或 2013 的组包重载：

```csharp
if (request.Version == JT808Version.JTT2019) {
    return JT808Serializer.Serialize(
        JT808MsgId._0x8001.Create_平台通用应答_2019(request.Header.TerminalPhoneNo, new JT808_0x8001 {
            AckMsgId = request.Header.MsgId,
            JT808PlatformResult = JT808PlatformResult.succeed,
            MsgNum   = request.Header.MsgNum
        }));
}
```

**评价**：
- 优点：**网关不关心业务，只保证「协议该回的应答一定回」**，业务可以在下游慢慢处理。这个「应答前置」的设计对设备体验很重要（心跳/位置这类高频消息不能等业务处理完才回）。
- 短板：**没有鉴权**（`0x0102` 无条件成功）、**没有注册去重**（`0x0100` 永远返回成功，不查库）、**两个关键上行直接抛异常**（`0x0801` 多媒体、`0x8801` 摄像头应答），异常被 `Processor` 的 `catch` 吞掉记日志，终端收不到应答会重发。
- `0x8204` 是**平台下行**消息却被放进上行处理表，说明这张表是「见过的消息 ID 全塞进去」而不是「终端上行消息集合」，设计意图不清晰。

#### 4.2.3 会话管理：双索引 + 同号踢旧连接（这段值得抄）

```csharp
// JT808SessionManager
public ConcurrentDictionary<string, IJT808Session> Sessions { get; }           // sessionId → session
public ConcurrentDictionary<string, IJT808Session> TerminalPhoneNoSessions { get; }  // 手机号 → session

internal void TryLink(string terminalPhoneNo, IJT808Session session) {
    session.TerminalPhoneNo = terminalPhoneNo;
    if (TerminalPhoneNoSessions.TryGetValue(terminalPhoneNo, out var cacheSession)) {
        if (session.SessionID != cacheSession.SessionID) {
            session.ActiveTime = DateTime.Now;
            TerminalPhoneNoSessions.TryUpdate(terminalPhoneNo, session, cacheSession);
            cacheSession.Close();                                  // ← 踢掉旧连接
            SessionProducer?.ProduceAsync(JT808GatewayConstants.SessionOnline, terminalPhoneNo);
        } else {
            cacheSession.ActiveTime = DateTime.Now;                // 同一连接，只刷活跃时间
        }
    } else if (TerminalPhoneNoSessions.TryAdd(terminalPhoneNo, session)) {
        SessionProducer?.ProduceAsync(JT808GatewayConstants.SessionOnline, terminalPhoneNo);  // 上线通知
    }
}
```

三个值得学的点：
1. **双索引**：`sessionId` 用于「按连接管理」，`手机号` 用于「按设备下发」。你的项目 `connection_registry.go` 只有 `cacheByID[s.ID]`，**没有手机号索引**，下行必须先知道 session ID。
2. **同号踢旧连接**：新连接一到就 `cacheSession.Close()`，彻底解决「设备重连后双活」问题（这是 yezhihao 的短板，这里是优点）。
3. **上线/下线事件外发**：`SessionProducer.ProduceAsync("JT808SessionOnline"/"JT808SessionOffline", phone)`，刚好对应你的 `OnDisconnect` hook——但它是**通过 MQ 解耦**的，不是进程内回调。

`RemoveByTerminalPhoneNo` 里还有一个巧思：因为「转发模式」下一条连接可能承载多个终端号，删除时要先找出「同一 SessionID 下的所有手机号」一起删，避免垃圾数据。

#### 4.2.4 心跳与离线判定

```csharp
// JT808TcpReceiveTimeoutHostedService.ExecuteAsync
foreach (var item in SessionManager.GetTcpAll()) {
    if (item.ActiveTime.AddSeconds(Configuration.TcpReaderIdleTimeSeconds) < DateTime.Now)
        item.ReceiveTimeout.Cancel();          // 取消 ReceiveAsync → 连接退出 → 下线
}
await Task.Delay(TimeSpan.FromSeconds(Configuration.TcpReceiveTimeoutCheckTimeSeconds));
```

默认 `TcpReaderIdleTimeSeconds = 600`（10 分钟），检查间隔 60 秒；UDP 默认 60 秒。配置项：

```csharp
TcpPort = 808; UdpPort = 808;
SoBacklog = 10000;
MiniNumBufferSize = 4096;
TcpReaderIdleTimeSeconds = 600;
TcpReceiveTimeoutCheckTimeSeconds = 60;
UdpReaderIdleTimeSeconds = 60;
UdpReceiveTimeoutCheckTimeSeconds = 60;
IgnoreMsgIdReply = new HashSet<uint>();     // 配 512(0x0200) 可关闭「位置上报的通用应答」
```

**评价**：用「取消读任务」来断连，比 `Socket.Close()` 干净（读循环自然退出）。但**10 分钟默认值对 808 太宽**（2019 建议心跳 60s、超时 3 个心跳周期），实际部署必须改配置。

**UDP 的特殊处理**（README 里专门解释了）：

> 使用物联网卡通过 udp 下发指令时，存储的那个 socket 地址端口，有效期非常短，不速度快点下发，那个 socket 地址端口就可能映射到别的对应卡去了，所以此处采用跟随设备消息下发指令。

代码对应 `JT808SessionManager.TryLink(terminalPhoneNo, Socket socket, EndPoint remoteEndPoint)`：**UDP 的「会话」是「手机号 → (共享 Socket, 该设备最后一次的 remoteEndPoint)」**，每次上行都刷新 `remoteEndPoint`；下发时 `SendToAsync(data, remoteEndPoint)`。这是 UDP 网关必须做的 NAT 映射跟随，**这个细节很多实现会漏**。

### 4.3 缓存与持久化

| 类型 | 实现 | 键 | 过期 |
| --- | --- | --- | --- |
| 会话（TCP） | `ConcurrentDictionary` | sessionId(Guid) | 连接关闭 |
| 会话（按手机号） | `ConcurrentDictionary` | 手机号 | 离线移除 |
| 黑名单 | `ConcurrentDictionary<string,byte>` + `blacklist.ini` | 手机号 | 无 |
| 转发通道 | `ConcurrentDictionary<string,Socket>` | `all_{host}` / `{phone}_{host}` | 断线即移除，20s 重连巡检 |
| 业务数据 | **无** | — | — |

**黑名单**是四个项目里唯一带「热更新」的持久化设计：

```csharp
fileSystemWatcher = new FileSystemWatcher(baseDir, "blacklist.ini");
fileSystemWatcher.NotifyFilter = NotifyFilters.LastWrite;
fileSystemWatcher.EnableRaisingEvents = true;
fileSystemWatcher.Changed += (s, e) => Init(e.FullPath);
```

`Add/Remove` 同时改内存 + 重写文件。**没有数据库**——定位如此。

### 4.4 分布式与部署模式

这是四个项目里**唯一真正设计了分布式**的。

#### 4.4.1 消息队列：Kafka 三组 Topic

```csharp
// JT808GatewayConstants
SessionTopic     = "jt808session";
MsgTopic         = "jt808msgdefault";
MsgReplyTopic    = "jt808msgreplydefault";
SessionOnline    = "JT808SessionOnline";
SessionOffline   = "JT808SessionOffline";
```

`appsettings.json`（`simples/JT808.Gateway.SimpleQueueServer`）：

```json
{
  "JT808Configuration": { "TcpPort": 808, "UdpPort": 808, "IgnoreMsgIdReply": [] },
  "JT808MsgProducerConfig":       { "TopicName": "JT808Msg",       "BootstrapServers": "127.0.0.1:9092" },
  "JT808MsgReplyConsumerConfig":  { "TopicName": "JT808MsgReply",  "GroupId": "jt808msgreply",
                                    "EnableAutoCommit": true,      "BootstrapServers": "127.0.0.1:9092" },
  "JT808SessionProducerConfig":   { "TopicName": "JT808Session",   "BootstrapServers": "127.0.0.1:9092" }
}
```

Producer 用 **手机号作 Kafka key**：

```csharp
await producer.ProduceAsync(TopicName, new Message<string, byte[]> { Key = terminalNo, Value = data });
```

这正是「同一终端的数据只落到一个分区、被一个消费者顺序处理」的标准做法——**下游可以按 key 做幂等与顺序保证**。这一点你的项目完全没有（无 MQ）。

#### 4.4.2 下行链路怎么闭环

```
业务服务 ──(Kafka: JT808MsgReply)──▶ 网关实例 ──(SessionManager.TrySendByTerminalPhoneNoAsync)──▶ 终端
```

```csharp
// JT808MsgReplyHostedService
JT808MsgReplyConsumer.OnMessage(async item => {
    await JT808SessionManager.TrySendByTerminalPhoneNoAsync(item.TerminalNo, item.Data);
});
```

**注意这里的结构性缺陷**：`JT808MsgReply` 是**普通 topic**（`GroupId = jt808msgreply`，单 group）。如果起 3 个网关实例，3 个实例同组消费，**消息只会被其中一个实例拿到**；而「持有该终端连接的实例」是另一个。结果就是：**下行指令有 2/3 概率投递到没有连接的实例，`TrySendByTerminalPhoneNoAsync` 返回 false，指令静默丢失**。

代码里没有「网关实例 ID / 会话路由表 / 广播+过滤」的任何机制。正确做法应是：按手机号哈希分区 + 消费者只处理自己持有连接的号码，或者用「广播 topic + 实例内过滤」。**这是该项目的最大分布式短板，必须如实标注**。

#### 4.4.3 其它分布式/部署支撑

- **WebApi**（`JT808GatewayApiExtensions`，`app.UseJT808GatewayWebApi()`）：
  - `/{RouteTablePrefix}/UnificationSend`：统一发送（`JT808UnificationSendRequestDto`）
  - `.../Tcp/Session/GetAll`、`SessionTcpByPage`、`RemoveByTerminalPhoneNo`、`QueryTcpSessionByTerminalPhoneNo`
  - `.../Blacklist/Add|Remove|GetAll`
  - `WebApiToken` 默认 `123456`（`JT808TokenFilter`）
  - 配套 `JT808.Gateway.WebApiClientTool`（`JT808HttpClient`）给业务方调用
- **转发（TransmitService）**：`RemoteServerOptions.DataTransfer` 配置 `{ Host, TerminalNos }`，可以「全量转发」或「按终端白名单转发」到另一个网关；每 20 秒巡检断线重连。用于多级平台级联。
- **Docker**：根目录 `Dockerfile` + `docker-compose.yml`。
- **压测**：`JT808.Gateway.Benchmark`（ServerBenchmark + CleintBenchmark，含 SignalR 实时报表）、`JT808.Gateway.Client`（模拟终端：`JT808TcpClient` / `JT808RetryBlockingCollection` / `JT808ReportHostedService`）。

**结论**：架构上「接入层解耦」是对的，但**「下行如何找到正确实例」没有解决**。它是一个「单实例网关 + Kafka 旁路」的方案，不是「可水平扩展的集群网关」。

### 4.5 架构分层与可扩展性

```
JT808.Gateway.Abstractions   ← 接口 + DTO + 默认 Handler + 常量
        ↑
JT808.Gateway                ← TCP/UDP Server + Session + HostedService + WebApi
        ↑
JT808.Gateway.Kafka          ← IJT808MsgProducer/Consumer 的 Kafka 实现
JT808.Gateway.Client         ← 模拟终端 SDK
JT808.Gateway.WebApiClientTool ← 业务方 HTTP 客户端
```

扩展点（全部是 `services.Replace(...)`）：

```csharp
.AddGateway(config)                     // 注册核心（SessionManager/Blacklist/空 Producer）
.AddTcp() / .AddUdp()
.AddMessageHandler<MyHandler>()         // 替换 JT808MessageHandler（同步业务）
.AddMsgProducer<MyProducer>()           // 替换上行生产者
.AddMsgReplyConsumer<MyConsumer>()      // 替换应答消费者（下行入口）
.AddMsgReplyLoggingProducer<T>()
.AddServerKafkaMsgProducer(config)      // Kafka 一键接入
.AddServerKafkaMsgReplyConsumer(config)
.AddServerKafkaSessionProducer(config)
.AddServerKafkaMsgReplyLoggingProducer(config)
```

**二次开发成本**：低。写一个 `IJT808MsgConsumer` 实现即可把数据接到任意存储；写一个 `IJT808MsgReplyProducer` 即可从任意来源下发。**接口粒度设计得相当准**（上行/下行/会话/日志四类，一一对应）。

### 4.6 优点与明显短板

**优点**：
1. **「应答前置 + 业务旁路」的架构**。网关只负责「协议应答」和「原始字节入队」，业务怎么处理都不影响设备体验。这个分层比 yezhihao 的「业务写在 Endpoint 里」更适合大规模设备。
2. **双索引会话 + 同号踢旧连接 + 上线/下线事件**。三件套齐全，是网关的核心正确性。
3. **UDP NAT 映射跟随**。README 专门解释、代码专门实现，这个坑很多平台踩过。
4. **Kafka key = 手机号**。为下游顺序性打下基础。
5. **四类接口分得清**（Msg / MsgReply / Session / Logging），扩展点粒度合适。
6. **黑名单热更新**（`FileSystemWatcher`），运维友好。
7. **自带模拟终端 + Benchmark**，压测不用另找工具。

**短板（具体）**：
1. **下行无法路由到「持有连接的实例」**（4.4.2）。这是**架构级缺陷**，不是配置能解决的。
2. **手工切帧不还原转义**，`7D 02` 会被误判为帧尾；分包重组不做，责任推给下游。
3. **`0x0102` 不鉴权、`0x0100` 不查重**，任何终端都能注册成功。
4. **`0x0801` / `0x8801` 抛 `NotImplementedException`**，多媒体上传链路不可用。
5. **`AcceptAsync` 异常即 `break`**，监听循环静默退出，可用性风险。
6. **Kafka `ProduceAsync` 是 `async void`**，异常只能记日志，无重试、无背压、无本地落盘缓冲——MQ 抖动时数据直接丢。
7. **10 分钟 idle 默认值**对 808 偏大；且 `ActiveTime` 只在 `TryLink` 时刷新，**没有按心跳单独维护**。
8. **无 TLS、无私有协议、无单端口多协议支持**（README 的 FAQ 明确建议「用端口分开」）。
9. **`IgnoreMsgIdReply` 用 `HashSet<uint>`**，配置里写 `[512]` 才能关掉 0x0200 的应答——这个「十进制 512 表示 0x0200」的约定 README 没写清楚，很容易配错。

---

## 5. SmallChi/JT1078

### 5.1 设计目标与定位

README 开篇就把它定位成「视频通讯协议的第三部分」——**设备上传的实时音视频流数据（也就是视频服务器）**，并列出三条子能力：

- 3.1 把 1078 的 H264 数据封装成 **FLV**
- 3.2 封装成 **HLS**
- 3.3 封装成 **fMP4**
- 4. **音频部分暂未实现**（README 原文，但代码里有 `JT1078.Protocol/Audio` 目录，见 5.2.5）

**它不是什么**：
- 不是 808 服务端（没有一条信令处理代码）；
- 不是媒体服务器（没有 RTP 监听、没有转发、没有 RTMP/WebRTC 输出）；
- 不是「开箱即用」的流媒体平台，而是「**把 1078 的 RTP 包变成可播放容器**」的库。

**维护状态**：234 star，**最近 push 2022-12-31**，已停滞近 4 年；91 MB 体积几乎全是 `doc/video` 里的样例视频。`README` 里的 Benchmark 还是 `.NET Core 3.1` 时代的数字。**版本过时，已明确标注**。

### 5.2 协议处理

#### 5.2.1 RTP 包模型（`JT1078Package`）

1078 的「包」不是标准 RTP，而是「RTP 头 + 私有扩展」：

| 字段 | 长度 | 说明 |
| --- | --- | --- |
| `FH_Flag` | 4 | 固定 `0x30 0x31 0x63 0x64`（与苏标附件帧头相同！） |
| `Label1` | 1 | `V(2b) P(1b) X(1b) CC(4b)`，固定 `0x81`（V=2,P=0,X=0,CC=1） |
| `Label2` | 1 | `M(1b) PT(7b)`，M 表示「是否完整帧边界」 |
| `SN` | 2 | 包序号，每包 +1 |
| `SIM` | 6 | BCD[12]，终端 SIM |
| `LogicChannelNumber` | 1 | 逻辑通道号 |
| `Label3` | 1 | `DataType(4b) SubpackageType(4b)` |
| `Timestamp` | 8 | ms；**透传数据时不存在** |
| `LastIFrameInterval` | 2 | 与上个 I 帧间隔；**透传/音频时不存在** |
| `LastFrameInterval` | 2 | 与上一帧间隔；**透传/音频时不存在** |
| `DataBodyLength` | 2 | 数据体长度 |
| `Bodies` | N | H264/音频数据 |

```csharp
// JT1078Serializer.Serialize：注意「条件序列化」
WriteUInt64(package.Timestamp);                    // if DataType != 透传数据
if (DataType != 透传数据 && DataType != 音频帧) {
    WriteUInt16(LastIFrameInterval);
    WriteUInt16(LastFrameInterval);
}
```

**这个条件序列化是 1078 编码最容易写错的地方**——透传数据没有时间戳、音频帧没有帧间隔，写错一位后面全部错位。SmallChi 用一个 `if` 覆盖了，值得照抄这个判断顺序。

`Label3` 语义（枚举 `JT1078DataType` / `JT1078SubPackageType`）：

| DataType | 含义 | SubpackageType | 含义 |
| --- | --- | --- | --- |
| 0000 | 视频 I 帧 | 0000 | 原子包（不可拆） |
| 0001 | 视频 P 帧 | 0001 | 分包的第一个包 |
| 0010 | 视频 B 帧 | 0010 | 分包的最后一个包 |
| 0011 | 音频帧 | 0011 | 分包的中间包 |
| 0100 | 透传数据 | | |

#### 5.2.2 分包合并（`Merge`）

```csharp
private static readonly ConcurrentDictionary<string, JT1078Package> livePackageGroup;
private static readonly ConcurrentDictionary<string, JT1078Package> historyPackageGroup;

public static JT1078Package Merge(JT1078Package pkg, JT808ChannelType channelType) {
    string key = pkg.GetKey();      // "{SIM}_{LogicChannelNumber}"（GetAVKey 已成同义替代）
    var group = channelType == Live ? livePackageGroup : historyPackageGroup;
    switch (pkg.Label3.SubpackageType) {
        case 第一个包: group.TryAdd(key, pkg);  return default;
        case 中间包:   // ArrayPool.Rent(total) → CopyTo → ToArray()，整体复制
        case 最后一个包: var p = group.TryRemove(key); 同样整体复制后返回 p;
        default: return pkg;   // 原子包直接返回
    }
}
```

**问题清单（要如实写）**：
1. **无超时、无容量上限**。终端不发「最后一个包」时，这个 key 永久占内存（`ConcurrentDictionary` 静态字段）。
2. **`+=` 是数组复制**（`tmpPackage.Bodies.CopyTo(tmpSpan)`），**中间包每来一片都整体复制一次**，一个 I 帧被切成 20 片就是 O(n²) 拷贝。
3. **key 不含 streamType**。代码注释已经承认：`due to the agreement cannot distinguish the Stream type, so need clear Stream type at the time of merger, avoid confusion`——**主码流/子码流同通道同时推送会串流**。
4. **`Merge` 是静态方法 + 静态缓存**，多租户/多实例不可用。

#### 5.2.3 H264 解析

`H264Decoder.ParseNALU` 是一个 **`00 00 01` 起始码状态机**（4 状态：0/1/2/3），逐字节扫描 `Bodies` 切出 NALU 列表：

```csharp
case 0: if (value == 0) state = 1; break;
case 1: state = value == 0 ? 2 : 0; break;
case 2:
case 3: if (value == 0) state = 3;
        else if (value == 1 && i < length) { /* 命中起始码，切分 NALU */ }
```

配套 `EXPGolombReader`（指数哥伦布解码，用于从 SPS 里取宽高/profile），`NALUHeader`（解析 `nal_ref_idc` / `nal_unit_type`）。

**评价**：这是 1078 生态里少见的「真的解析 H264」的实现（多数平台直接 `ffmpeg -i rtp://` 转推）。但有代价：
- 只支持 **H264**，不支持 H265（README 明说）；
- 不支持 B 帧重排序（`LastFrameInterval` 被直接塞进 FLV 的 `CompositionTime`，属于「够用」级近似）；
- 起始码扫描是纯 C# 逐字节，**没有 SIMD/查表优化**，高码率多通道时 CPU 会先到瓶颈（推断）。

#### 5.2.4 容器封装

| 项目 | 入口 | 关键类 |
| --- | --- | --- |
| FLV | `FlvEncoder` | `FlvHeader` / `FlvTags` / `AVCDecoderConfigurationRecord` / `FlvBufferWriter` |
| HLS | `TSEncoder` | `TS_Package` / `PES_Package` / `TS_PMT_Package` / `M3U8FileManage` / `M3U8Option` |
| fMP4 | `FMp4Encoder` | `FMp4Box` + 130+ 个 ISO BMFF Box 实现（`MoovBox` / `TrakBox` / `MvexBox` / `MoofBox` / `MdatBox`…） |

`FlvEncoder` 的注释直接给出了使用顺序（**一个客户端一个实例**）：

```
手动编码：
0. EncoderFlvHeader
1. 插入 PreviousTagSize = 0
2. EncoderScriptTag
3. EncoderFirstVideoTag
4. EncoderFirstAudioTag
5. EncoderVideoTag（第二参 false）
6. EncoderAudioTag（第二参 false）
自动编码：
1. EncoderFlvHeader
2. EncoderScriptTag
3. EncoderVideoTag(…, true)     ← true 表示「首帧，需要写 AVC sequence header」
4. EncoderAudioTag(…, true)
```

**FLV 映射关系（README 原文）**：

| JT1078 属性 | FLV 属性 |
| --- | --- |
| `Timestamp` | FLV `Timestamp` |
| `DataType` | FLV `FrameType`（判断关键帧） |
| `LastFrameInterval` | FLV B/P 帧的 `CompositionTime` |

#### 5.2.5 音频（与 README 不符）

README 第 4 条写「音频部分暂未实现」，但源码里有：

```
JT1078.Protocol/Audio/
  AdpcmCodec.cs    G711ACodec.cs    G711UCodec.cs
  Mp3Encoder.cs    FaacEncoder.cs   AudioCodecFactory.cs
  IAudioCodec.cs   IAudioAttachData.cs
```

`AudioCodecFactory` 按编码类型分发；`FaacEncoder` / `Mp3Encoder` 是「封装成 FLV/HLS 需要的 AAC/MP3 帧」。所以准确表述是：**音频解码器有代码，但端到端链路（1078 音频包 → 播放）没有验证/没有示例，README 的说法是保守的**。这条要标注「README 与代码不一致（代码比 README 多）」。

### 5.3 缓存与持久化 / 分布式

- 缓存：仅 `JT1078Serializer` 的两个静态 `ConcurrentDictionary`（live/history 分包组）；
- 持久化：无（不落盘、不落库）；
- 分布式：无（静态单例，无法多实例共享分包状态）；
- 媒体流：**透传思路**——库本身不转发，谁调用谁决定（推到 RTMP、写 MP4、喂给 WebRTC）。

### 5.4 架构分层与可扩展性

```
JT1078.Protocol        ← RTP 包模型 + 序列化 + 分包合并 + H264 解析 + 音频编解码
   ├── JT1078.Flv      ← FLV 封装
   ├── JT1078.Hls      ← TS/HLS 封装 + M3U8
   └── JT1078.FMp4     ← ISO BMFF / fMP4 封装
```

扩展方式：
- 换容器：实现新的 Encoder；
- 换音频编码：实现 `IAudioCodec`；
- 换分包策略：**没有接口**（`Merge` 是静态方法）——这是一个可扩展性缺口。

**二次开发成本**：中。用「解析 + 封装」很省事；要做「多通道并发媒体服务器」需要自己补：
- RTP 监听（UDP/TCP）；
- 会话/通道管理（`SIM + channel + streamType` 三维索引）；
- 分包超时与容量控制；
- 推流（RTMP/HTTP-FLV/WebRTC）；
- 录像（落盘 + 索引）。

### 5.5 优点与明显短板

**优点**：
1. **RTP 字段模型完整**，条件序列化（透传/音频省略字段）处理正确，可直接作为「1078 包结构参考手册」。
2. **真的解析 H264**（NALU + SPS + 指数哥伦布），不是「所有字节都当 Annex-B 扔给 ffmpeg」。
3. **三种容器都有实现**（FLV / HLS / fMP4），其中 fMP4 的 Box 覆盖度非常高（130+ 个 Box）。
4. **单测 + Benchmark 齐全**，`JT1078.Protocol.Test` 对每个字段都有断言。
5. **音频编码器有代码**（G711A/U、ADPCM、MP3、FAAC），比 README 声称的完整。

**短板**：
1. **停滞 4 年**（2022-12），.NET Core 3.1 时代的代码，H265/AV1 全无。
2. **分包合并无超时/无上限/无并发保护/`key` 缺 streamType**，生产环境会内存泄漏 + 串流。
3. **合并是 O(n²) 数组复制**，大 I 帧切成几十片时 CPU/GC 压力明显。
4. **不是媒体服务器**。没有 RTP 监听、没有会话管理、没有推流、没有鉴权、没有限流。
5. **无 1078 信令**（`0x9101/0x9102/0x9201/0x9202/0x9205/0x9206` 都不在本仓库，在 SmallChi/JT808 的 `Extensions.JT1078` 里）。
6. **`README` 的 Benchmark 数字不可用**（.NET Core 3.1、i7-8700K、且作者自己写「只是玩玩，不能当真」）。
7. **无 PS 封装解析**。1078 的 `PT` 负载类型里，很多设备推的是 **PS 流**（`0xE0` 视频 / `0xC0` 音频 PES），本项目直接按「H264 Annex-B」处理 `Bodies`，**遇到 PS 封装的设备会解析失败**（这是一个实际部署中很常见的坑，必须标注）。

---

## 6. 横向对比表

### 6.1 定位与能力矩阵

| 维度 | yezhihao/jt808-server | SmallChi/JT808 | SmallChi/JT808Gateway | SmallChi/JT1078 |
| --- | --- | --- | --- | --- |
| 定位 | 服务端 + SDK | 纯编解码 SDK | 网关骨架 | RTP/容器库 |
| 语言/框架 | Java / Netty + Spring Boot 3 | C# / 无框架 | C# / 裸 Socket + Pipelines | C# / 无框架 |
| 维护 | 活跃（今日有 push） | 活跃（低频） | 活跃（低频） | **停滞 4 年** |
| Star | 829 | 613 | 200 | 234 |
| 808 编解码 | ✅ 注解+反射 | ✅ 手写 formatter | ➖ 复用 JT808.Protocol | ❌ |
| 消息 ID 注册 | 常量+注解扫描 | MsgIdFactory 字典 + 程序集注册 | 复用 | — |
| 分包重组 | ✅（键缺流水号） | ✅（疑似重复拼接） | ❌（推给下游） | ✅（1078 分包，无超时） |
| 转义 | ✅ | ✅ | ⚠️ 切帧不还原 | — |
| 会话管理 | ✅ 单索引、不踢旧连接 | ❌ | ✅ 双索引 + 踢旧连接 | ❌ |
| 鉴权 | ⚠️ 不校验 | ❌ | ⚠️ 无条件成功 | ❌ |
| 心跳/离线 | IdleState 180s | ❌ | ActiveTime + 周期扫描（600s） | ❌ |
| 同步等应答 | ✅ Mono + timeout（无重试） | ❌ | ❌ | ❌ |
| 0x8003 缺片补传 | ✅ 只提供 `getNotArrived()` | ❌ | ❌ | ❌ |
| 1078 信令 | ✅ 消息定义 + HTTP 下发 | ✅ 消息定义（扩展包） | ❌ | ❌ |
| 1078 RTP | ❌ | ❌ | ❌ | ✅ 解析 + 合并 |
| 媒体容器 | ❌ | ❌ | ❌ | ✅ FLV/HLS/fMP4 |
| 苏标附件服务器 | ✅ 完整（独立端口 + 落盘校验） | ✅ 消息定义 | ❌ | ❌ |
| 落库 | ❌（pom 有依赖，代码为空） | ❌ | ❌ | ❌ |
| Redis / MQ | ❌ | ❌ | ✅ Kafka | ❌ |
| 分布式 | ❌ | ❌ | ⚠️ 接入解耦，下行不路由 | ❌ |
| Web 管理接口 | ✅ WebFlux | ❌ | ✅ 内置 WebApi | ❌ |
| 模拟器/压测 | ❌ | ❌ | ✅ Client + Benchmark | ❌ |
| Docker | ❌ | ❌ | ✅ | ❌ |

图例：✅ 有实现；⚠️ 有实现但有关键缺陷；❌ 无；➖ 不适用。

### 6.2 关键实现差异（同一件事的不同做法）

| 主题 | yezhihao | SmallChi/JT808 | 你的 Go 项目 |
| --- | --- | --- | --- |
| 协议建模 | 注解 `@Message/@Field` + 反射 | 每类一个 `JT808MessagePackFormatter<T>` 手写读 | struct + 手写 `Decode/Encode` + `hex.Read*` |
| 版本识别 | 看消息头 bit14（逐包判断） | `JT808Version` 枚举 + `JTT2013Force` 配置 | `0x0100` 消息体长度启发式（2011/2013） |
| 消息分发 | `@Mapping` 注解扫描 | 无（只编解码） | `map[uint16]*action` 表驱动 |
| 分包键 | `clientId/msgId/packageTotal` | `手机号/msgId` | `session + phone + msgId + 首包流水号` ✅ |
| 分包超时 | 20s（listener） | `AutoMergeTimeoutSecond` | 2 分钟（`segmentCacheTTL`） |
| 缺片补传 | 只提供接口 | ❌ | ✅ 300ms 空闲触发 `0x8003`，最多 3 次 ✅ |
| 下行等待 | `Mono` + 30s/10s timeout | ❌ | 各 `*_response_broker` + `ackTimeoutMs` + `maxRetransmissions` ✅ |
| 会话索引 | 仅 clientId | 无 | 仅 sessionID（缺手机号索引） |
| 落盘 | 苏标附件 `.tmp` + `.log` | ❌ | devices/params/properties/runtime 四表 |
| 扩展机制 | `@Endpoint` 类扫描 | 程序集注册 + 策略接口 | `Hooks` 接口 + `repository` 接口 |

---

## 7. 与我的项目（`jt808-server-go`）的差异对比

> 说明：本节按「用户给定基线 + 我实际读到的代码」写。**发现一处与给定基线不符的地方**：`internal/storage/storage.go` 与 `internal/repository/sqlstore/store.go` 已经引用了 `Industry / Region / Alarm / Location` 仓储（`newIndustryRepository`、`newRegionRepository`、`s.AlarmRepository(cfg)`、`s.LocationRepository(cfg)`），但 `internal/repository/sqlstore/` 目录下**没有这些实现文件**，`init_20261101.sql` 也只有 4 张表。**结论：该仓库处于重构中间态，可能暂不可编译**。这一点在对照时要打折，避免拿「未完成的代码」当既成事实。

### 7.1 模块级对照

| 我的模块 | 现状（已核实） | 对标项目的能力 | 差距结论 |
| --- | --- | --- | --- |
| `internal/protocol/packet_codec.go` | 逐字节 `unescape/escape/verify/genVerifier`；校验 `Attr.BodyLength == len(pkt)-Idx`；拒绝加密；body ≤ 1023 | yezhihao 用 `CompositeByteBuf` 切片反转义；SmallChi 用 `Span` | **你的实现更严格**（有长度一致性校验、显式拒绝加密），这两点两个 Java/C# 项目都没有或更宽松。**不必改** |
| `internal/protocol/frame_handler.go` | `bufio.Reader` + `MaxFrameLen = 2+21+1023*2+1+2` | SmallChi Gateway 手扫 `0x7E`，无上限 | **你的更稳**。可借鉴的是「注释里的长度推导公式」写法（已有） |
| `internal/protocol/msg_processor.go` | 71 个 MsgID 表驱动；`processOptions map[uint16]*action{genData, process}` | yezhihao `@Mapping` 注解扫描；SmallChi 无分发 | **你的更可读、零反射**。不建议切注解方案 |
| `internal/runtime/fragment_assembler.go` + `segment_retransmission.go` | 分包缓存 TTL 2 分钟；300ms 空闲触发 `0x8003` 请求缺失片，最多 3 次 | yezhihao 有 `getNotArrived()` 但没实现发送；SmallChi 无 | **这是四个项目都没有的能力**。反向：yezhihao 的 20s TTL 偏短，可参考其「有 listener / 无 listener 两种降级」的兜底思路 |
| `internal/runtime/connection_registry.go` | 仅 `cacheByID[s.ID]`，`Bind/Unbind/Count` + 10s 会话数监控 | SmallChi 双索引 + 同号踢旧；yezhihao 单索引不踢旧 | **缺手机号索引、缺踢旧连接**。设备重连会导致「双活 + 旧连接上行覆盖状态」 |
| `internal/protocol/keepalive.go` | `gron` 按设备 `Keepalive` 周期注册 `CheckDeviceJob` | yezhihao 全连接统一 `IdleStateHandler` 180s；SmallChi 统一 600s 扫描 | **你的更精细**（按设备配置周期）。可借鉴：SmallChi 的「取消读任务 = 断连」比 `Close()` 干净；yezhihao 的「一个统一 idle 兜底」可加一层 |
| `internal/action/hooks.go` + `http.go` | `Hooks`（OnRegister/OnAuth/OnLocation/OnAlarm/OnDisconnect）+ 异步 HTTP（队列 256、单 worker、满则丢、不重试） | SmallChi 四类 Producer/Consumer 接口 + Kafka | **你的 Hooks 只覆盖 5 个事件，且只有 HTTP 一种出口**；SmallChi 的「Msg/Session/MsgReply/Logging 四类接口」更完整 |
| `internal/repository` + `sqlstore` | 契约 + 内存实现 + SQLite/MySQL；`go:embed` 版本化迁移；启动只校验、独立 migrate | 四个项目**都没有落库** | **你的落库能力是这四个项目里最强的**。反面：你的表太少（4 张），缺报警/定位历史/区域/行业 |
| `internal/storage/storage.go` | `BackendMemory/SQLite/MySQL` 装配层 | — | 设计合理，但存在 7.0 说的「引用未实现仓储」问题 |
| `internal/protocol/model/msg_0200.go` | 附加信息 **raw TLV**（`LocationExtItem{ID,Length,Data}`） | SmallChi 二级工厂 + typed parser（苏标 0x64~0x67） | **缺 typed 解析**，业务层拿不到 ADAS/DSM/TPMS 结构化字段 |
| `internal/protocol/model`（1078） | 只有 `0x9205/0x1205` 信令 | yezhihao 有 9101/9102/9201/9202/9205/9206/9207/9301-9306；SmallChi 更全 | **信令覆盖约为 1/8** |
| 媒体 | 无 RTP、无转发 | SmallChi/JT1078 有完整 RTP 解析 + FLV/HLS/fMP4 | **完全空白** |
| 苏标 | 基本未实现 | yezhihao 有完整附件服务器 | **完全空白** |
| 分布式 | 无 | SmallChi Gateway 有 Kafka（但有下行路由缺陷） | **完全空白**（`docs/gateway-mode-design.md` 仍是设计稿） |
| 文档 | `docs/gateway-mode-design.md`、`docs/storage-abstraction-design.md` | 四个项目文档质量参差 | **你的设计文档质量高于四个项目**（A/B/C 三层分类、争议点 C1~C9 都写得很实） |

### 7.2 一句话总结差异

> 你的项目在**编解码严谨性、分包缺片补传、按设备心跳、落库与迁移机制、设计文档**这五件事上**领先四个项目**；
> 在**会话双索引与踢旧连接、0x0200 附加信息 typed 解析、可插拔事件/消息出口、1078 信令覆盖、媒体流、苏标附件、分布式**这七件事上**落后**。

---

## 8. 可直接借鉴的具体做法清单（按优先级）

> 每条统一写：**借鉴什么 / 为什么 / 落到哪个模块 / 工作量 / 风险**。
> 工作量按「熟悉该模块的开发者」估算，单位为人日；风险指「引入后可能踩的坑」。

### P0（正确性/稳定性，建议优先落地）

#### P0-1　0x0200 附加信息「二级工厂 + typed parser + raw 兜底」

- **借鉴什么**：SmallChi `IJT808_0x0200_Factory` / `IJT808_0x0200_Custom_Factory` 的两层注册 + 未注册 ID 退回 raw 的做法；苏标 `0x64/0x65/0x66/0x67` 的字段定义可直接照抄（`AlarmId / FlagState / AlarmOrEventType / Speed / Altitude / Latitude / Longitude / DateTime / VehicleNo / VehicleType`）。
- **为什么**：现在的 `LocationExtItem` 只存 raw，`OnAlarm` 回调里 `alarmSign != 0` 只能告诉业务「有报警」，说不出「是 ADAS 还是 DSM、报警类型几」。苏标落地绕不开这块。而且 raw 兜底能保证「不认识的 ID 不丢包」，比「全量强类型」安全。
- **落到**：`internal/protocol/model/msg_0200.go`（新增 `map[uint8]func([]byte) (any, error)` 注册表 + `ParseExtItems()`）；`internal/action/hooks.go`（`AlarmEvent` 增加 `ExtItems []Any`）。
- **工作量**：2~3 人日（含苏标 4 个 ID 的字段映射与单测）。
- **风险**：附加信息 ID 在不同标准（808 2019 / 苏标 / 粤标）里有重号（如 `0x64` 苏标=ADAS，粤标也有 `0x64`），必须**按协议域注册**而不是全局注册，否则会串。

#### P0-2　会话双索引（`手机号 → Session`）+ 同号踢旧连接

- **借鉴什么**：SmallChi `JT808SessionManager.Sessions` / `TerminalPhoneNoSessions` 双字典 + `TryLink` 里 `cacheSession.Close()`。
- **为什么**：你现在的 `connection_registry.go` 只有 `sessionID` 索引。两个直接后果：
  1. 业务要下发指令，必须先知道 sessionID（HTTP API 必须由调用方传，不能只给手机号）；
  2. 设备断网重连后，旧连接仍存活到心跳超时，**旧连接上报的 `0x0200` 会覆盖新连接的最新状态**（你的 `device_runtime` 表会来回跳）。
- **落到**：`internal/runtime/connection_registry.go`（新增 `cacheByPhone map[string]*model.Session`，`Bind` 时若已有同号且 `ID` 不同 → `old.Conn.Close()` + 触发 `OnDisconnect`）；`internal/protocol/msg_processor.go` 的 `processMsg0100/0102` 调用点。
- **工作量**：1.5 人日。
- **风险**：**踢旧连接会触发 `OnDisconnect` 回调**，业务侧可能把「重连」误判成「下线」；需要区分「被动断开」与「被新连接顶替」（建议 `DisconnectEvent` 加 `Reason` 字段，值 `superseded`）。另外要防死锁：`Bind` 持锁时不要回调业务。

#### P0-3　分包缓存容量上限（防御性）

- **借鉴什么**：yezhihao 的 `MultiPacketListener(20)`（超时清理）+ 你已有的 `segmentCacheTTL`，再补「全局容量上限 + 单 key 上限」。
- **为什么**：你已有 2 分钟 TTL，但没有容量上限。恶意/故障终端持续发「分包首包」可以堆满内存（每个 state 持有 `[]byte`）。这是**线上最容易被触发的内存问题**。
- **落到**：`internal/runtime/fragment_assembler.go`（新增 `maxStates`（如 10k）、`maxBytesPerState`（如 64KB）、超限时淘汰最旧 state 并 `log.Warn`）。
- **工作量**：0.5~1 人日。
- **风险**：淘汰阈值定太小会误杀正常慢速终端（弱网下分包间隔可能 >30s）；建议做成配置项 `server.fragment.maxStates`，默认值保守。

#### P0-4　Kafka/消息总线抽象（为 gateway 模式铺路）

- **借鉴什么**：SmallChi 的四类接口划分（`IJT808MsgProducer` / `IJT808MsgConsumer` / `IJT808MsgReplyProducer` / `IJT808SessionProducer`）+ **Producer 用手机号作 Kafka key**。
- **为什么**：你的 `docs/gateway-mode-design.md` 已经把 gateway 的 29 个上行 MsgID、42 个下行忽略矩阵、5 类事件契约写清楚了，缺的只是「出口」。现在 `Hooks` 只有 HTTP 一种实现，扩展成「可注册多个 Sink」成本极低。
- **落到**：`internal/action/`（新增 `Sink` 接口 + `MultiSink` 聚合 + `KafkaSink`）；`internal/config`（`action.sinks` 列表）。注意**不要**照抄 SmallChi 的 topic 命名（`jt808msgdefault` 这种），按你自己的命名规范来。
- **工作量**：3~4 人日（含 Kafka 客户端选型 `segmentio/kafka-go`、优雅关闭、`async` 失败落本地文件兜底）。
- **风险**：**SmallChi 的 `async void ProduceAsync` 是反面教材**——必须用「有界队列 + 背压 + 失败落盘」，否则 MQ 抖动时静默丢数据。另外「Kafka key = 手机号」要配合「下游按 key 幂等」，不要假设 exactly-once。

#### P0-5　苏标附件服务器（独立端口 + 随机写 + 块日志完整性校验）

- **借鉴什么**：yezhihao `FileService` 的完整设计：独立端口 `7200`、帧头 `0x30 0x31 0x63 0x64`、`offset+length+data` 随机写、`.log` 记录已收块、完成时排序找间隙、`0x9212` 回报缺失偏移。
- **为什么**：这是苏标落地的**唯一正确姿势**——「按顺序收、错了整包重来」在弱网下永远传不完。而且这块实现是**纯 IO，与你的 Go 架构天然契合**，没有框架包袱。
- **落到**：新增 `internal/alarmserver/`（`server.go` 监听 + `assembler.go` 文件组装 + `file_store.go`）；`internal/protocol/model/` 补 `Msg1210/1211/1212/9208/9212/0200_0x64~0x67`；`internal/runtime` 复用 connection registry。
- **工作量**：6~8 人日（含 0x1210 文件清单缓存、补传（`type=1`）分支、并发同文件保护）。
- **风险**：
  1. **同目录多文件并发写**需要 `sync.Map[fileName]*fileState` + 单文件互斥；
  2. **落盘路径要防路径穿越**（文件名来自终端，必须 `filepath.Base` + 白名单字符）；
  3. yezhihao 用「一个 `.log` 记录所有块」在文件很多时读放大明显，Go 侧建议改成「按块位图」或「SQLite 索引」。

### P1（能力补齐，建议在做完 P0 后启动）

#### P1-1　1078 信令补全（先信令、后媒体）

- **借鉴什么**：yezhihao `T9101/T9201/T9205/T9208` 的字段定义（尤其 `T9201` 的 `storageType/playbackMode/playbackSpeed/startTime/endTime`）；SmallChi `Extensions.JT1078` 的 34 个消息体覆盖表。
- **为什么**：你现在只有 `0x9205/0x1205`。而 1078 的**信令是媒体的前置条件**——没有 `0x9101/0x9201`，终端不会推流，媒体服务器再强也没数据。
- **落到**：`internal/protocol/model/` 新增 15~20 个消息体；`internal/protocol/msg_processor.go` 注册对应上行（`0x1003/0x1005/0x1205/0x1206`）；HTTP 下发接口补 `9101/9102/9201/9202/9206/9207`。
- **工作量**：4~6 人日（消息体纯字段，属于体力活）。
- **风险**：`0x1005`（终端上传音视频资源列表）字段多且可变（`AVResource` 列表），不同厂商实现差异大——建议先按官方文档实现 + 保留 `Raw []byte` 兜底。

#### P1-2　1078 RTP 接入与「PS/H264」分流

- **借鉴什么**：SmallChi `JT1078Package` 的字段顺序与**条件序列化**（透传/音频省略 `Timestamp`、`LastIFrameInterval`、`LastFrameInterval`）；`Label3` 的 `DataType × SubpackageType` 组合表；`H264Decoder` 的 `00 00 01` 状态机。
- **为什么**：这是从「信令平台」走向「视频平台」的分水岭。但**不要照抄 SmallChi 的分包合并**（5.2.2 的 4 个问题：无超时/无上限/O(n²)/key 缺 streamType）。
- **落到**：新增 `internal/media/rtp.go`（UDP 监听 + 包解析）、`internal/media/frame.go`（按 `SIM+channel+streamType` 三维索引 + 超时淘汰）、`internal/media/nalu.go`（NALU 切分）。
- **工作量**：5~7 人日（只到「解析出完整帧 + 落盘/推 HTTP-FLV」）。
- **风险**：
  1. **PS 封装**：Many 设备 `PT` 是 PS（`0xE0/0xC0` PES），SmallChi 直接当 H264 处理是**错的**；必须先判 payload 类型，PS 要走 PES 解封装再取 NALU。
  2. **通道 × 码流 的会话数**：一辆车 4 通道 × 2 码流 = 8 条流，1 万车就是 8 万条并发 UDP「会话」，内存与 FD 都要算。
  3. **丢包**：RTP 走 UDP 天然丢包，`SN` 不连续时要能检测并让终端重传（1078 没有协议级重传，只能靠上层 `0x9102/0x9101` 重开）。

#### P1-3　可插拔事件出口（多 Sink + 至少一种 MQ 实现）

- 已在 P0-4 覆盖接口抽象，P1 补「第二个实现」验证抽象是否成立（例如 Kafka 或 RocketMQ，或用 `nats`/`redis stream` 更轻）。
- **落到**：`internal/action/kafka.go` 或 `internal/action/redis_stream.go`。
- **工作量**：2~3 人日。
- **风险**：多 Sink 后「谁失败谁成功」的语义要定义清楚（建议 at-most-once + 失败计数指标，不做重试）。

#### P1-4　报文解释器 / JSON 诊断

- **借鉴什么**：yezhihao 的「报文解释器」与 SmallChi 的 `Analyze`：把 hex 解析成「每个位都有中文含义」的 JSON。
- **为什么**：你已经有 71 个 MsgID 与完整 struct，**生成这份 JSON 只差一个 `analyze` 命令**。排障时比 `zerolog` 打 hex 高效得多，也能直接给对接厂商看。
- **落到**：`cmd/jt808-analyze`（`--hex` 输入 → JSON 输出）；复用现有 `Decode` + 反射读 struct tag 生成字段名。
- **工作量**：1~2 人日。
- **风险**：反射 + `encoding/json` 对 `[]byte` 默认输出 base64，需要自定义 `MarshalJSON` 输出 hex。

#### P1-5　按域注册的「协议扩展包」机制（粤标/苏标可选编译）

- **借鉴什么**：SmallChi `IJT808ExternalRegister.Register(Assembly)`——核心库不认识扩展，扩展自己注册。
- **为什么**：你未来要支持粤标/苏标/地方标准，如果全塞进 `internal/protocol/model`，包会膨胀且编译时间上升。Go 的对等做法是「`internal/protocol/ext/<std>` 包 + `init()` 调用 `protocol.Register(...)`」，用 build tag 控制。
- **落到**：`internal/protocol/registry.go`（暴露 `Register(id uint16, factory func() Msg)`）+ `internal/protocol/ext/{subiao,yuebiao}/`。
- **工作量**：1~2 人日（只搭骨架）。
- **风险**：`init()` 隐式注册会让「消息 ID 冲突」在运行时才暴露；建议加「重复注册 panic + 启动时打印完整注册表」。

### P2（工程化/运维，可选）

| 项 | 借鉴来源 | 落到 | 工作量 | 风险 |
| --- | --- | --- | --- | --- |
| 黑名单 + 热更新 | SmallChi `JT808BlacklistManager`（`blacklist.ini` + `FileSystemWatcher`） | `internal/config` + `internal/protocol/pipeline.go` 入口处拦截 | 1 人日 | 读文件热更新在多实例下不一致，建议加「管理接口推送」 |
| Web 管理接口（会话查询/统一下发/黑名单） | SmallChi `JT808GatewayApiExtensions` + `JT808TokenFilter` | 现有 `internal/api` 补 `/sessions`、`/send`、`/blacklist` | 2~3 人日 | 下发接口必须带幂等键，否则前端重试会重复下发 |
| 模拟终端增强 | SmallChi `JT808.Gateway.Client`（`JT808ReportHostedService` + `JT808RetryBlockingCollection`） | 你已有 `cmd/jt808-client-go`，补「N 终端并发 + 报表」 | 2 人日 | 压测客户端与被测服务同机时会相互抢 CPU，数据失真 |
| Docker 化 | SmallChi `Dockerfile` + `docker-compose.yml` | 仓库根目录 | 0.5 人日 | SQLite 场景要挂 volume，且 WAL 在容器里要 `journal_mode=WAL` 才安全（你已有） |
| 会话/报文指标（Prometheus） | 四个项目都没有（只有 Benchmark） | `internal/server` + `expvar`/`promhttp` | 2 人日 | 高基数标签（手机号）会打爆 Prometheus，只能打「总数/按协议版本」 |

---

## 9. 明确不建议照搬的地方及理由

1. **不要照搬 yezhihao 的「注解 + 反射」编解码到 Go**
   - 理由：Go 没有注解，只能靠 struct tag + 反射；而反射解析 `[]byte` 的分配与 `time` 开销比 Java 的 `RuntimeSchema` 预编译更差，且会失去「`Decode` 里能写业务校验」的灵活性。
   - 你 README 里已经在 WIP「调研 struct tag 简化编解码」——**建议只借鉴 protostar 的字段 DSL 设计（`lengthUnit/totalUnit/lengthExpression/version`），不要借鉴运行机制**。

2. **不要照搬 yezhihao 的业务层（`0x0102` 不校验、`0x0200` 空实现、MyBatis 依赖但零 Mapper）**
   - 理由：那是演示骨架。你的 `processMsg0102` 已经做了真实设备查询与错误返回，比它正确。
   - 唯一可借的是 `@AsyncBatch` 的**批量语义**（`poolSize/maxElements/maxWait`）——但你的 `internal/action` 目前是逐条回调，若要批量入库应另设 `BatchSink`，而不是改协议层。

3. **不要照搬 SmallChi Gateway 的手工切帧**
   - 理由：不还原转义（`7D 02` 误判帧尾）、无长度上限、`AcceptAsync` 异常即 `break`。你的 `bufio.Reader` + `MaxFrameLen` + 长度一致性校验都比它稳。
   - 可借的只有「按 `0x7E` 双标记切段」的**思路**（用于实现「非标准帧/私有帧」的兼容层），不是代码。

4. **不要照搬 SmallChi Gateway 的 Kafka 下行方案**
   - 理由：单 group 消费 + 无实例路由 = 多实例下行必丢（4.4.2 已推演）。如果要做 gateway 集群，正确顺序是：**先做「会话归属路由表」**（Redis 存 `phone → nodeId` 租约），再让下行消费者按 `phone` 查表转发，或让每实例只消费「自己持有号码」的分区。
   - 你在 `docs/storage-abstraction-design.md` 里已经写了「owner 租约、presence、CommandBus、ReplyRoute」——**方向比 SmallChi 正确**，照自己的文档做。

5. **不要照搬 SmallChi/JT1078 的分包合并与静态单例**
   - 理由：无超时/无上限/O(n²) 拷贝/key 缺 streamType/静态字段（多实例不可用）。你新建 `internal/media` 时应按「三维索引 + TTL + 容量上限 + 单帧只分配一次」重写。

6. **不要把 1078 的 `Bodies` 一律当 H264 Annex-B**
   - 理由：设备可能推 PS 封装，直接扫 `00 00 01` 会解析失败或切出垃圾 NALU。必须先看 `PT` 与 payload 首字节（`0x000001BA` = PS pack start）分流。

7. **不要照搬「把业务写进网关进程」的形态（`AddMessageHandler<T>`）**
   - 理由：README 自己也说「改业务要重启网关」，只适合设备少、允许丢数据的场景。你的 `Hooks` + 内存后端是更好的起点，**不要为了「像 SmallChi」而把存储塞回协议进程**。

8. **不要照搬「YSATL 附件落本地盘 + `.log`」到分布式场景**
   - 理由：本地盘在多实例下不可共享，`0x9212` 的「缺失偏移」会因换实例而永远补不齐。多实例场景应改为「对象存储 + 分片清单（DB 位图/Redis bitmap）」。单机场景可以照抄。

9. **不要照搬「用十进制配消息 ID」的配置习惯**
   - 理由：SmallChi 的 `IgnoreMsgIdReply: [512]` 表示 `0x0200`，极易配错且无校验。你的配置项应统一用十六进制字符串（`"0x0200"`）并在启动时校验。

10. **不要照搬 `WeakHashMap` / GC 兜底做分包超时**
    - 理由：行为不可预测，压测时表现与生产差异大。你已有显式 TTL，保持显式。

---

## 10. 结论：四个项目里最值得参考的是谁、参考哪一部分

**一句话结论**：**没有「一个项目全都要」，但有「三段最值得抄的局部」——分别来自 yezhihao、SmallChi/JT808、SmallChi Gateway，而 SmallChi/JT1078 只值得当「协议字段手册」。**

按「对你项目的实际价值」排序：

### 第 1 名：yezhihao/jt808-server —— 值得抄「苏标附件服务器」和「请求-应答关联」

- **苏标附件服务器（`FileService` + `JSATL12Endpoint` + `DataFrameMessageDecoder`）**：这是四个项目里**唯一一个「可直接落地到生产」的完整业务实现**。随机写 + `.log` 块记录 + 间隙检测 + `0x9212` 缺失偏移回报，这套设计能真正支持弱网下的断点续传。你目前苏标是空白，**这是最高 ROI 的参考点**（对应 P0-5）。
- **`Session.request(...).timeout()` 的应答关联**：思路值得学（键 = 应答类 + 流水号、`onDispose` 清理、重复请求拒绝），但**不要照抄实现**——你的 `*_response_broker` + `maxRetransmissions` 已经更强，只需检查「重复请求拒绝」和「Channel 关闭时清理 pending」这两点是否缺失。
- **`getNotArrived()`**：你已经实现了更完整的 `0x8003` 补传，**这一项你领先**，不用抄。

### 第 2 名：SmallChi/JT808 —— 值得抄「0x0200 二级工厂」和「按程序集注册扩展」

- **`IJT808_0x0200_Factory` + `IJT808_0x0200_Custom_Factory`**：typed parser + raw 兜底 + 业务方可插拔，是解决「附加信息 ID 越来越乱」的标准方案（对应 P0-1）。
- **`IJT808ExternalRegister.Register(Assembly)` + `AddJT1078Configure()`**：核心库不认识扩展、扩展自己注册。这是你未来做粤标/苏标/地方标准的**分层模板**（对应 P1-5）。
- **`Analyze` 报文解释器**：刚需，成本极低（对应 P1-4）。
- 顺带：它的 1078/苏标/粤标**消息体字段定义**可以直接当「对照表」用，省去翻文档的时间；但**不要抄 `DefaultMerger`**（疑似重复拼接）。

### 第 3 名：SmallChi/JT808Gateway —— 值得抄「双索引会话 + 踢旧连接」和「四类接口划分」

- **`TryLink` 的「同号踢旧连接」+ 上线/下线事件**：直接解决你「设备重连双活」的问题（对应 P0-2），代码只有几十行，收益很大。
- **四类接口划分（Msg / MsgReply / Session / Logging）**：给你 `Hooks` 扩展成多 Sink 提供了现成的语义分类（对应 P0-4）。
- **UDP「跟随设备消息下发」**：如果你以后做 UDP 网关，这个 NAT 映射细节必须实现。
- **但它的 Kafka 下行路由是坏的**（4.4.2），**只能抄「上行入队」，不能抄「下行出队」**。

### 第 4 名：SmallChi/JT1078 —— 只值得当「协议字段手册」

- **`JT1078Package` 字段顺序 + 条件序列化规则 + `Label3` 组合表**：写 Go 的 RTP 解析时逐字段对照，能少踩很多坑（对应 P1-2）。
- **`H264Decoder` 状态机**：思路可参考，但 Go 侧更推荐用「`bytes.Index` + 手工扫描」或成熟库，不必逐字节 C# 风格移植。
- **容器封装（FLV/HLS/fMP4）**：不建议自己写。**优先用 `ffmpeg`/`go-astiav`/`mediamtx` 做推流与封装**，把 Go 服务端只做「RTP 解析 + 帧组装 + 推给媒体服务器」。理由：容器封装的坑（时间戳、B 帧、SPS/PPS 缓存）远比「解析 RTP」多，而这个库已经停滞 4 年。
- **不要抄它的分包合并**（5.2.2 四个问题）。

### 最后一句

如果只能选一个项目读一遍，**读 yezhihao/jt808-server**：它的协议建模（注解 DSL）、请求-应答关联、苏标附件服务器三块，覆盖了「协议层 + 会话层 + 文件类业务」的完整链路，且代码质量与可读性最好。

如果只能抄一个模块，**抄 yezhihao 的苏标附件服务器**：它是四个项目里唯一「协议 + 存储 + 可恢复性」都闭环的实现，也正好是你当前最大的空白。

如果你的目标是「做大规模型网关」，**参考 SmallChi Gateway 的架构分层（接入/业务解耦 + Kafka key=手机号），但下行路由必须自己按 `docs/storage-abstraction-design.md` 的 owner 租约方案重做**——不要指望 SmallChi 的 Kafka 方案能直接水平扩展。

---

## 附录 A：本次调研的关键源码索引（供后续查证）

| 主题 | 文件 |
| --- | --- |
| yezhihao 消息基类/位运算 | `jtt808-protocol/src/main/java/org/yzh/protocol/basics/JTMessage.java` |
| yezhihao 解码/转义/分包 | `…/codec/JTMessageDecoder.java`、`MultiPacket.java`、`MultiPacketDecoder.java` |
| yezhihao 注解 DSL | `protostar/src/main/java/io/github/yezhihao/protostar/annotation/Field.java`、`schema/SchemaManager.java` |
| yezhihao 分发 | `netmc/.../core/DefaultHandlerMapping.java`、`handler/DispatcherHandler.java` |
| yezhihao 请求-应答 | `netmc/.../session/Session.java`（`request/response`） |
| yezhihao 会话表 | `netmc/.../session/SessionManager.java`（`add` 覆盖不关旧连接） |
| yezhihao 苏标 | `jtt808-server/.../endpoint/JSATL12Endpoint.java`、`service/FileService.java` |
| yezhihao 1078 信令 | `jtt808-server/.../controller/JT1078Controller.java`、`endpoint/JT1078Endpoint.java` |
| yezhihao 配置 | `jtt808-server/src/main/resources/application.yml`、`web/config/JTProperties.java` |
| SmallChi 头/属性 | `src/JT808.Protocol/JT808Header.cs`、`JT808HeaderMessageBodyProperty.cs` |
| SmallChi 组包/解包 | `src/JT808.Protocol/JT808Package.cs`、`JT808Serializer.cs` |
| SmallChi 策略 | `src/JT808.Protocol/Internal/DefaultSplitPackageStrategyImpl.cs`、`DefaultMerger.cs`、`DefaultMsgSNDistributedImpl.cs` |
| SmallChi 扩展注册 | `src/JT808.Protocol.Extensions.JT1078/DependencyInjectionExtensions.cs` |
| SmallChi 苏标字段 | `…Extensions.SuBiao/MessageBody/JT808_0x1210.cs`、`JT808_0x0200_0x64.cs` |
| Gateway 网络层 | `src/JT808.Gateway/JT808TcpServer.cs`（Pipe + 手扫 0x7E） |
| Gateway 会话 | `src/JT808.Gateway/Session/JT808SessionManager.cs`（`TryLink`） |
| Gateway 应答 | `src/JT808.Gateway.Abstractions/JT808MessageHandler.cs`（19 项表） |
| Gateway 离线 | `src/JT808.Gateway/Services/JT808TcpReceiveTimeoutHostedService.cs` |
| Gateway MQ | `src/JT808.Gateway.Kafka/JT808MsgProducer.cs`、`JT808MsgReplyConsumer.cs` |
| Gateway 配置 | `src/JT808.Gateway.Abstractions/Configurations/JT808Configuration.cs`、`simples/.../appsettings.json` |
| 1078 RTP 模型 | `src/JT1078.Protocol/JT1078Package.cs`、`JT1078Serializer.cs` |
| 1078 分包 | `src/JT1078.Protocol/JT1078Serializer.cs`（`Merge`） |
| 1078 H264 | `src/JT1078.Protocol/H264/H264Decoder.cs` |
| 1078 容器 | `src/JT1078.Flv/FlvEncoder.cs`、`src/JT1078.Hls/TSEncoder.cs`、`src/JT1078.FMp4/FMp4Encoder.cs` |

## 附录 B：本次调研未能覆盖的部分（如实标注）

1. **未运行任何项目**，因此「吞吐/内存/并发正确性」全是静态推断。SmallChi 的 `DefaultMerger` 重复拼接问题需要实测确认。
2. **未核实 SmallChi 各 NuGet 包的实际版本号与 NuGet 上的发布状态**（README 的徽章是动态的，抓不到具体值）。
3. **未核实 yezhihao 是否存在「闭源商业版」**。README 未提及；但开源版显然只到「演示骨架」，完整的业务实现（鉴权、落库、流媒体、集群）不在本仓库——这一点以代码为准，不做商业版猜测。
4. **未深入 `JT808.Gateway.Client` / `Benchmark` 的压测细节**（只确认存在与用途），因为与你的项目「借鉴清单」关系不大。
5. **未逐一验证苏标/粤标消息体字段与官方文档的一致性**（只抽样读了 `0x1210/0x0200_0x64/0x9101`）；正式实现时仍需以官方标准原文为准。
