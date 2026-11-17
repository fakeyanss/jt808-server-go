#!/usr/bin/env bash

# 统一构建入口。编译、镜像与发布包的入口清单都来自 build/targets.tsv，
# 不要在这里再维护第二份二进制列表。
#
# 用法:
#   scripts/build.sh test                        # 单元测试与覆盖率报告
#   scripts/build.sh compile                     # 本机平台，全部目标 -> target/debug/
#   scripts/build.sh image <outdir>              # 镜像内目标，输出到 outdir（Dockerfile 使用）
#   scripts/build.sh image <outdir> <target>...  # 只构建指定目标（模拟终端镜像使用）
#   scripts/build.sh release <version>            # 6 个平台打包 -> target/dist/
#   scripts/build.sh release <version> <os> <arch> # 单平台，供 CI matrix 使用
#   scripts/build.sh clean

set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
manifest="$root_dir/build/targets.tsv"
target_dir="$root_dir/target"
binary_name="jt808-server-go"
ldflags="-s -w"

cd "$root_dir"

# 按清单筛选目标，输出 "<二进制名>\t<包路径>"。
# filter: all / image / release
# 额外传入目标名时只构建这些目标，忽略清单里的镜像/发布列；
# 模拟终端镜像据此只取 jt808-client-go。
function targets() {
	local filter=$1
	shift
	awk -F'\t' -v filter="$filter" -v only="$*" '
		BEGIN {
			n = split(only, names, " ")
			for (i = 1; i <= n; i++) wanted[names[i]] = 1
		}
		/^[[:space:]]*#/ || NF < 4 { next }
		{
			picked = (filter == "all" ||
				(filter == "image" && $3 == 1) ||
				(filter == "release" && $4 == 1))
			if (n > 0) picked = ($1 in wanted)
			if (picked) print $1 "\t" $2
		}
	' "$manifest"
}

function build_targets() {
	local outdir=$1 os=$2 arch=$3 filter=$4
	shift 4
	local exe="" name pkg
	if [ "$os" = "windows" ]; then
		exe=".exe"
	fi

	mkdir -p "$outdir"
	while IFS=$'\t' read -r name pkg; do
		[ -n "$name" ] || continue
		echo "==> build $name ($os/$arch)"
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
			go build -trimpath -buildvcs=false -ldflags "$ldflags" \
			-o "$outdir/$name$exe" "$pkg"
	done < <(targets "$filter" "$@")
}

function test() {
	local output=$target_dir/test
	mkdir -p "$output"

	# 测试覆盖率文件
	local covprof=${output}/covprof.out  # coverage profile
	local covfunc=${output}/covfunc.txt  # coverage profile information for each function
	local covhtml=${output}/covhtml.html # HTML representation of coverage profile

	local gopkgs
	gopkgs=$(go list ./... | grep -vE "vendor")
	go test -race -timeout=120s -v -cover $gopkgs -coverprofile="$covprof" | tee "$output/unittest.txt"
	go tool cover -func="$covprof" -o "$covfunc"
	go tool cover -html="$covprof" -o "$covhtml"
}

function image() {
	local outdir=${1:?usage: $0 image <outdir> [target...]}
	shift
	# 多架构构建时由 buildx 注入 TARGETOS/TARGETARCH；本机直接构建时回退到 go env。
	build_targets "$outdir" "${TARGETOS:-$(go env GOOS)}" "${TARGETARCH:-$(go env GOARCH)}" image "$@"
}

function release() {
	local version=${1:-}
	if [ -z "$version" ]; then
		echo "usage: $0 release <version> [<os> <arch>]" >&2
		exit 1
	fi

	local dist_dir="$target_dir/dist"
	local platforms=()
	if [ -n "${2:-}" ] && [ -n "${3:-}" ]; then
		platforms=("$2 $3")
	else
		platforms=(
			"linux amd64"
			"linux arm64"
			"darwin amd64"
			"darwin arm64"
			"windows amd64"
			"windows arm64"
		)
	fi

	mkdir -p "$dist_dir"
	# macOS 打包时避免写入 AppleDouble 资源分支文件
	export COPYFILE_DISABLE=1

	local platform os arch package stage
	for platform in "${platforms[@]}"; do
		read -r os arch <<<"$platform"
		package="${binary_name}_${version}_${os}_${arch}"
		stage="$dist_dir/$package"

		echo "==> package $package"
		rm -rf "$stage"
		mkdir -p "$stage"

		build_targets "$stage" "$os" "$arch" release

		cp "$root_dir/LICENSE" "$stage/LICENSE"
		cp "$root_dir/README.md" "$stage/README.md"
		printf '%s\n' "$version" >"$stage/VERSION"

		if [ "$os" = "windows" ]; then
			rm -f "$dist_dir/$package.zip"
			(cd "$dist_dir" && zip -qr "$package.zip" "$package")
		else
			tar -czf "$dist_dir/$package.tar.gz" -C "$dist_dir" "$package"
		fi

		echo "==> packaged $dist_dir/$package"
	done
}

case "${1:-}" in
test)
	test
	;;
compile)
	# 先清空输出目录：旧版布局在 target/debug/<name>/ 下留过同名目录，
	# go build -o 遇到已存在的目录会把二进制写进目录里，产物布局就不平铺了。
	rm -rf "$target_dir/debug"
	build_targets "$target_dir/debug" "$(go env GOOS)" "$(go env GOARCH)" all
	;;
image)
	image "${@:2}"
	;;
release)
	release "${2:-}" "${3:-}" "${4:-}"
	;;
clean)
	echo "clean build output..."
	rm -rf "$target_dir"
	;;
*)
	echo "UnknownArgs: ${1:-}" >&2
	echo "usage: $0 {test|compile|image <outdir> [target...]|release <version> [<os> <arch>]|clean}" >&2
	exit 1
	;;
esac
