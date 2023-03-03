# 初始化项目目录变量
CURDIR := $(shell pwd)
# 设置编译时所需要的 Go 环境
export GOENV = $(CURDIR)/go.env
# 程序编译产出信息
PROG_NAME    := jt808-server-go
BUILD_TIME   := $(shell date +'%Y-%m-%dT%H:%M:%S')
BUILD_COMMIT := $(shell git rev-parse --short HEAD)
$(info BUILD_TIME: $(BUILD_TIME))
$(info BUILD_COMMIT: $(BUILD_COMMIT))

# 执行编译，可使用命令 make 或 make all 执行
all: prepare lint test compile

# prepare阶段，下载非 Go 依赖，可单独执行命令: make prepare
prepare:
	bash $(CURDIR)/scripts/install.sh golangcilint # 下载非 Go 依赖
	git version # 低于 2.17.1 可能不能正常工作
	go env # 打印出 go 环境信息，可用于排查问题

set-env:
	go mod download -x || go mod download -x # 下载 Go 依赖

# test 阶段，进行单元测试，可单独执行命令: make test
test: set-env
	bash $(CURDIR)/scripts/build.sh test

# complile 阶段，执行编译命令并打包，可单独执行命令: make compile
compile: set-env
	bash $(CURDIR)/scripts/build.sh compile $(PROG_NAME)

run: set-env
	go run $(CURDIR)/main.go

# release 阶段，单独执行交叉编译命令并打包
release: set-env
	bash $(CURDIR)/scripts/build.sh release $(PROG_NAME)

lint: set-env
	golangci-lint run ./...

# clean 阶段，清除过程中的输出， 可单独执行命令: make clean
clean:
	bash $(CURDIR)/scripts/build.sh clean

# 构建镜像
dockerbuild:
	docker build -f build/Dockerfile -t fakeyanss/jt808-server-go:$(BUILD_COMMIT) .

# avoid filename conflict and speed up build
.PHONY: all prepare test compile release lint clean
