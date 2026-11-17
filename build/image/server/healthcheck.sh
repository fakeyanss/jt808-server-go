#!/bin/sh
# 使用服务自身的 /readyz 就绪接口；角色容器可用 JT808_PROBE_ADDR 覆盖探针地址。
set -eu

addr="${JT808_PROBE_ADDR:-127.0.0.1:9105}"
wget -q -O- "http://${addr}/readyz" >/dev/null
