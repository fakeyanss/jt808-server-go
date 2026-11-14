# syntax=docker/dockerfile:1

# Build stage：静态编译全部角色二进制与迁移工具。
# SQLite 使用纯 Go 驱动（modernc.org/sqlite），可以关闭 cgo，产物可直接放进最小运行镜像。
FROM golang:1.26.10-alpine AS build

ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

ENV CGO_ENABLED=0 \
    GOPROXY=${GOPROXY} \
    GOTOOLCHAIN=local

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p /out && \
    for cmd in jt-server-standalone jt-signal jt-control jt-media jt-attachment jt808-migrate; do \
      go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X main.buildTime=${BUILD_TIME} -X main.buildCommit=${COMMIT}" \
        -o "/out/${cmd}" "./cmd/${cmd}"; \
    done

# Runtime stage：Alpine 提供 busybox wget 用于健康检查，额外补 CA 证书与时区数据。
FROM alpine:3.22

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.title="jt808-server-go" \
      org.opencontainers.image.description="JT/T 808 / JT/T 1078 / 苏标主动安全终端接入服务" \
      org.opencontainers.image.source="https://github.com/fakeyanss/jt808-server-go" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S -g 10001 jt808 && \
    adduser -S -D -H -u 10001 -G jt808 jt808 && \
    mkdir -p /app/data/alarms /app/data/media /app/data/media-uploads /app/logs /etc/jt808 && \
    chown -R jt808:jt808 /app /etc/jt808

WORKDIR /app

COPY --from=build /out/ /usr/local/bin/
COPY --chown=jt808:jt808 build/docker/standalone.yaml /etc/jt808/standalone.yaml
COPY --chown=jt808:jt808 build/docker/entrypoint.sh /usr/local/bin/jt808-entrypoint
COPY --chown=jt808:jt808 build/docker/healthcheck.sh /usr/local/bin/jt808-healthcheck
RUN chmod 0755 /usr/local/bin/jt808-entrypoint /usr/local/bin/jt808-healthcheck

# 运行时配置与业务数据全部通过 volume 挂载；镜像内只带 standalone 默认配置。
VOLUME ["/app/data", "/app/logs"]

ENV TZ=Asia/Shanghai \
    JT808_ROLE=standalone \
    JT808_PROBE_ADDR=127.0.0.1:9105

# standalone 默认监听端口；角色容器的端口见 docs/installation.md。
EXPOSE 8080/tcp 8081/udp 8008/tcp 7200/tcp 7201/udp 9105/tcp

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/jt808-healthcheck"]

USER jt808:jt808

ENTRYPOINT ["/usr/local/bin/jt808-entrypoint"]
CMD []
