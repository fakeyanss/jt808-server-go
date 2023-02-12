# syntax=docker/dockerfile:1

# Build

FROM golang:1.19-buster AS build
ARG GOPROXY=https://goproxy.cn,direct
WORKDIR /app

COPY ./ /app/

RUN make prepare compile package

# Deploy

FROM alpine:latest

WORKDIR /

COPY --from=build /output/ /app/

EXPOSE 8081
EXPOSE 8080

USER nonroot:nonroot

CMD ["/app/bin/jt808-server-go"]
