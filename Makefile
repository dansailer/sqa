# sqa build helpers. Release builds embed the official sonar CLI
# (linux/amd64 only) with the bundled_sonar build tag.

GO          ?= go
VERSION     ?= dev
COMMIT      ?= $(shell git rev-parse HEAD 2>/dev/null)
SONAR_BIN   := internal/sonarcli/bundled/sonar-linux-amd64
LDFLAGS     := -s -w -X github.com/dansailer/sqa/internal/cli.version=$(VERSION) -X github.com/dansailer/sqa/internal/cli.commit=$(COMMIT)
BUILD_ENV   := CGO_ENABLED=0 GOOS=linux GOARCH=amd64

.PHONY: all build build-bundled fetch-sonar sonar-version test test-bundled lint clean

all: test build

## build: sqa without a bundled sonar (uses sonar.path, `sqa setup` or PATH)
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o sqa ./cmd/sqa

## fetch-sonar: download the pinned sonar for linux/amd64 and verify its SHA-256
fetch-sonar:
	$(GO) run ./tools/fetch-sonar -o $(SONAR_BIN)

## sonar-version: print the pinned sonar version
sonar-version:
	@$(GO) run ./tools/fetch-sonar -print-version

## build-bundled: linux/amd64 sqa with the official sonar embedded
build-bundled: fetch-sonar
	$(BUILD_ENV) $(GO) build -tags bundled_sonar -trimpath -ldflags "$(LDFLAGS)" -o sqa ./cmd/sqa
	@ls -l sqa

test:
	$(GO) test -race ./...

## test-bundled: tests including the embedded-binary check
test-bundled: fetch-sonar
	$(GO) test -race -tags bundled_sonar ./...

lint:
	$(GO) vet ./...
	golangci-lint run

clean:
	rm -f sqa $(SONAR_BIN)
