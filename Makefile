.PHONY: build test test-race test-docker test-e2e lint lint-strict serve clean cluster cluster-down deploy undeploy test-cluster

BIN       := bin/cf
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BLUEPRINT ?= testdata/xqueue.cf.yaml
OUT       ?= out
STATICCHECK := v0.8.1

build:
	go build -buildvcs=false -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/cf

# Lane A: no Docker, no cluster. Must pass anywhere.
test:
	go test ./... -short -count=1

test-race:
	go test ./... -short -race -count=1

# Lane B: needs a Docker daemon and the crossplane CLI on PATH.
test-docker:
	go test ./... -run Acceptance -v -count=1

# Playwright behavior suite over web-proto/. Boots its own isolated engine
# on a workspace-derived port with a scratch blueprint (see playwright.config.js).
test-e2e:
	npx playwright test

# Lane C: in-cluster verification using kind, Crossplane, and workspace isolation.
test-cluster: build
	./scripts/cluster/test-cluster.sh

cluster:
	./scripts/cluster/cluster.sh

cluster-down:
	./scripts/cluster/cluster-down.sh

deploy:
	@eval $$(./scripts/cluster/workspace.sh env); \
	echo "Deploying to namespace $$WORKSPACE_NAMESPACE..."; \
	skaffold run --namespace "$$WORKSPACE_NAMESPACE"

undeploy:
	@eval $$(./scripts/cluster/workspace.sh env); \
	echo "Deleting namespace $$WORKSPACE_NAMESPACE..."; \
	kubectl delete namespace "$$WORKSPACE_NAMESPACE" --timeout=60s || true

# gofmt is pointed at the tracked files rather than `.` so that agent
# worktrees under .worktrees/ and .claude/worktrees/ -- other branches'
# checkouts, ten times as many .go files as this tree owns -- cannot fail the
# lint of the tree you are actually editing.
lint:
	@unformatted="$$(gofmt -l $$(git ls-files '*.go'))"; \
		if [ -n "$$unformatted" ]; then echo "$$unformatted" >&2; exit 1; fi
	go vet ./...

# Deeper analysis than vet: staticcheck's default check set, configured in
# staticcheck.conf. Pinned and `go run` so it needs no separate install and
# cannot drift between a developer's machine and CI.
lint-strict:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK) ./...

# BLUEPRINT and OUT are overridable: make serve BLUEPRINT=path/to/other.cf.yaml
serve: build
	./$(BIN) serve --blueprint $(BLUEPRINT) --out $(OUT)

clean:
	rm -rf bin $(OUT) .testrun* .demorun* test-results playwright-report web/dist

