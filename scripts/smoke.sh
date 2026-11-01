#!/bin/bash
# End-to-end smoke test for the JT808 gateway.
#
# Builds the real server and the client simulator, then drives the simulator
# over real TCP connections across five scenarios:
#   S1 register -> authenticate -> logout (clean exit)
#   S2 full uplink sweep (0x0002/0x0200/0x0004 and every supported industry
#      report: 0301/0302/0303/0108/0701/0704/0900/0901/0702/0705/0700/0800/0801)
#   S3 resident terminal: periodic heartbeat and location report
#   S4 fragmented registration: ordered / out-of-order / dropped fragment
#      recovered via a server-initiated 0x8003 retransmission request
#   S5 malformed frames (bad-checksum / bad-escape / oversize) are rejected
#      without crashing the server, and the next valid frame still registers
#
# Every scenario starts a fresh server so in-memory device registrations never
# leak between runs. Exit code is the number of failed assertions (0 = pass).
#
# Requirements: bash 3.2+ (macOS default), go in PATH. Duration ~60s.
# Options / environment:
#   --keep-logs            keep the temporary work directory with full logs
#   SMOKE_TCP_PORT (8080)  server TCP / client target port
#   SMOKE_UDP_PORT (8081)  server UDP port
#   SMOKE_HTTP_PORT(8008)  server HTTP API port
set -u

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
TCP_PORT=${SMOKE_TCP_PORT:-8080}
UDP_PORT=${SMOKE_UDP_PORT:-8081}
HTTP_PORT=${SMOKE_HTTP_PORT:-8008}
KEEP_LOGS=0
[ "${1:-}" = "--keep-logs" ] && KEEP_LOGS=1

if ! command -v go >/dev/null 2>&1; then
  echo "smoke: go toolchain not found in PATH" >&2
  exit 1
fi

WORK=$(mktemp -d -t jt808-smoke.XXXXXX)
mkdir -p "$WORK/bin"

# Port must be free, otherwise the server fails to listen and the client
# would burn 60s dialing before giving up.
port_in_use() { # port
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && { exec 3<&-; exec 3>&-; return 0; }
  return 1
}
for p in "$TCP_PORT" "$UDP_PORT" "$HTTP_PORT"; do
  if port_in_use "$p"; then
    echo "smoke: port $p is already in use; set SMOKE_TCP_PORT/SMOKE_UDP_PORT/SMOKE_HTTP_PORT" >&2
    exit 1
  fi
done

# Generate per-run configs from the checked-in defaults with ports rewritten.
sed -E \
  -e "s|(tcpPort: \")[0-9]+|\\1$TCP_PORT|" \
  -e "s|(udpPort: \")[0-9]+|\\1$UDP_PORT|" \
  -e "s|(httpPort: \")[0-9]+|\\1$HTTP_PORT|" \
  "$ROOT/configs/default.yaml" >"$WORK/server.yaml"
sed -E "s|(remoteAddr: \"localhost:)[0-9]+|\\1$TCP_PORT|" \
  "$ROOT/client/configs/default.yaml" >"$WORK/client.yaml"

SBIN="$WORK/bin/server"
CBIN="$WORK/bin/client"
echo "smoke: building server and client..."
( cd "$ROOT" && go build -o "$SBIN" ./cmd/jt808-server && go build -o "$CBIN" ./cmd/jt808-client ) || {
  echo "smoke: build failed" >&2
  exit 1
}

PASS=0
FAIL=0
SERVER_PIDS=()

start_server() { # name
  local name=$1
  local dir=$WORK/$name
  mkdir -p "$dir"
  # cwd is the scenario dir, so the server's ./logs/ never touches the repo.
  ( cd "$dir" && exec "$SBIN" -c "$WORK/server.yaml" ) >"$dir/server.out" 2>&1 &
  local pid=$!
  echo "$pid" >"$dir/server.pid"
  SERVER_PIDS+=("$pid")
  for _ in $(seq 1 60); do
    grep -q "Listening on :$TCP_PORT" "$dir/server.out" 2>/dev/null && {
      echo "[$name] server started pid=$pid"
      return 0
    }
    grep -q 'Fail to listen' "$dir/server.out" 2>/dev/null && break
    sleep 0.2
  done
  echo "[$name] FAIL: server did not listen on $TCP_PORT" >&2
  cat "$dir/server.out" >&2
  exit 1
}

