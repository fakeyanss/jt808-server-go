#!/usr/bin/env bash
# 集群模式终端级冒烟：真实 compose 集群 + 真实 TCP 终端模拟器。
#
# 拓扑：gateway-1、gateway-2、control、media、attachment + MySQL。
# 终端使用 cmd/jt808-client 模拟，覆盖：
#   S1 注册 / 鉴权 / 心跳 / 位置 / 报警
#   S2 0x8300 下行：按目录路由到 gateway-1，终端自动 0x0001 应答，命令转 acked
#   S3 同号终端在 gateway-2 重新鉴权：归属转移到 gateway-2、generation 递增，
#      下行只到 gateway-2 上的终端
#   S4 旧会话断开：携带旧 generation 的 release 被围栏，不能删除新归属
#   S5 再次断连重连回 gateway-1：归属继续转移，下行仍然可达
#
# 依赖：docker（含 compose v2）、go、curl。macOS 3.2 bash 可用。
# 选项 / 环境变量：
#   --keep-logs               保留临时工作目录（含 compose 日志与客户端日志）
#   JT808_SMOKE_IMAGE         冒烟镜像 tag，默认 fakeyanss/jt808-server-go:smoke
#   JT808_SMOKE_PROJECT       compose 项目名，默认 jt808-smoke
#   JT808_SMOKE_WORK_DIR      工作目录（CI 用它固定日志位置）
#   JT808_SMOKE_SKIP_BUILD=1  跳过镜像构建（镜像已存在时）
set -u

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
IMAGE=${JT808_SMOKE_IMAGE:-fakeyanss/jt808-server-go:smoke}
PROJECT=${JT808_SMOKE_PROJECT:-jt808-smoke}
PHONE=13012345678
API_TOKEN=api-token
CONTROL=http://127.0.0.1:8008
GW1_PORT=8080
GW2_PORT=18080
KEEP_LOGS=0
[ "${1:-}" = "--keep-logs" ] && KEEP_LOGS=1

if ! command -v docker >/dev/null 2>&1; then
  echo "smoke-cluster: docker not found in PATH" >&2
  exit 1
fi
if docker compose version >/dev/null 2>&1; then
  COMPOSE_BIN="docker compose"
elif command -v docker-compose >/dev/null 2>&1 && docker-compose version >/dev/null 2>&1; then
  COMPOSE_BIN="docker-compose"
else
  echo "smoke-cluster: docker compose (v2) or docker-compose is required" >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo "smoke-cluster: go toolchain not found in PATH" >&2
  exit 1
fi

if [ -n "${JT808_SMOKE_WORK_DIR:-}" ]; then
  WORK=$JT808_SMOKE_WORK_DIR
  mkdir -p "$WORK"
  KEEP_LOGS=1
else
  # 默认放在仓库的 target/ 下：Docker Desktop 不共享 /tmp，
  # 落在 /tmp 的配置文件挂进容器会变成空目录。
  WORK=$ROOT/target/cluster-smoke
  rm -rf "$WORK"
  mkdir -p "$WORK"
fi
CFG_DIR=$WORK/configs
mkdir -p "$CFG_DIR" "$WORK/bin"

PASS=0
FAIL=0
CLIENT_PIDS=""

ok() { echo "PASS: $*"; PASS=$((PASS + 1)); }
bad() { echo "FAIL: $*"; FAIL=$((FAIL + 1)); }

compose() {
  $COMPOSE_BIN --project-name "$PROJECT" \
    -f "$ROOT/deploy/docker-compose.cluster.yml" \
    -f "$ROOT/deploy/docker-compose.cluster-smoke.yml" "$@"
}
export JT808_SMOKE_IMAGE=$IMAGE
export JT808_SMOKE_CONFIG_DIR=$CFG_DIR

cleanup() {
  for pid in $CLIENT_PIDS; do
    kill "$pid" 2>/dev/null
  done
  compose logs --no-color --no-log-prefix >"$WORK/compose-logs.txt" 2>&1
  compose ps >"$WORK/compose-ps.txt" 2>&1
  compose down -v --remove-orphans >/dev/null 2>&1
  if [ "$KEEP_LOGS" -eq 1 ]; then
    echo "smoke-cluster: logs kept in $WORK"
  else
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT

port_in_use() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && { exec 3<&-; exec 3>&-; return 0; }
  return 1
}
for p in 8008 "$GW1_PORT" "$GW2_PORT" 8081; do
  if port_in_use "$p"; then
    echo "smoke-cluster: port $p is already in use" >&2
    exit 1
  fi
done

# ---------------------------------------------------------------- helpers
mysql_q() { # sql
  compose exec -T mysql mysql -N -B -ujt808 -pjt808 jt808 -e "$1" 2>/dev/null | tr -d '\r'
}

