#!/usr/bin/env bash
# 校验 docs/protocol-coverage.md 与注册表是否一致；入口为 make check-registry-doc。
# 注册表变更后先运行 `go run ./tools/registrydoc -write` 刷新自动生成段。
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run ./tools/registrydoc -check
