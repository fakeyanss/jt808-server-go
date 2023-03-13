#!/usr/bin/env bash

set -uo pipefail

function install_golangcilint() {
	golangci-lint --version >/dev/null
	if [[ $? == 0 ]]; then
		return 0
	fi
	echo "golangci-lint not downloaded"
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@master
}

function install_gobindata() {
	go-bindata -version >/dev/null
	if [[ $? == 0 ]]; then
		return 0
	fi
	echo "go-bindata not downloaded"	
	go install github.com/go-bindata/go-bindata/...@latest
}

case "$1" in
golangcilint)
	install_golangcilint
	ret=0
	;;
gobindata)
	install_gobindata
	ret=0
	;;
*)
	echo "UnknownArgs"
	ret=1
	;;
esac

exit $ret
