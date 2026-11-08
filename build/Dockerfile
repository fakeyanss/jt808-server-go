# Build
FROM golang:1.26.10-alpine AS build
ARG GOPROXY=https://goproxy.cn,direct
WORKDIR /app

RUN apk add \
  bash \
  git \
  make

COPY . .

RUN bash scripts/build.sh release jt808-server-go linux amd64
RUN bash scripts/build.sh release jt808-migrate linux amd64

# Deploy

FROM alpine:latest

WORKDIR /app/output

COPY --from=build /app/target/release/jt808-server-go_linux_amd64/ /app/output/
COPY --from=build /app/target/release/jt808-migrate_linux_amd64/jt808-migrate /app/output/

EXPOSE 8080
EXPOSE 8081
EXPOSE 8008
EXPOSE 7200
EXPOSE 7201
EXPOSE 9000

CMD ["/app/output/jt808-server-go", "-c", "/app/output/configs/default.yaml"]
