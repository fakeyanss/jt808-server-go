# init project path
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output
CONFDIR := $(OUTDIR)/conf

# init command params
GO      := go
GOROOT  := $(shell $(GO) env GOROOT)
GOPATH  := $(shell $(GO) env GOPATH)
GOMOD   := $(GO) mod
GOBUILD := $(GO) build
GOTEST  := $(GO) test -race -timeout 30s -gcflags="-N -l"
GOPKGS  := $$($(GO) list ./...| grep -vE "vendor")

# test cover files
COVPROF := $(HOMEDIR)/covprof.out  # coverage profile
COVFUNC := $(HOMEDIR)/covfunc.txt  # coverage profile information for each function
COVHTML := $(HOMEDIR)/covhtml.html # HTML representation of coverage profile

PROG_NAME    := jt808-server-go
BUILD_TIME   := $(shell date +'%Y-%m-%dT%H:%M:%S')
BUILD_COMMIT := $(shell git rev-parse HEAD)
$(info BUILD_COMMIT: $(BUILD_COMMIT))

# make, make all
all: prepare compile package

# set proxy env
set-env:
	$(GO) env -w GO111MODULE=on

#make prepare, download dependencies
prepare: gomod

gomod: set-env
	$(GOMOD) download -x

#make compile
compile: build

build:
	$(GOBUILD) -o $(OUTDIR)/$(PROG_NAME) \
	            -ldflags "-X main.buildTime=$(BUILD_TIME) -X main.buildCommit=$(BUILD_COMMIT) -X main.progName=$(PROG_NAME)"

lint:
	go vet $(GOPKGS)

# make test, test your code
test: prepare test-case
test-case:
	$(GOTEST) -v -cover $(GOPKGS)

# make package
package: 
	mkdir -p $(CONFDIR) 
	cp -r configs/ $(CONFDIR)

# make clean
clean:
	$(GO) clean
	rm -rf $(OUTDIR)

# avoid filename conflict and speed up build 
.PHONY: all prepare compile test package clean build
