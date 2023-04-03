#!/usr/bin/env bash

set -uo pipefail

$(go env GOPATH)/bin/golangci-lint run ./...
exit $?
