# Contributing to composition-factory

Thank you for contributing to **composition-factory**!

Composition Factory is a schema-aware generator and visual canvas for Crossplane v2 Compositions and CompositeResourceDefinitions (XRDs).

---

## 1. Prerequisites

- **Go**: Version 1.25 or later.
- **Node.js**: Version 18+ and npm (for Playwright browser e2e tests).
- **Docker** & **Crossplane CLI** (optional): Required for running end-to-end acceptance render tests (`make test-docker`).

---

## 2. Architecture & Codebase Layout

- **`internal/emit`**: The single emission engine. All Crossplane YAML artifacts (XRDs, Compositions, `functions.yaml`, ProviderConfigs, RBAC) are emitted here. CLI, HTTP API, and MCP tools are thin callers over this package.
- **`internal/blueprint`**: Blueprint (`factory.crossplane.io/v1alpha1`) parsing, validation, and mutation logic.
- **`internal/api`**: HTTP server implementation for `cf serve`.
- **`internal/mcp`**: Model Context Protocol stdio bridge for AI agents.
- **`internal/cache`**: Provider package schema cache and OCI CRD layer extraction.
- **`cmd/cf`**: CLI entrypoint and subcommand implementations (`gen`, `serve`, `package`, `push`, `adopt`, `provider`, `mcp`, `version`).
- **`web-proto/`**: Embedded visual canvas frontend built with native ES modules and pure DOM/SVG (no build step or bundling required).
- **`tests/`**: Playwright browser e2e test suite.

---

## 3. Development Workflow

### Quick Commands

```sh
make build        # Build bin/cf
make test         # Run unit tests
make test-race    # Run unit tests with Go race detector
make test-e2e     # Run Playwright browser e2e tests
make test-docker  # Run acceptance tests with Docker daemon and crossplane CLI
make lint         # Check formatting (gofmt) and vet (go vet)
make serve        # Launch visual canvas server on http://localhost:8080
make clean        # Clean build outputs and test artifacts
```

### Port Allocation Contract
- **Port 8080**: Human developer canvas (`make serve` / `cf serve`).
- **Port 8081**: Automated Playwright test runner (`make test-e2e`).
- **Port 8086**: Headless demo recorder (`scripts/record-demos/`).

*Do not run test suites against port 8080.*

---

## 4. Code Standards & Pull Request Guidelines

1. **One Engine Truth**: Never duplicate or fork emission logic outside `internal/emit`. All frontends (CLI, GUI, MCP) must generate 100% byte-identical YAML.
2. **Behavior-Driven Verification**: Every new feature or bugfix must be accompanied by automated tests:
   - Unit tests in the modified Go packages.
   - API contract tests in `internal/api/contract_fixtures_test.go` if HTTP API payloads change.
   - Playwright e2e specs in `tests/` if canvas UI behaviors are altered.
3. **Format Code**: Always run `gofmt -w` (or `make lint`) on all modified Go files before committing.
4. **Clean Commits**: Make atomic, descriptive commits without blanket staging (`git add -A`). Never include AI-attribution tags (e.g. `Co-authored-by: Claude`, etc.).
5. **Verify Before Submitting**: Run `make lint`, `make test-race`, and `make test-e2e` locally before opening a pull request.
