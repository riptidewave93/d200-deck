# Builds and tests run in a Go container, so any host with Docker (and CI) builds the same way.
# DOCKER= runs them with the host's Go instead: make test DOCKER=
GO_IMAGE ?= golang:1.27.0-bookworm
DOCKER ?= docker
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
OUT := build

REPO_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
ifneq ($(DOCKER),)
RUN := $(DOCKER) run --rm --user $(shell id -u):$(shell id -g) -e HOME=/tmp \
	-e GOCACHE=/repo/.cache/go-build -e GOMODCACHE=/repo/.cache/mod -e GOFLAGS=-buildvcs=false \
	-v "$(REPO_DIR)":/repo:Z -w /repo $(GO_IMAGE)
endif

LDFLAGS := -s -w -X main.version=$(VERSION)
DEVICE := GOOS=linux GOARCH=arm64 CGO_ENABLED=0
PC := GOOS=linux GOARCH=amd64 CGO_ENABLED=0

.PHONY: all build deckd deck-update deckhost test check cover clean

all : check test build

# Device programs (arm64) and the PC program (amd64) into ./build
build : deckd deck-update deckhost

deckd deck-update :
	$(RUN) env $(DEVICE) go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/$@

deckhost :
	$(RUN) env $(PC) go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT)/$@ ./cmd/$@

# Unit tests with the race detector, and a coverage summary
test :
	mkdir -p $(OUT)
	$(RUN) sh -c 'go test -race -coverprofile=$(OUT)/cover.out ./... && go tool cover -func=$(OUT)/cover.out | tail -1'

# Per-function coverage of the last test run
cover :
	$(RUN) go tool cover -func=$(OUT)/cover.out

# Formatting and vet
check :
	$(RUN) sh -c 'f=$$(find . -name "*.go" -not -path "./.cache/*" | xargs gofmt -l); test -z "$$f" || { echo "$$f"; exit 1; }; go vet ./...'

clean :
	rm -rf ./$(OUT) ./.cache
