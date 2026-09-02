.PHONY: build test test-race test-docker test-e2e lint serve clean

BIN       := bin/cf
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BLUEPRINT ?= testdata/xqueue.cf.yaml
OUT       ?= out

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/cf

# Lane A: no Docker, no cluster. Must pass anywhere.
test:
	go test ./... -short -count=1

test-race:
	go test ./... -short -race -count=1

# Lane B: needs a Docker daemon and the crossplane CLI on PATH.
test-docker:
	go test ./... -run Acceptance -v -count=1

# Playwright behavior suite over web-proto/. Boots its own isolated engine
# on 127.0.0.1:8081 with a scratch blueprint (see playwright.config.js).
test-e2e:
	npx playwright test

lint:
	gofmt -l . | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...

# BLUEPRINT and OUT are overridable: make serve BLUEPRINT=path/to/other.cf.yaml
serve: build
	./$(BIN) serve --blueprint $(BLUEPRINT) --out $(OUT)

clean:
	rm -rf bin $(OUT) .testrun .demorun
