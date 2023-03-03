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
