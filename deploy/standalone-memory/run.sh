#!/usr/bin/env bash
# memory 后端 standalone 的一键启动脚本，等价于原 deploy/docker-compose.yml。
#
# 启动：deploy/standalone-memory/run.sh
# 停止（并清理卷）：
#   docker rm -f "${NAME:-jt808}"
#   docker volume rm "${DATA_VOLUME:-jt808-standalone-data}" "${LOG_VOLUME:-jt808-standalone-logs}"
#
# 可用环境变量覆盖默认值：
#   IMAGE            镜像，默认 fakeyanss/jt808-server-go:latest
#   NAME             容器名，默认 jt808
#   TCP_PORT         808 终端 TCP，默认 8080
#   UDP_PORT         808 终端 UDP，默认 8081
#   HTTP_PORT        业务 API，默认 8008
#   ALARM_TCP_PORT   苏标附件 TCP，默认 7200
#   ALARM_UDP_PORT   苏标附件 UDP，默认 7201
#   DATA_VOLUME      数据卷，默认 jt808-standalone-data
#   LOG_VOLUME       日志卷，默认 jt808-standalone-logs
#   TZ               时区，默认 Asia/Shanghai
set -euo pipefail

IMAGE=${IMAGE:-fakeyanss/jt808-server-go:latest}
NAME=${NAME:-jt808}
TCP_PORT=${TCP_PORT:-8080}
UDP_PORT=${UDP_PORT:-8081}
HTTP_PORT=${HTTP_PORT:-8008}
ALARM_TCP_PORT=${ALARM_TCP_PORT:-7200}
ALARM_UDP_PORT=${ALARM_UDP_PORT:-7201}
DATA_VOLUME=${DATA_VOLUME:-jt808-standalone-data}
LOG_VOLUME=${LOG_VOLUME:-jt808-standalone-logs}
TZ=${TZ:-Asia/Shanghai}

docker rm -f "$NAME" >/dev/null 2>&1 || true

exec docker run -d \
  --name "$NAME" \
  --restart unless-stopped \
  --security-opt no-new-privileges:true \
  -e TZ="$TZ" \
  -e JT808_ROLE=standalone \
  -p "${TCP_PORT}:8080/tcp" \
  -p "${UDP_PORT}:8081/udp" \
  -p "${HTTP_PORT}:8008/tcp" \
  -p "${ALARM_TCP_PORT}:7200/tcp" \
  -p "${ALARM_UDP_PORT}:7201/udp" \
  -v "${DATA_VOLUME}:/app/data" \
  -v "${LOG_VOLUME}:/app/logs" \
  "$IMAGE"
