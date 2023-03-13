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
	output=$target_dir/debug/$target
	mkdir -p $output
	copy_conf $output/configs

	go build -o $output/$target \
		-ldflags "-X main.buildTime=$time -X main.buildCommit=$version" \
		$build_file	
}

function release() {
	os=$1
	arch=$2
	package="${target}_${os}_${arch}"
	output=$target_dir/releases/$package

	echo "build $package..."
	mkdir -p $output
	copy_conf $output/configs

	# 编译静态链接，Linux alpine 发行版要求可执行文件是静态链接
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
		go build -tags $target \
		-o $output/$target \
		-ldflags "-X main.buildTime=$time -X main.buildCommit=$version" \
		$build_file	
}

function copy_conf() {
	conf_dir=$1
	mkdir -p $conf_dir
	cp -r $conf_src $conf_dir

	echo "compiling default conf to embed asset.go"
	go-bindata -o internal/config/asset.go -pkg config $conf_asset
}

# target: jt808-server-go / jt808-client-go
target=$2
project_dir=$(git rev-parse --show-toplevel)
target_dir="${project_dir}/target"
time=$(date +'%Y-%m-%dT%H:%M:%S')
version=$(git rev-parse --short HEAD)
build_file="main.go"
conf_src="$project_dir/configs/"
conf_asset="configs test/client/configs"
if [[ $target == "jt808-client-go" ]]; then
	build_file="test/client/main.go"
	conf_src="$project_dir/test/client/configs/"
fi

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
