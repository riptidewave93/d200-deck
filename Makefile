# Builds and tests run in a Go container, so any host with Docker (and CI) builds the same way.
# DOCKER= runs them with the host's Go instead: make test DOCKER=
GO_IMAGE ?= golang:1.27.0-bookworm
DOCKER ?= docker
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
OUT := build
# Package version: v1.2.3 -> 1.2.3 on a tagged commit, else 0.0.0~git<date>.<commit>
PKG_VERSION ?= $(shell t=$$(git describe --tags --exact-match 2>/dev/null) && echo $${t\#v} || \
	echo 0.0.0~git$$(git show -s --format=%cd --date=format:%Y%m%d HEAD).$$(git rev-parse --short HEAD))
NFPM_VERSION := v2.41.1
# The website builds its WebAssembly with TinyGo
TINYGO_IMAGE ?= tinygo/tinygo:0.43.0
WASMTIME_VERSION := v29.0.1

REPO_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
ifneq ($(DOCKER),)
RUN := $(DOCKER) run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp \
	-e GOCACHE=/repo/.cache/go-build -e GOMODCACHE=/repo/.cache/mod -e GOFLAGS=-buildvcs=false \
	-v "$(REPO_DIR)":/repo:Z -w /repo $(GO_IMAGE)
TINYGO := $(DOCKER) run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp \
	-e PATH=/repo/.cache/bin:/usr/local/tinygo/bin:/usr/local/go/bin:/usr/bin:/bin \
	-e GOCACHE=/repo/.cache/tinygo-build -e GOMODCACHE=/repo/.cache/mod -e GOFLAGS=-buildvcs=false \
	-v "$(REPO_DIR)":/repo:Z -w /repo $(TINYGO_IMAGE)
endif

LDFLAGS := -s -w -X main.version=$(VERSION)
DEVICE := GOOS=linux GOARCH=arm64 CGO_ENABLED=0
PC := GOOS=linux GOARCH=amd64 CGO_ENABLED=0
WINDOWS := GOOS=windows GOARCH=amd64 CGO_ENABLED=0
# what deckhost builds from: checked for Windows too
# what the configuration website runs in the browser
WASM := GOOS=js GOARCH=wasm
WASM_PKGS := ./render ./deck ./icons ./fonts
PC_PKGS := ./cmd/deckhost ./internal/discord ./internal/obs ./internal/hidraw ./proto/... ./deck ./netid

.PHONY: all build deckd deckhost deckhost.exe install dist dist-linux dist-windows test tinygo check cover clean

all : check test tinygo build

# The device program (arm64: the keys and the update page) and the PC program (Linux and
# Windows amd64) into ./build
build : deckd deckhost deckhost.exe

deckd :
	$(RUN) env $(DEVICE) go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/$@

deckhost :
	$(RUN) env $(PC) go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/$@

# a GUI program: no console window at login (commands still print in a terminal)
deckhost.exe :
	$(RUN) env $(WINDOWS) go build -trimpath -ldflags '$(LDFLAGS) -H=windowsgui' -o $(OUT)/$@ ./cmd/deckhost

# Installs or updates deckhost for the current user from this checkout, as the Linux tarball's
# install.sh does, and restarts it
install : deckhost
	rm -rf $(OUT)/stage/install && mkdir -p $(OUT)/stage/install
	cp $(OUT)/deckhost contrib/linux/install.sh contrib/linux/deckhost.service \
		contrib/linux/deckhost-tray.desktop contrib/linux/70-d200-deck.rules $(OUT)/stage/install/
	sh $(OUT)/stage/install/install.sh

# Installers into ./build/dist: see docs/install.md
dist : dist-linux dist-windows

# .deb, .rpm and a tarball with install.sh, for amd64 and arm64
dist-linux : .cache/bin/nfpm
	for a in amd64 arm64; do \
		$(RUN) env GOOS=linux GOARCH=$$a CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/linux-$$a/deckhost ./cmd/deckhost && \
		$(RUN) sh contrib/linux/package.sh $$a '$(PKG_VERSION)' $(OUT)/linux-$$a/deckhost || exit 1; \
	done

# A per-user installer, built with NSIS in its own image (DOCKER= uses the host's makensis)
dist-windows : deckhost.exe .cache/nsis-image
	mkdir -p $(OUT)/dist
	$(if $(DOCKER),$(DOCKER) run --rm --user $(shell id -u):$(shell id -g) -v "$(REPO_DIR)":/repo:Z -w /repo d200-deck-nsis) \
		makensis -V2 -NOCD -DVERSION='$(PKG_VERSION)' -DOUTFILE='$(OUT)/dist/deckhost-$(PKG_VERSION)-windows-amd64-setup.exe' contrib/windows/deckhost.nsi

.cache/nsis-image : contrib/windows/Dockerfile
	$(if $(DOCKER),$(DOCKER) build -q -t d200-deck-nsis contrib/windows)
	mkdir -p .cache && touch $@

.cache/bin/nfpm :
	$(RUN) sh -c 'GOBIN=$$PWD/.cache/bin go install github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION)'

# Unit tests with the race detector, and a coverage summary; the website's packages also with
# 32-bit ints, as in TinyGo's WebAssembly
test :
	mkdir -p $(OUT)
	$(RUN) sh -c 'go test -race -coverprofile=$(OUT)/cover.out ./... && go tool cover -func=$(OUT)/cover.out | tail -1 && \
		GOARCH=386 go test $(WASM_PKGS)'

# The website's packages' tests built with TinyGo, as the website builds them (its JSON without
# encoding/json/v2), run in wasmtime. TestLoad* use temp dirs WASI can't remove.
tinygo : .cache/bin/wasmtime
	$(TINYGO) env GOEXPERIMENT=nojsonv2 tinygo test -target wasip1 -skip '^TestLoad' $(WASM_PKGS)

.cache/bin/wasmtime :
	mkdir -p .cache/bin
	curl -sfL https://github.com/bytecodealliance/wasmtime/releases/download/$(WASMTIME_VERSION)/wasmtime-$(WASMTIME_VERSION)-x86_64-linux.tar.xz | \
		tar -xJ -C .cache/bin --strip-components=1 wasmtime-$(WASMTIME_VERSION)-x86_64-linux/wasmtime

# Per-function coverage of the last test run
cover :
	$(RUN) go tool cover -func=$(OUT)/cover.out

# Formatting, vet (Linux, the PC packages for Windows, what the website runs as WebAssembly)
# and a tidy go.mod
check :
	$(RUN) sh -c 'f=$$(find . -name "*.go" -not -path "./.cache/*" | xargs gofmt -l); test -z "$$f" || { echo "$$f"; exit 1; }; \
		go vet ./... && $(WINDOWS) go vet $(PC_PKGS) && $(WASM) go vet $(WASM_PKGS) && go mod tidy -diff'

clean :
	rm -rf ./$(OUT) ./.cache