api_get() { # path
  curl -sS -H "Authorization: Bearer $API_TOKEN" "$CONTROL$1"
}

wait_api() { # timeout-seconds
  local i
  for i in $(seq 1 "$1"); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $API_TOKEN" "$CONTROL/api/device")" = "200" ]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

directory_row() {
  mysql_q "select concat(gateway_id,'/',generation) from session_directory where phone='$PHONE';" | head -1
}

# 协议头手机号是 BCD[10]，服务端会把 11 位号码左侧补零成 20 位。
# API 与目录查询都必须用服务端规范化后的号码，这里从目录反查。
resolve_phone() {
  local phone
  phone=$(mysql_q "select phone from session_directory limit 1;" | head -1)
  [ -z "$phone" ] && phone=$(mysql_q "select phone from devices limit 1;" | head -1)
  printf '%s' "$phone"
}

wait_phone() {
  local i phone
  for i in $(seq 1 60); do
    phone=$(resolve_phone)
    if [ -n "$phone" ]; then
      printf '%s' "$phone"
      return 0
    fi
    sleep 1
  done
  return 1
}

# 等待目录归属出现在指定网关上，返回完整 "gateway/generation"。
wait_gateway() { # gateway
  local i row
  for i in $(seq 1 60); do
    row=$(directory_row)
    case "$row" in
      "$1"/*)
        printf '%s' "$row"
        return 0
        ;;
    esac
    sleep 1
  done
  echo "smoke-cluster: expected $1/*, got '$(directory_row)'" >&2
  return 1
}

issue_text() { # marker
  curl -sS -X POST -H "Authorization: Bearer $API_TOKEN" -H 'Content-Type: application/json' \
    --data "{\"flag\":1,\"textType\":1,\"text\":\"$1\"}" \
    "$CONTROL/api/device/$PHONE/text/issue"
}

command_id() { sed -nE 's/.*"commandId":"([^"]+)".*/\1/p'; }
command_status() {
  api_get "/api/commands/$1" | sed -nE 's/.*"status":"([^"]+)".*/\1/p'
}
wait_acked() { # commandId
  local i
  for i in $(seq 1 40); do
    [ "$(command_status "$1")" = "acked" ] && return 0
    sleep 0.5
  done
  echo "smoke-cluster: command $1 status=$(command_status "$1")" >&2
  return 1
}

make_client_config() { # name port
  # 同一台终端重连时要保持设备身份不变：设备 ID 固定，
  # 否则服务端会按新档案重算鉴权码，重连鉴权必然失败。
  sed -E \
    -e "s|(remoteAddr: \")[^\"]*|\1127.0.0.1:$2|" \
    -e 's|^    idReg: .*|    idReg: "1234567"|' \
    -e 's|(keepalive: )[0-9]+|\15|' \
    -e 's|(locationReportInterval: )[0-9]+|\13|' \
    -e "s|(logFile: \")[^\"]*|\1$1.log|" \
    "$ROOT/client/configs/default.yaml" >"$WORK/client-$1.yaml"
}

start_client() { # name port
  local name=$1
  local port=$2
  local dir="$WORK/client-$name"
  mkdir -p "$dir"
  make_client_config "$name" "$port"
  ( cd "$dir" && exec "$WORK/bin/client" -c "$WORK/client-$name.yaml" ) >"$dir/console.out" 2>&1 &
  local pid=$!
  echo "$pid" >"$dir/pid"
  CLIENT_PIDS="$CLIENT_PIDS $pid"
  echo "smoke-cluster: client $name started (pid=$pid, addr=127.0.0.1:$port)"
}

stop_client() { # name
  local dir="$WORK/client-$1"
  local pid
  [ -f "$dir/pid" ] || return 0
  pid=$(cat "$dir/pid")
  kill "$pid" 2>/dev/null
  local i
  for i in $(seq 1 30); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  kill -9 "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
}

client_seen() { # name pattern
  grep -q "$2" "$WORK/client-$1/console.out" 2>/dev/null
}

wait_client_seen() { # name pattern seconds
  local i
  for i in $(seq 1 "$3"); do
    client_seen "$1" "$2" && return 0
    sleep 1
  done
  return 1
}

gateway_log() { # service
  compose logs --no-color "$1" 2>/dev/null
}

gateway_count() { # service msgid
  gateway_log "$1" | grep -cE "RawMsgID=\"?$2\"?"
}

# ---------------------------------------------------------------- setup
echo "smoke-cluster: work dir $WORK"
sed -E \
  -e 's|logLevel: "INFO"|logLevel: "DEBUG"|' \
  -e 's|logFile: "[^"]*"|logFile: "jt-signal-1.log"|' \
  "$ROOT/deploy/docker/cluster/signal.yaml" >"$CFG_DIR/signal-1.yaml"

sed -E \
  -e 's|nodeId: "gateway-1"|nodeId: "gateway-2"|' \
  -e 's|advertiseUrl: "[^"]*"|advertiseUrl: "http://jt-signal-2:9101"|' \
  -e 's|addr: "0.0.0.0:9106"|addr: "0.0.0.0:9116"|' \
  -e 's|logLevel: "INFO"|logLevel: "DEBUG"|' \
  -e 's|logFile: "[^"]*"|logFile: "jt-signal-2.log"|' \
  "$ROOT/deploy/docker/cluster/signal.yaml" >"$CFG_DIR/signal-2.yaml"

if [ "${JT808_SMOKE_SKIP_BUILD:-0}" != "1" ]; then
  echo "smoke-cluster: building image $IMAGE"
  if ! docker build -f "$ROOT/build/Dockerfile" -t "$IMAGE" "$ROOT" >"$WORK/docker-build.log" 2>&1; then
    echo "smoke-cluster: image build failed, tail of $WORK/docker-build.log:" >&2
    tail -n 40 "$WORK/docker-build.log" >&2
    exit 1
  fi
fi

echo "smoke-cluster: building client simulator"
( cd "$ROOT" && go build -o "$WORK/bin/client" ./cmd/jt808-client ) || {
  echo "smoke-cluster: client build failed" >&2
  exit 1
}

echo "smoke-cluster: starting cluster"
compose up -d >"$WORK/compose-up.log" 2>&1 || {
  echo "smoke-cluster: compose up failed" >&2
  tail -n 40 "$WORK/compose-up.log" >&2
  exit 1
}

if ! wait_api 240; then
  echo "smoke-cluster: control API did not become ready" >&2
  compose ps >&2
  exit 1
fi
ok "control API ready on $CONTROL"

for i in $(seq 1 30); do
  [ -n "$(mysql_q 'select 1')" ] && break
  sleep 1
done

nodes=$(mysql_q "select concat(gateway_id,':',status) from gateway_nodes order by gateway_id;" | tr '\n' ' ')
for _ in $(seq 1 30); do
  printf '%s' "$nodes" | grep -q 'gateway-1:online' && printf '%s' "$nodes" | grep -q 'gateway-2:online' && break
  sleep 1
  nodes=$(mysql_q "select concat(gateway_id,':',status) from gateway_nodes order by gateway_id;" | tr '\n' ' ')
done
if printf '%s' "$nodes" | grep -q 'gateway-1:online' && printf '%s' "$nodes" | grep -q 'gateway-2:online'; then
  ok "both gateways registered: $nodes"
else
  bad "gateway_nodes expected gateway-1 and gateway-2 online, got: $nodes"
fi

GEN1=0
# ---------------------------------------------------------------- S1 terminal on gateway-1
start_client a "$GW1_PORT"
resolved=$(wait_phone) || resolved=""
if [ -n "$resolved" ]; then
  PHONE=$resolved
  ok "S1 device registered on control (phone=$PHONE)"
  row=$(wait_gateway gateway-1) || row=""
  if [ -n "$row" ]; then
    GEN1=${row##*/}
    ok "S1 0x0102 claim: $row"
  else
    bad "S1 0x0102 claim did not land on gateway-1"
  fi
  if [ -n "$(mysql_q "select auth_code from devices where phone='$PHONE';" | head -1)" ]; then
    ok "S1 auth code persisted for reconnect"
  else
    bad "S1 no auth code persisted; reconnect will fail"
  fi
else
  bad "S1 no device/session row appeared after registration"
fi

if wait_client_seen a '0x8100' 20; then
  ok "S1 terminal registered (0x0100) and received 0x8100"
else
  bad "S1 terminal did not receive registration reply 0x8100"
fi
if [ "$(gateway_count jt-signal 0x0102)" -ge 1 ]; then
  ok "S1 gateway-1 processed authentication 0x0102"
else
  bad "S1 gateway-1 saw no 0x0102"
fi
if wait_client_seen a '0x0002' 30 || [ "$(gateway_count jt-signal 0x0002)" -ge 1 ]; then
  ok "S1 heartbeat 0x0002 reached gateway-1"
else
  bad "S1 no heartbeat 0x0002 within 30s"
fi

geo_ok=0
for _ in $(seq 1 30); do
  if api_get "/api/device/$PHONE/geo" | grep -q '"gis"'; then
    geo_ok=1
    break
  fi
  sleep 1
done
if [ "$geo_ok" = "1" ]; then
  ok "S1 location 0x0200 persisted (GET /api/device/$PHONE/geo has gis)"
else
  bad "S1 location 0x0200 not persisted"
fi

alarm_seen=0
for _ in $(seq 1 30); do
  if api_get "/api/device/$PHONE/alarms" | grep -q 'alarmSign'; then
    alarm_seen=1
    break
  fi
  sleep 1
done
if [ "$alarm_seen" = "1" ]; then
  ok "S1 alarm recorded from 0x0200 alarm sign"
else
  bad "S1 no alarm record from 0x0200 alarm sign"
fi

# ---------------------------------------------------------------- S2 downlink to gateway-1
marker1="cluster-smoke-gw1-$$"
resp1=$(issue_text "$marker1")
cmd1=$(printf '%s' "$resp1" | command_id)
if [ -n "$cmd1" ]; then
  if wait_acked "$cmd1"; then
    ok "S2 downlink 0x8300 accepted and acked (command=$cmd1)"
  else
    bad "S2 downlink 0x8300 not acked: $resp1"
  fi
else
  bad "S2 downlink 0x8300 rejected: $resp1"
fi
if wait_client_seen a "$marker1" 15; then
  ok "S2 terminal on gateway-1 received 0x8300 ($marker1)"
else
  bad "S2 terminal on gateway-1 did not receive 0x8300"
fi
if [ "$(gateway_count jt-signal 0x0001)" -ge 1 ]; then
  ok "S2 gateway-1 received terminal reply 0x0001"
else
  bad "S2 gateway-1 saw no terminal reply 0x0001"
fi

# ---------------------------------------------------------------- S3 ownership moves to gateway-2
start_client b "$GW2_PORT"
row=$(wait_gateway gateway-2) || row=""
GEN2=${row##*/}
if [ -n "$row" ] && [ "${GEN2:-0}" -gt "${GEN1:-0}" ]; then
  ok "S3 same phone re-authenticated on gateway-2: $row (was ${GEN1})"
else
  bad "S3 ownership did not move to gateway-2 with a newer generation (got '$row')"
fi

marker2="cluster-smoke-gw2-$$"
resp2=$(issue_text "$marker2")
cmd2=$(printf '%s' "$resp2" | command_id)
if [ -n "$cmd2" ] && wait_acked "$cmd2"; then
  ok "S3 downlink after takeover acked (command=$cmd2)"
else
  bad "S3 downlink after takeover not acked: $resp2"
fi
if wait_client_seen b "$marker2" 15; then
  ok "S3 terminal on gateway-2 received 0x8300"
else
  bad "S3 terminal on gateway-2 did not receive 0x8300"
fi
if client_seen a "$marker2"; then
  bad "S3 superseded terminal on gateway-1 still received 0x8300"
else
  ok "S3 gateway-1 terminal no longer receives downlink (directory routing works)"
fi
if [ "$(gateway_count jt-signal-2 0x0001)" -ge 1 ]; then
  ok "S3 gateway-2 received terminal reply 0x0001"
else
  bad "S3 gateway-2 saw no terminal reply 0x0001"
fi

# ---------------------------------------------------------------- S4 stale release is fenced
stop_client a
sleep 3
row=$(directory_row)
if [ "$row" = "gateway-2/$GEN2" ]; then
  ok "S4 stale release from gateway-1 did not delete gateway-2 ownership ($row)"
else
  bad "S4 stale release changed ownership to '$row' (expected gateway-2/$GEN2)"
fi

# ---------------------------------------------------------------- S5 reconnect back to gateway-1
stop_client b
for _ in $(seq 1 20); do
  [ -z "$(directory_row)" ] && break
  sleep 0.5
done
start_client c "$GW1_PORT"
row=$(wait_gateway gateway-1) || row=""
if [ -n "$row" ]; then
  ok "S5 reconnect moved ownership back to gateway-1 ($row)"
else
  bad "S5 reconnect did not re-claim gateway-1"
fi

marker3="cluster-smoke-gw1-back-$$"
resp3=$(issue_text "$marker3")
cmd3=$(printf '%s' "$resp3" | command_id)
if [ -n "$cmd3" ] && wait_acked "$cmd3"; then
  ok "S5 downlink after reconnect acked (command=$cmd3)"
else
  bad "S5 downlink after reconnect not acked: $resp3"
fi
if wait_client_seen c "$marker3" 15; then
  ok "S5 terminal after reconnect received 0x8300"
else
  bad "S5 terminal after reconnect did not receive 0x8300"
fi

# gateway-1 的 0x0001 计数应随两次下发增长
if [ "$(gateway_count jt-signal 0x0001)" -ge 2 ]; then
  ok "S5 gateway-1 handled both downlinks (0x0001 count >= 2)"
else
  bad "S5 gateway-1 reply count did not grow as expected"
fi

echo
echo "================ CLUSTER SMOKE SUMMARY: PASS=$PASS FAIL=$FAIL ================"
exit "$FAIL"
