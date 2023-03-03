#!/usr/bin/env bash

set -eo pipefail

function test() {
	output=$target_dir/test
	mkdir -p $output

	# 测试覆盖率文件
	covprof=${output}/covprof.out  # coverage profile
	covfunc=${output}/covfunc.txt  # coverage profile information for each function
	covhtml=${output}/covhtml.html # HTML representation of coverage profile

	gopkgs=$(go list ./... | grep -vE "vendor")
	go test -race -timeout=120s -v -cover $gopkgs -coverprofile=$covprof | tee $output/unittest.txt
	go tool cover -func=$covprof -o $covfunc
	go tool cover -html=$covprof -o $covhtml
}

function compile() {
	output=$target_dir/debug
	mkdir -p $output
	go build -o $output/$target \
		-ldflags "-X main.buildTime=$time -X main.buildCommit=$version" \
		main.go
	copy_conf $output/configs
}

function release() {
	os=$1
	arch=$2
	package="${target}_${os}_${arch}"
	output=$target_dir/releases/$package

	echo "build $package..."
	mkdir -p $output
	# 编译静态链接，Linux alpine 发行版要求可执行文件是静态链接
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
		go build -tags $target \
		-o $output/$target \
		-ldflags "-X main.buildTime=$time -X main.buildCommit=$version" \
		main.go
	copy_conf $output/configs
}

function copy_conf() {
	conf_dir=$1
	mkdir -p $conf_dir
	cp -r $project_dir/configs/ $conf_dir
}

# target: jt808-server-go / jt808-client-go
target=$2
project_dir=$(git rev-parse --show-toplevel)
target_dir="${project_dir}/target"
time=$(date +'%Y-%m-%dT%H:%M:%S')
version=$(git rev-parse --short HEAD)

ret=0
case "$1" in
test)
	test
	;;
compile)
	compile $2
	;;
release)
	# OS X Mac
	release darwin amd64
	release darwin arm64

	# Linux
	release linux amd64
	release linux arm64
	;;
clean)
	echo "clean test and compile output..."
	rm -rf $target_dir
	;;
*)
	echo "UnknownArgs"
	ret=1
	;;
esac

exit $ret