stop_server() { # name
  local name=$1
  local dir=$WORK/$name
  local pid
  pid=$(cat "$dir/server.pid")
  kill "$pid" 2>/dev/null
  for _ in $(seq 1 30); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  kill -9 "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
}

ok()  { echo "[$1] PASS: $2"; PASS=$((PASS+1)); }
bad() { echo "[$1] FAIL: $2"; FAIL=$((FAIL+1)); }

run_client_exit() { # name secs client-args... ; expects exit 0 within secs
  local name=$1 secs=$2; shift 2
  local dir=$WORK/$name
  mkdir -p "$dir"
  local out=$dir/client-exit.out
  ( cd "$dir" && exec "$CBIN" -c "$WORK/client.yaml" "$@" ) >"$out" 2>&1 &
  local p=$!
  ( sleep "$secs"; kill -9 "$p" 2>/dev/null ) &
  local w=$!
  wait "$p"; local rc=$?
  kill "$w" 2>/dev/null; wait "$w" 2>/dev/null
  if [ "$rc" -eq 0 ]; then
    ok "$name" "client exited 0 (args: $*)"
  elif [ "$rc" -eq 137 ]; then
    bad "$name" "client timed out after ${secs}s, expected clean exit (args: $*)"
  else
    bad "$name" "client exited $rc (args: $*)"
  fi
}

run_client_resident() { # name secs client-args... ; expects to stay alive
  local name=$1 secs=$2; shift 2
  local dir=$WORK/$name
  mkdir -p "$dir"
  ( cd "$dir" && exec "$CBIN" -c "$WORK/client.yaml" "$@" ) >"$dir/client-resident.out" 2>&1 &
  local p=$!
  sleep "$secs"
  if kill -0 "$p" 2>/dev/null; then
    kill "$p" 2>/dev/null; sleep 0.5; kill -9 "$p" 2>/dev/null
    wait "$p" 2>/dev/null
    ok "$name" "client alive for ${secs}s as expected, then killed (args: $*)"
  else
    wait "$p"; local rc=$?
    bad "$name" "client exited early rc=$rc after <${secs}s (args: $*)"
  fi
}

received_ids() { # name
  local name=$1
  grep 'Received jt808 msg' "$WORK/$name/server.out" 2>/dev/null \
    | grep -oE '0x[0-9a-f]{4}' | sort -u | tr '\n' ' '
  echo
}

