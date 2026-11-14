#!/usr/bin/env bash

# 构建发布包，产物结构与手工 v1.0.0 保持一致：
#   jt808-server-go_<version>_<os>_<arch>/
#     ├── jt808-server-go
#     ├── jt808-client-go
#     ├── jt808-migrate
#     ├── jt-server-standalone
#     ├── jt-signal
#     ├── jt-control
#     ├── jt-media
#     ├── jt-attachment
#     ├── LICENSE
#     ├── README.md
#     └── VERSION
# 每个平台一个包：linux/darwin 为 tar.gz，windows 为 zip。
#
# 用法:
#   scripts/package-release.sh <version>                  # 全部 6 个平台
#   scripts/package-release.sh <version> <os> <arch>       # 单个平台，供 CI matrix 使用

set -euo pipefail

binary_name="jt808-server-go"
project_dir=$(git rev-parse --show-toplevel)
dist_dir="$project_dir/target/dist"

version="${1:-}"
if [ -z "$version" ]; then
	echo "usage: $0 <version> [<os> <arch>]" >&2
	exit 1
fi

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

# 包路径与二进制名的对应关系，覆盖 cmd/ 下全部 8 个入口
entries=(
	"cmd/jt808-server:jt808-server-go"
	"cmd/jt808-client:jt808-client-go"
	"cmd/jt808-migrate:jt808-migrate"
	"cmd/jt-server-standalone:jt-server-standalone"
	"cmd/jt-signal:jt-signal"
	"cmd/jt-control:jt-control"
	"cmd/jt-media:jt-media"
	"cmd/jt-attachment:jt-attachment"
)

cd "$project_dir"
mkdir -p "$dist_dir"
# macOS 打包时避免写入 AppleDouble 资源分支文件
export COPYFILE_DISABLE=1

for platform in "${platforms[@]}"; do
	read -r os arch <<<"$platform"
	package="${binary_name}_${version}_${os}_${arch}"
	stage="$dist_dir/$package"

	exe_suffix=""
	if [ "$os" = "windows" ]; then
		exe_suffix=".exe"
	fi

	echo "==> build $package"
	rm -rf "$stage"
	mkdir -p "$stage"

	for entry in "${entries[@]}"; do
		pkg="${entry%%:*}"
		name="${entry##*:}"
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
			go build -trimpath -ldflags "-s -w" -o "$stage/$name$exe_suffix" "./$pkg"
	done

	cp "$project_dir/LICENSE" "$stage/LICENSE"
	cp "$project_dir/README.md" "$stage/README.md"
	printf '%s\n' "$version" >"$stage/VERSION"

	if [ "$os" = "windows" ]; then
		rm -f "$dist_dir/$package.zip"
		(cd "$dist_dir" && zip -qr "$package.zip" "$package")
	else
		tar -czf "$dist_dir/$package.tar.gz" -C "$dist_dir" "$package"
	fi

	echo "==> packaged $dist_dir/$package"
done
