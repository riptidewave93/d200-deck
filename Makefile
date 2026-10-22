# Builds and tests run in a Go container, so any host with Docker or Podman (and CI) builds the
# same way. Without either, or with DOCKER=, they use the host's Go: make test DOCKER=
GO_IMAGE ?= docker.io/library/golang:1.27.0-bookworm
ifeq ($(origin DOCKER),undefined)
DOCKER := $(shell command -v docker >/dev/null 2>&1 && echo docker || { command -v podman >/dev/null 2>&1 && echo podman; })
endif
# Release tags are lightweight, so describe needs --tags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
OUT := build
# Package version: v1.2.3 -> 1.2.3 on a tagged commit, else 0.0.0~git<date>.<commit>
TAG := $(shell git describe --tags --exact-match 2>/dev/null)
PKG_VERSION ?= $(if $(TAG),$(TAG:v%=%),0.0.0~git$(shell git show -s --format=%cd --date=format:%Y%m%d HEAD).$(shell git rev-parse --short HEAD))
NFPM_VERSION := v2.41.1
WINRES_VERSION := v0.3.3
# The website builds its WebAssembly with TinyGo
TINYGO_IMAGE ?= docker.io/tinygo/tinygo:0.43.0
WASMTIME_VERSION := v50.0.0

REPO_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
ifneq ($(DOCKER),)
# rootless Podman keeps the user's ID with keep-id, not --user
USER_FLAGS := $(if $(findstring podman,$(shell $(DOCKER) --version 2>/dev/null)),--userns=keep-id,--user $(shell id -u):$(shell id -g))
RUN := $(DOCKER) run --rm $(USER_FLAGS) -e HOME=/tmp \
	-e GOCACHE=/repo/.cache/go-build -e GOMODCACHE=/repo/.cache/mod -e GOFLAGS=-buildvcs=false \
	-v "$(REPO_DIR)":/repo:Z -w /repo $(GO_IMAGE)
TINYGO := $(DOCKER) run --rm $(USER_FLAGS) -e HOME=/tmp \
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

.PHONY: all build deckd deckhost deckhost.exe deckhost-macos install dist dist-linux dist-windows dist-macos test test-macos tinygo check cover clean

all : check test tinygo build

# The device program (arm64: the keys and the update page) and the PC program (Linux and
# Windows amd64) into ./build
build : deckd deckhost deckhost.exe

deckd :
	$(RUN) env $(DEVICE) go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/$@

deckhost :
	$(RUN) env $(PC) go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/$@

# a GUI program: no console window at login (commands still print in a terminal)
deckhost.exe : cmd/deckhost/rsrc_windows_amd64.syso
	$(RUN) env $(WINDOWS) go build -trimpath -ldflags '$(LDFLAGS) -H=windowsgui' -o $(OUT)/$@ ./cmd/deckhost

# Builds deckhost for this PC, then installs or updates it for the current user with the Linux
# tarball's install.sh, and restarts it
install :
	@[ -n "$(DOCKER)" ] || command -v go >/dev/null 2>&1 || \
		{ echo "make install needs Docker, Podman or Go $(shell sed -n 's/^go //p' go.mod) or later" >&2; exit 1; }
	rm -rf $(OUT)/stage/install && mkdir -p $(OUT)/stage/install
	$(RUN) env CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/stage/install/deckhost ./cmd/deckhost
	cp contrib/linux/install.sh contrib/linux/deckhost.service contrib/linux/deckhost-tray.desktop \
		contrib/linux/deckhost.desktop contrib/icons/deckhost.svg contrib/linux/70-d200-deck.rules \
		$(OUT)/stage/install/
	sh $(OUT)/stage/install/install.sh

# The exe's icon, a resource go build links in
cmd/deckhost/rsrc_windows_amd64.syso : contrib/icons/deckhost.ico
	$(RUN) go run github.com/tc-hib/go-winres@$(WINRES_VERSION) simply --arch amd64 --manifest none \
		--icon $< --out cmd/deckhost/rsrc

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
	$(if $(DOCKER),$(DOCKER) run --rm $(USER_FLAGS) -v "$(REPO_DIR)":/repo:Z -w /repo d200-deck-nsis) \
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

# On a Mac with Go (Apple Silicon): deckhost needs cgo there, for the tray and IOKit, so these use
# the host's Go and Xcode's command line tools, not a container
deckhost-macos :
	CGO_ENABLED=1 go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/deckhost

test-macos :
	go vet $(PC_PKGS) && go test -race $(PC_PKGS)

# D200 Deck.app, zipped, into ./build/dist
dist-macos : deckhost-macos
	OUT=$(OUT) sh contrib/macos/package.sh $(PKG_VERSION) $(OUT)/deckhost-macos

# The website's packages' tests built with TinyGo, as the website builds them (its JSON without
# encoding/json/v2), run in wasmtime. TestLoad* use temp dirs WASI can't remove. The target is
# wasip1 with wasmtime's cache off: it needs a home directory, which a user ID the image doesn't
# know (as in CI) lacks.
tinygo : .cache/bin/wasmtime
	$(TINYGO) env GOEXPERIMENT=nojsonv2 tinygo test -target contrib/tinygo-wasip1.json -skip '^TestLoad' $(WASM_PKGS)

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
