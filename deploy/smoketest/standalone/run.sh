#!/usr/bin/env bash
# standalone 冒烟一键入口：memory 后端 + 客户端容器。
#
# 用法：
#   ./run.sh          # 启动（默认）
#   ./run.sh down     # 停止并删除容器、卷、网络
#   ./run.sh logs     # 跟踪日志
#   ./run.sh ps       # 查看状态
#
# 可选环境变量：JT808_SMOKE_IMAGE、JT808_SMOKE_CLIENT_IMAGE
set -euo pipefail

cd "$(dirname "$0")"

if command -v docker-compose >/dev/null 2>&1; then
  DC=(docker-compose)
else
  DC=(docker compose)
fi

export JT808_SMOKE_IMAGE=${JT808_SMOKE_IMAGE:-fakeyanss/jt808-server-go:latest}
export JT808_SMOKE_CLIENT_IMAGE=${JT808_SMOKE_CLIENT_IMAGE:-fakeyanss/jt808-client-go:latest}

FILES=(-f "$PWD/docker-compose.yml")

case "${1:-up}" in
  up) "${DC[@]}" "${FILES[@]}" up -d ;;
  down) "${DC[@]}" "${FILES[@]}" down -v --remove-orphans ;;
  logs) "${DC[@]}" "${FILES[@]}" logs -f --no-color ;;
  ps) "${DC[@]}" "${FILES[@]}" ps ;;
  *)
    echo "usage: $0 [up|down|logs|ps]" >&2
    exit 2
    ;;
esac
