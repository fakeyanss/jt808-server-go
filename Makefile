# 初始化项目目录变量
CURDIR := $(shell pwd)
# 设置编译时所需要的 Go 环境
export GOENV = $(CURDIR)/go.env
# 程序编译产出信息
PROG_NAME := jt808-server-go
TEST_CLIENT_NAME := jt808-client-go
MIGRATE_NAME := jt808-migrate
BUILD_TIME := $(shell date +'%Y-%m-%dT%H:%M:%S')
BUILD_COMMIT := $(shell git rev-parse --short HEAD)
$(info BUILD_TIME: $(BUILD_TIME))
$(info BUILD_COMMIT: $(BUILD_COMMIT))
$(info ========================================)

# 执行编译，可使用命令 make 或 make all 执行
all: test compile

set-env:
	go mod download -x || go mod download -x # 下载 Go 依赖

# test 阶段，进行单元测试，可单独执行命令: make test
test: set-env
	bash $(CURDIR)/scripts/build.sh test

# complile 阶段，执行编译命令并打包，可单独执行命令: make compile
compile: set-env
	bash $(CURDIR)/scripts/build.sh compile $(PROG_NAME)
compile-client: set-env
	bash $(CURDIR)/scripts/build.sh compile $(TEST_CLIENT_NAME)
compile-migrate: set-env
	bash $(CURDIR)/scripts/build.sh compile $(MIGRATE_NAME)

run: set-env
	go run $(CURDIR)/main.go -c configs/default.yaml
run-client: set-env
	go run $(CURDIR)/test/client/main.go -c $(CURDIR)/test/client/configs/default.yaml

# release 阶段，单独执行交叉编译命令并打包
release: set-env
	bash $(CURDIR)/scripts/build.sh release $(PROG_NAME)
	bash $(CURDIR)/scripts/build.sh release $(MIGRATE_NAME)
release-client: set-env
	bash $(CURDIR)/scripts/build.sh release $(TEST_CLIENT_NAME)
release-migrate: set-env
	bash $(CURDIR)/scripts/build.sh release $(MIGRATE_NAME)

# smoke 阶段，构建真实 server/client 二进制并跑端到端冒烟，可单独执行: make smoke
# 可选: --keep-logs 保留日志，或 SMOKE_TCP_PORT/SMOKE_UDP_PORT/SMOKE_HTTP_PORT 覆盖端口
smoke:
	bash $(CURDIR)/scripts/smoke.sh

# clean 阶段，清除过程中的输出， 可单独执行命令: make clean
clean:
	bash $(CURDIR)/scripts/build.sh clean

# 构建镜像
dockerbuild:
	docker build -f build/Dockerfile -t fakeyanss/jt808-server-go:$(BUILD_COMMIT) .

# 统计代码行数
statline:
	@total=`find . | grep "\.go$$" | xargs -I f cat f | wc -l`; \
	echo "TOTAL_CODE_LINE: $$total"

# avoid filename conflict and speed up build
.PHONY: all test compile release clean smoke compile-migrate release-migrate