expect_ids() { # name ids...
  local name=$1; shift
  local missing=()
  local got
  got=$(received_ids "$name")
  for id in "$@"; do
    echo "$got" | grep -q "$id" || missing+=("$id")
  done
  if [ ${#missing[@]} -eq 0 ]; then
    ok "$name" "server processed all expected uplink ids: $*"
  else
    bad "$name" "missing uplink ids: ${missing[*]}; got: $got"
  fi
}

# "Fail to read stream ..." lines are transport-level peer closures
# (EOF / connection reset / closed pipe), not protocol defects.
scan_unexpected_errors() { # name [extra-marker-to-ignore]
  local name=$1
  local marker=${2:-}
  local extra
  extra=$(grep -E '\| ERROR \|' "$WORK/$name/server.out" 2>/dev/null \
    | grep -v 'Fail to read stream to framePayload:' || true)
  if [ -n "$marker" ]; then
    extra=$(printf '%s\n' "$extra" | grep -v "$marker" || true)
  fi
  if [ -z "$extra" ]; then
    ok "$name" "no unexpected ERROR in server log"
  else
    bad "$name" "unexpected ERROR in server log:"
    echo "$extra"
  fi
}

expect_server_alive() { # name
  local pid
  pid=$(cat "$WORK/$1/server.pid")
  if kill -0 "$pid" 2>/dev/null; then
    ok "$1" "server process alive"
  else
    bad "$1" "server process died"
  fi
}

cleanup() {
  if [ "${#SERVER_PIDS[@]}" -gt 0 ]; then
    for pid in "${SERVER_PIDS[@]}"; do
      kill -9 "$pid" 2>/dev/null
    done
  fi
  if [ "$KEEP_LOGS" -eq 1 ]; then
    echo "smoke: logs kept in $WORK"
  else
    rm -rf "$WORK"
  fi
}
trap cleanup EXIT

# ---------- S1: register / authenticate / logout ---------------------------
start_server s1-logout
run_client_exit s1-logout 20 -logout
sleep 0.5
expect_ids s1-logout 0x0100 0x0102 0x0003
scan_unexpected_errors s1-logout
stop_server s1-logout

# ---------- S2: full industry uplink sweep while resident ------------------
start_server s2-industry
run_client_resident s2-industry 12 -industry-reports
expect_ids s2-industry 0x0100 0x0102 0x0002 0x0200 0x0004 \
  0x0301 0x0302 0x0303 0x0108 0x0701 0x0704 0x0900 0x0901 0x0702 0x0705 0x0700 0x0800 0x0801
scan_unexpected_errors s2-industry
stop_server s2-industry

# ---------- S3: bare resident terminal: heartbeat + location ---------------
start_server s3-bare
run_client_resident s3-bare 12
expect_ids s3-bare 0x0100 0x0102 0x0002 0x0200
scan_unexpected_errors s3-bare
stop_server s3-bare

# ---------- S4: fragmented registration, ending with clean logout ----------
start_server s4-frag
run_client_exit s4-frag 20 -fragment-size 100 -logout
sleep 0.5
run_client_exit s4-frag 20 -fragment-size 100 -reverse-fragments -logout
sleep 0.5
# 0100 body is 84B: size 40 -> 3 fragments; dropping #2 forces the server's
# 300ms idle timer to issue 0x8003, after which the client replays fragment 2.
run_client_exit s4-frag 20 -fragment-size 40 -drop-fragment 2 -logout
sleep 0.5
expect_ids s4-frag 0x0100 0x0102 0x0003
if grep -lq 'RawMsgID=0x8003' "$WORK"/s4-frag/client-*.out 2>/dev/null; then
  ok s4-frag "client received 0x8003 retransmission request and recovered"
else
  bad s4-frag "no client-side 0x8003 retransmission request observed"
fi
if grep -q 'attempt limit reached' "$WORK/s4-frag/server.out"; then
  bad s4-frag "fragment retransmission gave up (attempt limit)"
else
  ok s4-frag "retransmission succeeded within attempt limit"
fi
scan_unexpected_errors s4-frag
stop_server s4-frag

# ---------- S5: malformed frame rejected, next valid frame survives ---------
for spec in "bad-checksum|Verify failed" "bad-escape|Malformed JT808 frame" "oversize|Frame exceeds maximum length"; do
  fault=${spec%%|*}
  marker=${spec#*|}
  name="s5-$fault"
  start_server "$name"
  run_client_resident "$name" 6 -fault-frame "$fault"
  expect_server_alive "$name"
  if grep -q "$marker" "$WORK/$name/server.out"; then
    ok "$name" "server rejected $fault ($marker)"
  else
    bad "$name" "rejection marker not found for $fault: $marker"
  fi
  # Recovery proof: the follow-up 0100/0102 are accepted and the device reaches
  # Online (0x0002 heartbeat is only sent after Online).
  expect_ids "$name" 0x0100 0x0102 0x0002
  scan_unexpected_errors "$name" "$marker"
  stop_server "$name"
done

echo
echo "================ SMOKE SUMMARY: PASS=$PASS FAIL=$FAIL ================"
exit "$FAIL"
