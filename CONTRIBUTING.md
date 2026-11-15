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

## 代码结构与扩展

本文只覆盖日常小改动与修复的流程。目录职责、分层与接口实现对应关系、依赖方向，以及新增仓储、数据库后端、业务模块与协议消息的步骤与最小示例，统一见[docs/codebase.md](docs/codebase.md)；系统架构、角色边界、部署形态与链路图见[docs/architecture.md](docs/architecture.md)。

## 代码与文档约定

- 日志与 `error` 信息使用英文，便于检索与对接；注释、文档、测试业务数据使用中文。
- 描述上行可靠性时统一用“应答时机”，不混用“时序”与“时机”。
- 协议字段以标准为准，版本差异通过消息头判别后分支处理，不做无依据的兼容猜测。
- 面向使用者的文档放在 `docs/`：
  - `installation.md`：安装与部署
  - `configuration.md`：配置说明
  - `architecture.md`：系统架构、角色边界与部署形态
  - `codebase.md`：代码结构、依赖方向与扩展步骤
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
