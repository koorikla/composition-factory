.PHONY: build test test-race test-docker test-e2e lint serve dev clean

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

# Playwright behavior suite over web-proto/. It starts serve.py itself (see
# playwright.config.js) but `cf serve` must already be listening on :8080 --
# run `make serve` in another shell first (after one `provider add`).
test-e2e:
	npx playwright test

lint:
	gofmt -l . | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...

# BLUEPRINT and OUT are overridable: make serve BLUEPRINT=path/to/other.cf.yaml
serve: build
	./$(BIN) serve --blueprint $(BLUEPRINT) --out $(OUT)

dev:
	python3 web-proto/serve.py

clean:
	rm -rf bin $(OUT)
