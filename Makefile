COMMIT_HASH = $(shell git describe --always --tags --long)
COMMIT = $(shell git describe --always --tags --long --dirty)
BINS := lxcri
LIBEXEC_BINS := lxcri-start lxcri-init lxcri-hook lxcri-hook-builtin
# Installation prefix for BINS
PREFIX ?= /usr/local
export PREFIX
LIBEXEC_DIR = $(PREFIX)/libexec
export LIBEXEC_DIR
PKG_CONFIG_PATH ?= $(PREFIX)/lib/pkgconfig
# Note: The default pkg-config directory is search after PKG_CONFIG_PATH
# Note: (Exported) environment variables are NOT visible in the environment of the $(shell ...) function.
export PKG_CONFIG_PATH
VERSION ?= $(COMMIT)
LDFLAGS=-X main.version=$(VERSION) -X github.com/lxc/lxcri.defaultLibexecDir=$(LIBEXEC_DIR)
ifdef STATIC
	LDFLAGS += -extldflags=-static
	STATIC_BUILD := --static
	# build tags for containers/storage
	TAGS := -tags osusergo,netgo,exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper
endif
CGO_CFLAGS=$(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config $(STATIC_BUILD) --cflags lxc)
CGO_LDFLAGS=$(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) pkg-config $(STATIC_BUILD) --libs lxc)
export CGO_CFLAGS CGO_LDFLAGS

CC ?= cc
SHELL_SCRIPTS = $(shell find . -name \*.sh)
GO_SRC = $(shell find . -name \*.go | grep -v _test.go)
C_SRC = $(shell find . -name \*.c)
TESTCOUNT ?= 1
# reduce open file descriptor limit for testing too detect file descriptor leaks early
MAX_OPEN_FILES ?= 30

all: fmt test

update-tools:
	GO111MODULE=off go get -u mvdan.cc/sh/v3/cmd/shfmt
	GO111MODULE=off go get -u golang.org/x/lint/golint
	GO111MODULE=off go get -u honnef.co/go/tools/cmd/staticcheck

fmt:
	go fmt ./...
	gofmt -s -w .
	shfmt -w $(SHELL_SCRIPTS)
	clang-format -i --style=file $(C_SRC)
	golint ./...
	go mod tidy
	staticcheck ./...

# NOTE: Running the test target requires a running systemd.
.PHONY: test
test: build lxcri-test
	install -d -m 777 /tmp/lxcri-test-libexec
	install -v $(LIBEXEC_BINS) lxcri-test /tmp/lxcri-test-libexec
	LIBEXEC_DIR=/tmp/lxcri-test-libexec \
	MAX_OPEN_FILES=$(MAX_OPEN_FILES) \
	./test.sh --failfast --count $(TESTCOUNT) ./...

test-privileged: build lxcri-test
	install -d -m 777  /tmp/lxcri-test-libexec
	install -v $(LIBEXEC_BINS) lxcri-test /tmp/lxcri-test-libexec
	ulimit -n $(MAX_OPEN_FILES) && \
		LIBEXEC_DIR=/tmp/lxcri-test-libexec \
		go test --failfast --count $(TESTCOUNT) -v ./...

.PHONY: build
build: $(BINS) $(LIBEXEC_BINS)

lxcri: go.mod $(GO_SRC) Makefile
	go build -ldflags '$(LDFLAGS)' $(TAGS) -o $@ ./cmd/$@

# NOTE -lphread and -ldl was added to lxc.pc recently
# https://github.com/lxc/lxc/commit/c2a7a6977b819d2e9aa6bcf60e40166fc960fa7d
lxcri-start: cmd/lxcri-start/lxcri-start.c
	$(CC) $(STATIC_BUILD) -Werror -Wpedantic -o $@ $? $(CGO_CFLAGS) $(CGO_LDFLAGS) -lpthread -ldl

lxcri-init: go.mod $(GO_SRC) Makefile
	CGO_ENABLED=0 go build -o $@ ./cmd/lxcri-init
	# this is paranoia - but ensure it is statically compiled
	! ldd $@  2>/dev/null

lxcri-hook: go.mod $(GO_SRC) Makefile
	CGO_ENABLED=0 go build -o $@ ./cmd/$@

lxcri-hook-builtin: go.mod $(GO_SRC) Makefile
	CGO_ENABLED=0 go build -o $@ ./cmd/$@

lxcri-test: go.mod $(GO_SRC) Makefile
	CGO_ENABLED=0 go build -o $@ ./pkg/internal/$@

install: build
	mkdir -p $(PREFIX)/bin
	install -v --strip $(BINS) $(PREFIX)/bin
	mkdir -p $(LIBEXEC_DIR)
	install -v --strip $(LIBEXEC_BINS) $(LIBEXEC_DIR)

.PHONY: clean
clean:
	-rm -f $(BINS) $(LIBEXEC_BINS) lxcri-test

