#!/usr/bin/env bash
# 本地压测基线：启动真实 server 与 N 个并发模拟终端，采集管理面指标。
#
# 口径：
#   - 存储用 memory 后端，只压协议接入与业务处理路径，不把 SQLite 写入算进基线；
#   - 每个模拟终端每秒上报一次 0x0200，另按 keepalive 周期发送 0x0002；
#   - 结束时从 /metrics 取连接数、待应答数、应答超时与 outbox 指标。
#
# 用法：bash scripts/loadtest.sh [持续秒数]，默认 60 秒。
# 可用环境变量：LOAD_CONCURRENCY(默认 200) / LOAD_TCP_PORT / LOAD_HTTP_PORT / LOAD_ADMIN_PORT。
set -u

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
DURATION=${1:-60}
CONCURRENCY=${LOAD_CONCURRENCY:-200}
TCP_PORT=${LOAD_TCP_PORT:-18080}
UDP_PORT=$((TCP_PORT + 1))
HTTP_PORT=${LOAD_HTTP_PORT:-18008}
ADMIN_PORT=${LOAD_ADMIN_PORT:-19000}
TOKEN="loadtest"

if ! command -v go >/dev/null 2>&1; then
  echo "loadtest: go toolchain not found in PATH" >&2
  exit 1
fi

WORK=$(mktemp -d -t jt808-load.XXXXXX)
mkdir -p "$WORK/bin" "$WORK/run"

cleanup() {
  [ -n "${CLIENT_PID:-}" ] && kill -9 "$CLIENT_PID" 2>/dev/null
  [ -n "${SERVER_PID:-}" ] && kill -9 "$SERVER_PID" 2>/dev/null
  if [ "${KEEP_WORK:-0}" = "1" ]; then
    echo "loadtest: work dir kept at $WORK"
  else
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT

sed -E \
  -e "s|(tcpPort: \")[0-9]+|\\1$TCP_PORT|" \
  -e "s|(udpPort: \")8081|\\1$UDP_PORT|" \
  -e "s|(httpPort: \")[0-9]+|\\1$HTTP_PORT|" \
  -e "s|(logLevel: \")DEBUG|\\1INFO|" \
  -e 's|(alarmServer:.*)|&|' \
  -e 's|(port: )"7200"|\1""|' \
  -e 's|(udpPort: )"7201"|\1""|' \
  "$ROOT/configs/default.yaml" >"$WORK/run/server.yaml"

# admin 默认关闭；压测需要指标，这里打开并绑定本机端口。
python3 - "$WORK/run/server.yaml" "$ADMIN_PORT" "$TOKEN" <<'PY'
import re, sys
path, port, token = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path).read()
text = re.sub(r'(admin:\n  enable: )false', r'\1true', text)
text = re.sub(r'(admin:\n  enable: true\n  addr: )"[^"]*"', lambda m: m.group(1) + '"127.0.0.1:' + port + '"', text)
text = re.sub(r'(admin:\n  enable: true\n  addr: "127.0.0.1:%s"\n  token: )""' % port, lambda m: m.group(1) + '"' + token + '"', text)
open(path, 'w').write(text)
PY

sed -E \
  -e "s|(remoteAddr: \"localhost:)[0-9]+|\\1$TCP_PORT|" \
  -e "s|concurrency: [0-9]+|concurrency: $CONCURRENCY|" \
  -e 's|(phoneReg: ).*|\1"130[0-9]{8}"|' \
  -e 's|(plateReg: ).*|\1"京A[A-Z0-9]{5}"|' \
  -e "s|locationReportInterval: [0-9]+|locationReportInterval: 1|" \
  -e "s|(consoleEnable: )true|\\1false|" \
  -e "s|(fileEnable: )true|\\1false|" \
  -e "s|(logLevel: \")DEBUG|\\1INFO|" \
  "$ROOT/client/configs/default.yaml" >"$WORK/run/client.yaml"

echo "loadtest: building server and client..."
( cd "$ROOT" && go build -o "$WORK/bin/server" ./cmd/jt808-server && go build -o "$WORK/bin/client" ./cmd/jt808-client ) || {
  echo "loadtest: build failed" >&2
  exit 1
}

( cd "$WORK/run" && exec "$WORK/bin/server" -c "$WORK/run/server.yaml" ) >"$WORK/run/server.out" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 60); do
  if nc -z 127.0.0.1 "$TCP_PORT" >/dev/null 2>&1; then break; fi
  kill -0 "$SERVER_PID" 2>/dev/null || { echo "loadtest: server exited early" >&2; cat "$WORK/run/server.out" >&2; exit 1; }
  sleep 0.2
done

echo "loadtest: $CONCURRENCY terminals for ${DURATION}s against 127.0.0.1:$TCP_PORT"
( cd "$WORK/run" && exec "$WORK/bin/client" -c "$WORK/run/client.yaml" ) >"$WORK/run/client.out" 2>&1 &
CLIENT_PID=$!

sleep "$DURATION"

# 沙箱内 ps 可能读不到子进程 rss，统一用 top 采样；取第二个样本以获得真实 CPU 差值。
TOP_LINE=$(top -l 2 -s 1 -pid "$SERVER_PID" -stats pid,mem,cpu 2>/dev/null | awk 'NF>=3 && $1 ~ /^[0-9]+$/ {line=$0} END{print line}')
RSS_MB=$(awk '{v=$2; if (v ~ /G$/) {sub("G","",v); v=v*1024} else if (v ~ /M$/) {sub("M","",v)} else if (v ~ /K$/) {sub("K","",v); v=v/1024} print int(v+0)}' <<<"$TOP_LINE")
CPU=$(awk '{print $3+0}' <<<"$TOP_LINE")
METRICS=$(curl -fsS -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$ADMIN_PORT/metrics" 2>/dev/null || true)
READY=$(curl -fsS "http://127.0.0.1:$ADMIN_PORT/readyz" 2>/dev/null || true)

kill "$CLIENT_PID" 2>/dev/null
wait "$CLIENT_PID" 2>/dev/null

echo
echo "================ LOAD SUMMARY ================"
echo "duration_s=$DURATION concurrency=$CONCURRENCY server_rss_mb=$RSS_MB server_cpu_pct=$CPU"
printf '%s\n' "$METRICS" | grep -E '^jt808_' || true
echo "readyz=$READY"
ERRORS=$(grep -c '| ERROR |' "$WORK/run/server.out" 2>/dev/null || true)
echo "server_error_lines=$ERRORS"
echo "=============================================="
