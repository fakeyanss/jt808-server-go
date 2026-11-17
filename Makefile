# 初始化项目目录变量
# Go 环境直接使用开发机上的 `go env`，仓库不再提供 go.env。
CURDIR := $(shell pwd)
# 二进制清单只有一份：build/targets.tsv，由 scripts/build.sh 读取
BUILD_COMMIT := $(shell git rev-parse --short HEAD)
# release 版本号：默认取最近的 tag，可用 make release VERSION=v1.2.3 覆盖
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
$(info BUILD_COMMIT: $(BUILD_COMMIT))
$(info ========================================)

# 执行编译，可使用命令 make 或 make all 执行
all: test vet compile

set-env:
	go mod download -x || go mod download -x # 下载 Go 依赖

# test 阶段，进行单元测试，可单独执行命令: make test
test: set-env
	bash $(CURDIR)/scripts/build.sh test

# vet 阶段，执行静态检查，可单独执行命令: make vet
vet: set-env
	go vet ./...

# outbox 回归：CI 显式点名这几条用例，避免重构后「绿灯但没跑」
# 可单独执行命令: make test-outbox
test-outbox: set-env
	@mkdir -p $(CURDIR)/target/test
	set -eo pipefail; go test -race -count=1 -v \
		./internal/control/ \
		./internal/repository/ \
		./internal/repository/memory/ \
		./internal/repository/sqlite/ | tee $(CURDIR)/target/test/outbox-regression.log
	grep -q -- '--- PASS: TestReplayAfterRestart' $(CURDIR)/target/test/outbox-regression.log
	grep -q -- '--- PASS: TestCommandRepositoryMemoryPendingScanAndLease' $(CURDIR)/target/test/outbox-regression.log
	grep -q -- '--- PASS: TestSQLiteCommandOutboxIdempotencyAndResult' $(CURDIR)/target/test/outbox-regression.log

# 协议注册表与覆盖矩阵一致性校验，可单独执行命令: make check-registry-doc
check-registry-doc: set-env
	bash $(CURDIR)/scripts/registry-doc.sh

# compile 阶段，把清单里全部二进制编译到 target/debug/，可单独执行命令: make compile
compile: set-env
	bash $(CURDIR)/scripts/build.sh compile

run: set-env
	go run $(CURDIR)/cmd/jt-server-standalone/main.go -c conf/default.yaml
run-client: set-env
	go run $(CURDIR)/cmd/jt808-client/main.go -c $(CURDIR)/client/configs/default.yaml

# release 阶段：交叉编译 6 个平台并按发布格式打包到 target/dist/
# 也可只构建单平台: make release OS=linux ARCH=arm64
release: set-env
	bash $(CURDIR)/scripts/build.sh release $(VERSION) $(OS) $(ARCH)

# smoke 阶段，构建真实 server/client 二进制并跑端到端冒烟，可单独执行: make smoke
# 可选: --keep-logs 保留日志，或 SMOKE_TCP_PORT/SMOKE_UDP_PORT/SMOKE_HTTP_PORT 覆盖端口
smoke:
	bash $(CURDIR)/scripts/smoke.sh

# 集群终端级冒烟：真实 compose 集群 + 模拟终端，可单独执行: make smoke-cluster
# 镜像构建可传 Go 模块代理: make smoke-cluster GOPROXY=https://goproxy.cn,direct
# 未设置时不传 build-arg，沿用 Dockerfile 自身默认代理。
smoke-cluster:
	bash $(CURDIR)/scripts/smoke-cluster.sh $(if $(GOPROXY),--goproxy '$(GOPROXY)')

# 本地压测基线，可单独执行: make loadtest 或 make loadtest LOAD_DURATION=30
LOAD_DURATION ?= 60
loadtest:
	bash $(CURDIR)/scripts/loadtest.sh $(LOAD_DURATION)

# clean 阶段，清除过程中的输出， 可单独执行命令: make clean
clean:
	bash $(CURDIR)/scripts/build.sh clean

# 构建镜像（单架构，使用本机 Docker）
IMAGE ?= fakeyanss/jt808-server-go
# 模拟终端镜像只含 jt808-client-go，供测试环境连接服务端
IMAGE_CLIENT ?= fakeyanss/jt808-client-go
IMAGE_TAG ?= $(BUILD_COMMIT)
dockerbuild:
	docker build -f build/Dockerfile -t $(IMAGE):$(IMAGE_TAG) .
dockerbuild-client:
	docker build -f build/Dockerfile.client -t $(IMAGE_CLIENT):$(IMAGE_TAG) .

# 统计代码行数
statline:
	@total=`find . | grep "\.go$$" | xargs -I f cat f | wc -l`; \
	echo "TOTAL_CODE_LINE: $$total"

# avoid filename conflict and speed up build
.PHONY: all set-env test vet test-outbox check-registry-doc compile run run-client release smoke smoke-cluster loadtest clean dockerbuild dockerbuild-client statline
