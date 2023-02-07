# 初始化项目目录变量
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output
COVDIR  := $(HOMEDIR)/cov

GOPKGS  := $$(go list ./...| grep -vE "vendor")

# 设置编译时所需要的 Go 环境
export GOENV = $(HOMEDIR)/go.env

# 测试覆盖率文件 
COVPROF := $(COVDIR)/covprof.out  # coverage profile
COVFUNC := $(COVDIR)/covfunc.txt  # coverage profile information for each function
COVHTML := $(COVDIR)/covhtml.html # HTML representation of coverage profile

# 程序编译产出信息
PROG_NAME    := jt808-server-go
BUILD_TIME   := $(shell date +'%Y-%m-%dT%H:%M:%S')
$(info BUILD_TIME: $(BUILD_TIME))
BUILD_COMMIT := $(shell git rev-parse HEAD)
$(info BUILD_COMMIT: $(BUILD_COMMIT))

# 执行编译，可使用命令 make 或 make all 执行， 顺序执行 prepare -> lint -> compile -> test -> package 几个阶段
all: prepare lint compile test package

# prepare阶段， 使用 bcloud 下载非 Go 依赖，可单独执行命令: make prepare
prepare: prepare-dep
prepare-dep:
	$(shell bash $(CURDIR)/scripts/install.sh golangcilint) # 下载非 Go 依赖
	git version # 低于 2.17.1 可能不能正常工作
	go env # 打印出 go 环境信息，可用于排查问题

set-env:
	go mod download -x || go mod download -x # 下载 Go 依赖

# complile 阶段，执行编译命令，可单独执行命令: make compile
compile: build
build: set-env
	go build -o $(HOMEDIR)/${PROG_NAME} \
	-ldflags "-X main.buildTime=$(BUILD_TIME) -X main.buildCommit=$(BUILD_COMMIT) -X main.progName=$(PROG_NAME)" \
	main.go

# test 阶段，进行单元测试， 可单独执行命令: make test
test: test-case
test-case: set-env
	mkdir -p $(COVDIR)
	go test -race -timeout=120s -v -cover $(GOPKGS) -coverprofile=$(COVPROF) | tee $(COVDIR)/unittest.txt
	go tool cover -func=$(COVPROF) -o $(COVFUNC)
	go tool cover -html=$(COVPROF) -o $(COVHTML)

# package 阶段，对编译产出进行打包，输出到 output 目录， 可单独执行命令: make package
package: package-bin
package-bin:
	mkdir -p $(OUTDIR)/bin
	mv $(HOMEDIR)/${PROG_NAME} $(OUTDIR)/bin/
	mkdir -p $(OUTDIR)/bin
	cp -r $(HOMEDIR)/configs/ $(OUTDIR)/conf/

lint: set-env
	golangci-lint run ./...

# clean 阶段，清除过程中的输出， 可单独执行命令: make clean
clean:
	rm -rf $(OUTDIR) $(COVDIR)

# avoid filename conflict and speed up build
.PHONY: all prepare compile test package clean build lint