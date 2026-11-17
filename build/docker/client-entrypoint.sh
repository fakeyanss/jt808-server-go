#!/bin/sh
# 模拟终端镜像入口：默认读取镜像内配置，命令行参数原样透传。
set -eu

cfg="${JT808_CONFIG:-/etc/jt808/client.yaml}"
cfg_explicit=false
if [ -n "${JT808_CONFIG:-}" ]; then
  cfg_explicit=true
fi

has_cfg=false
for arg in "$@"; do
  case "$arg" in
    -c|--config|-c=*|--config=*) has_cfg=true ;;
  esac
done

if [ "$has_cfg" = false ]; then
  if [ -f "$cfg" ]; then
    set -- -c "$cfg" "$@"
  elif [ "$cfg_explicit" = true ]; then
    echo "config file not found: ${cfg}" >&2
    exit 64
  fi
fi

exec jt808-client-go "$@"
