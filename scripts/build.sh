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
	output=$target_dir/release/$package

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
	# 只复制运行时配置文件（yaml/txt），不复制 configs.go，避免构建产物目录
	# 被 go list ./... 识别成一个额外的 Go 包。
	conf_dir=$1
	mkdir -p $conf_dir
	for f in "$conf_src"*.yaml "$conf_src"*.txt; do
		if [ -e "$f" ]; then
			cp "$f" "$conf_dir"/
		fi
	done
}

# target: jt808-server-go / jt808-client-go / jt808-migrate
target=$2
project_dir=$(git rev-parse --show-toplevel)
target_dir="${project_dir}/target"
time=$(date +'%Y-%m-%dT%H:%M:%S')
version=$(git rev-parse --short HEAD)
build_file="cmd/jt808-server/main.go"
conf_src="$project_dir/configs/"
if [[ $target == "jt808-client-go" ]]; then
	build_file="cmd/jt808-client/main.go"
	conf_src="$project_dir/client/configs/"
elif [[ $target == "jt808-migrate" ]]; then
	build_file="cmd/jt808-migrate/main.go"
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
	if [[ -n "${3:-}" && -n "${4:-}" ]]; then
		# 单平台构建，供 Dockerfile 等只需要一个目标的场景使用
		release "$3" "$4"
	else
		# OS X Mac
		release darwin amd64
		release darwin arm64

		# Linux
		release linux amd64
		release linux arm64
	fi
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
