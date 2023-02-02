#!/usr/bin/env bash

function install_golangcilint() {
    if command -v golangci-lint >/dev/null 2>&1; then
        return 0
    else
        go install github.com/golangci/golangci-lint/cmd/golangci-lint@master
    fi
}

case "$1" in
golangcilint)
    install_golangcilint
    ret=0
    ;;
*)
    echo "UnknownArgs"
    ret=1
    ;;
esac

exit $ret
