#!/usr/bin/env bash
# 校验 docs/JT808-2019-coverage.md 与 internal/protocol/msg_processor.go 的注册表是否一致。
# 注册表变更后先运行 `go run ./tools/registrydoc -write` 刷新自动生成段。
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run ./tools/registrydoc -check
