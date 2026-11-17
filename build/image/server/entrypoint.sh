#!/bin/sh
# 单镜像多角色入口：默认 standalone，可通过首个参数或 JT808_ROLE 切换。
set -eu

role="${JT808_ROLE:-standalone}"

if [ "$#" -gt 0 ]; then
  case "$1" in
    -*) ;;
    *)
      role="$1"
      shift
      ;;
  esac
fi

case "$role" in
  standalone|server|jt-server-standalone)
    bin=jt-server-standalone
    default_cfg=/etc/jt808/standalone.yaml
    ;;
  signal|gateway|jt-signal)
    bin=jt-signal
    default_cfg=/etc/jt808/signal.yaml
    ;;
  control|jt-control)
    bin=jt-control
    default_cfg=/etc/jt808/control.yaml
    ;;
  media|jt-media)
    bin=jt-media
    default_cfg=/etc/jt808/media.yaml
    ;;
  attachment|jt-attachment)
    bin=jt-attachment
    default_cfg=/etc/jt808/attachment.yaml
    ;;
  migrate|jt808-migrate)
    bin=jt808-migrate
    default_cfg=/etc/jt808/migrate.yaml
    ;;
  *)
    echo "unknown role: $role" >&2
    echo "supported roles: standalone signal control media attachment migrate" >&2
    exit 64
    ;;
esac

cfg="${JT808_CONFIG:-}"
cfg_explicit=false
if [ -n "$cfg" ]; then
  cfg_explicit=true
else
  cfg="$default_cfg"
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
  elif [ "$role" != "standalone" ] && [ "$role" != "server" ] && [ "$role" != "jt-server-standalone" ]; then
    echo "role '${role}' requires a config file." >&2
    echo "mount it at ${default_cfg}, pass -c <path>, or set JT808_CONFIG." >&2
    exit 64
  fi
fi

exec "$bin" "$@"
