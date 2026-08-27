.PHONY: test test-docker lint build

# Lane A: no Docker, no cluster. Must pass anywhere.
test:
	go test ./... -short

# Lane B: needs a Docker daemon.
test-docker:
	go test ./... -run Acceptance -v

build:
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/cf ./cmd/cf

lint:
	go vet ./...
	gofmt -l . | tee /dev/stderr | test -z "$$(cat)"
