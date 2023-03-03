# Build
# golang:1.19-buster
FROM golang:1.19-alpine AS build
ARG GOPROXY=https://goproxy.cn,direct
WORKDIR /app

RUN apk add \
  bash \
  git \
  make

COPY . .

RUN make prepare compile

# Deploy

FROM alpine:latest

WORKDIR /app

COPY --from=build /app/output/ /app/

EXPOSE 8081
EXPOSE 8080
EXPOSE 8008

CMD ["/app/bin/jt808-server-go"]
