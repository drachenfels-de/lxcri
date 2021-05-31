COMMIT_HASH = $(shell git describe --always --tags --long)
COMMIT = $(shell git describe --always --tags --long --dirty)
BINS := lxcri
LIBEXEC_BINS := lxcri-start lxcri-init lxcri-hook lxcri-hook-builtin
# Installation prefix for BINS
PREFIX ?= /usr/local
export PREFIX
LIBEXEC_DIR = $(PREFIX)/libexec/lxcri
export LIBEXEC_DIR
PKG_CONFIG_PATH ?= $(PREFIX)/lib/pkgconfig
# Note: The default pkg-config directory is search after PKG_CONFIG_PATH
# Note: (Exported) environment variables are NOT visible in the environment of the $(shell ...) function.
export PKG_CONFIG_PATH
VERSION ?= $(COMMIT)
LDFLAGS=-X main.version=$(VERSION) -X main.defaultLibexecDir=$(LIBEXEC_DIR)
CC ?= cc
SHELL_SCRIPTS = $(shell find . -name \*.sh)
GO_SRC = $(shell find . -name \*.go | grep -v _test.go)
C_SRC = $(shell find . -name \*.c)
TESTCOUNT ?= 1
# folder that contains the binaries used for testing
# (defaults to the directory containing the Makefile)
LIBEXEC_TESTDIR ?= $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
# reduce open file descriptor limit for testing too detect file descriptor leaks early
MAX_OPEN_FILES ?= 30
export MAX_OPEN_FILES
export LIBEXEC_TESTDIR

all: fmt test

update-tools:
	GO111MODULE=off go get -u mvdan.cc/sh/v3/cmd/shfmt
	GO111MODULE=off go get -u golang.org/x/lint/golint
	GO111MODULE=off go get -u honnef.co/go/tools/cmd/staticcheck

fmt:
	go fmt ./...
	shfmt -w $(SHELL_SCRIPTS)
	clang-format -i --style=file $(C_SRC)
	golint ./...
	go mod tidy
	staticcheck ./...

# NOTE: Running the test target requires a running systemd.
.PHONY: test
test: build lxcri-test
	./test.sh --failfast --count $(TESTCOUNT) ./...

test-privileged: build lxcri-test
	ulimit -n $(MAX_OPEN_FILES) && \
		LXCRI_LIBEXEC=$(LIBEXEC_TESTDIR) \
		go test --failfast --count $(TESTCOUNT) -v ./...

.PHONY: build
build: $(BINS) $(LIBEXEC_BINS)

lxcri: go.mod $(GO_SRC) Makefile
	go build -ldflags '$(LDFLAGS)' -o $@ ./cmd/lxcri

lxcri-start: cmd/lxcri-start/lxcri-start.c
	$(CC) -Werror -Wpedantic -o $@ $? $$(pkg-config --libs --cflags lxc)

lxcri-init: go.mod $(GO_SRC) Makefile
	CGO_ENABLED=0 go build -o $@ ./cmd/lxcri-init
	# this is paranoia - but ensure it is statically compiled
	! ldd $@  2>/dev/null

lxcri-hook: go.mod $(GO_SRC) Makefile
	go build -o $@ ./cmd/$@

lxcri-hook-builtin: go.mod $(GO_SRC) Makefile
	go build -o $@ ./cmd/$@

lxcri-test: go.mod $(GO_SRC) Makefile
	go build -o $@ ./pkg/internal/$@

install: build
	mkdir -p $(PREFIX)/bin
	cp -v $(BINS) $(PREFIX)/bin
	mkdir -p $(LIBEXEC_DIR)
	cp -v $(LIBEXEC_BINS) $(LIBEXEC_DIR)

.PHONY: clean
clean:
	-rm -f $(BINS) $(LIBEXEC_BINS) lxcri-test

