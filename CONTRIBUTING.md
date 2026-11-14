# 贡献指南

感谢你愿意改进 jt808-server-go。本项目是一个用 Go 编写的 JT/T 808 终端接入服务，同时覆盖苏标主动安全附件、JT/T 1078 音视频与集群部署。提交前请先阅读本指南，避免把时间花在返工上。

## 开始之前

- 先搜索已有 [Issue](https://github.com/fakeyanss/jt808-server-go/issues) 与 Pull Request，避免重复工作。
- 较大的功能、协议行为变更或集群接口调整，请先开 Issue 说明场景、边界与验证方式，达成一致后再写代码。
- 安全漏洞不要走公开 Issue，请按 [SECURITY.md](SECURITY.md) 的私密渠道报告。

## 环境要求

- Go 1.26+
- `make`、`bash`
- 可选：`sqlite3`（检查本地库）、Docker（验证镜像与 compose）

默认配置使用 `memory` 后端，不需要数据库即可跑通接入链路。

## 分支与提交

1. 从最新 `main` 拉出主题分支，命名建议 `dev/<主题>`，例如 `dev/protocol-0x8105`、`dev/media-playback`。
2. 不要直接向 `main` 提交；通过 Pull Request 合并。
3. 一个提交只做一件事。提交信息说明“为什么改”，而不只是“改了什么”，格式建议：

   ```text
   feat(media): 支持按通道查询录像覆盖区间
   fix(gateway): control 不可达时按有界宽限放行缓存档案
   docs(coverage): 补记 0x9207 并刷新注册表自动生成段
   refactor(attachment): 附件节点不连库，进度与恢复点交由 control
   ```

   常用类型：`feat`、`fix`、`refactor`、`docs`、`test`、`ci`、`chore`。作用域可用模块名，如 `protocol`、`server`、`storage`、`cluster`、`media`、`attachment`、`api`。

4. 不提交构建产物、数据库文件、日志、真实配置或密钥。提交前检查 `git status`，只暂存本次相关的文件。

## 提交前检查

在仓库根目录执行：

```sh
gofmt -l .                      # 应无输出
go build ./...
go vet ./...
go test -race -count=1 ./...
./scripts/check-coverage.sh     # 协议注册表与覆盖矩阵一致
```

涉及端到端链路时再执行：

```sh
make smoke                      # 构建真实 server/client 二进制并跑 5 个场景
```

`make smoke` 会占用默认端口，可用 `SMOKE_TCP_PORT`、`SMOKE_UDP_PORT`、`SMOKE_HTTP_PORT` 覆盖；需要保留日志时加 `--keep-logs`。

CI 会在 `main`、`dev/**` 的 push 与 Pull Request 上执行 `make all`、覆盖矩阵校验和 outbox 回归测试。PR 合并前必须全绿。

## 协议消息开发

新增或修改一条 JT/T 808 / 苏标 / 1078 消息时，按以下顺序处理：

1. 在 `internal/protocol/model/` 新建或修改 `msg_XXXX.go`，实现 `Decode` / `Encode`，按 `Header.Attr.VersionDesc` 处理 2011/2013/2019 的字段差异；
2. 在 `internal/protocol/msg_processor.go` 的注册表登记 MsgID，绑定请求/应答模型与处理函数；
3. 如果消息有归属差异，同步更新 `internal/protocol/ownership.go`：网关项、管控项、双方项必须与注册表一致；
4. 在处理函数中完成业务逻辑与应答生成。需要落库时只调用 `internal/repository` 的接口，不直接写 SQL；
5. 补单元测试。模型测试覆盖编解码往返与版本差异，处理器测试覆盖业务分支；涉及连接、分包或应答配对时，在 `internal/server/` 或端到端测试中补场景；
6. 刷新覆盖矩阵：

   ```sh
   go run ./tools/registrydoc -write
   ./scripts/check-coverage.sh
   ```

   自动生成段位于 `docs/protocol-coverage.md` 的 `BEGIN/END GENERATED REGISTRY` 标记之间，**不要手工编辑**。手工状态表需要按代码实际行为更新；状态定义见该文档。

## 存储与迁移

- 仓储接口定义在 `internal/repository/`，SQL 方言实现分别在 `internal/repository/sqlite/` 与 `internal/repository/mysql/`。
- 迁移文件名格式为 `v<MAJOR>.<MINOR>.<PATCH>.sql`，两套目录版本号必须同步推进。
- 每个迁移文件只写本后端的方言：SQLite 使用 `TEXT`/`BLOB` 与 `ON CONFLICT`，MySQL 使用显式 `ENGINE=InnoDB`/`utf8mb4` 与方言类型。
- 版本按三段数值比较（`1.10.0` > `1.9.0`），不要用字符串排序。
- 新增表或字段时，同步检查 `internal/repository/` 的仓储接口、memory 实现与 SQL 实现，避免只改一端。
- 主服务启动只校验 schema 版本，不执行 DDL；迁移由 `cmd/jt808-migrate` 独立执行。改迁移后请本地跑一次升级路径。

## 集群模式开发

- 五个角色二进制分别是 `jt-server-standalone`、`jt-signal`、`jt-media`、`jt-control`、`jt-attachment`，固定角色与样例配置见 `configs/cluster/`。
- 集群改动至少验证两类形态：
  - Standalone：不配置 `cluster` 段时行为、端口与外部依赖不变；
  - Cluster：按 `configs/cluster/` 的样例启动相关角色，确认启动校验、探针与关键接口。
- 集群内部 HTTP 契约、会话目录、节点注册与下行路由的语义以 `docs/architecture.md` 为准；改契约时同步更新文档与配置示例。
- 集群元数据面只允许 control 连数据库；gateway、media、attachment 不应新增数据库依赖。

## 代码与文档约定

- 日志与 `error` 信息使用英文，便于检索与对接；注释、文档、测试业务数据使用中文。
- 描述上行可靠性时统一用“应答时机”，不混用“时序”与“时机”。
- 协议字段以标准为准，版本差异通过消息头判别后分支处理，不做无依据的兼容猜测。
- 面向使用者的文档放在 `docs/`：
  - `installation.md`：安装与部署
  - `configuration.md`：配置说明
  - `architecture.md`：架构与集群模式
  - `protocol-coverage.md`：协议覆盖（含自动生成段）
  - `api.md`：业务 API
  - `operations.md`：运维手册

  `docs/archive/` 保存历史设计与研究文档，不再维护，除修正事实性错误外不要改写。

## Pull Request

- PR 描述按 [.github/PULL_REQUEST_TEMPLATE.md](.github/PULL_REQUEST_TEMPLATE.md) 填写，写清行为变化、兼容性与验证方式。
- 关联相关 Issue；如果 PR 依赖其他未合入的分支，请在描述中明确依赖关系与合并顺序。
- 只包含本次主题的改动，不混入无关格式化或重构。
- UI/接口/配置变更请在描述中给出调用示例或配置片段；集群行为变更请说明 Standalone 是否受影响。
- 评审意见通过新增提交处理，不要用 force push 覆盖评审历史，除非维护者明确要求。

## 许可证

提交贡献即表示你同意以本仓库的 [MIT License](LICENSE) 授权你的贡献。
