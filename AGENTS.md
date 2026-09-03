# Agent Guidelines for composition-factory

This document records the foundational architecture rules, testing loops, and code hygiene principles for all AI coding agents working on this repository.

---

## 1. Engine Truths

- **One Engine (`internal/emit`)**:
  All emission of Crossplane artifacts (XRD, Composition, functions.yaml, RBAC) is implemented strictly in `internal/emit`.
  The CLI (`cf gen`), HTTP API (`cf serve`), and MCP server (`cf mcp`) are thin bridges calling `internal/emit`. Never create duplicate or parallel emission logic. Every interface must generate 100% byte-identical, deterministic YAML.

- **Blueprint as Single Source of Truth**:
  The `factory.crossplane.io/v1alpha1` `Blueprint` document is the canonical intermediate representation (IR). Edits, parameter definitions, and resource wiring manipulate this document directly.

- **Strict CRD Schema Validation**:
  Provider CRD OpenAPI schemas (`spec.forProvider`) are authoritative. Field paths and types must match the schema. Invalid or unknown field paths must fail loudly with nearest-match suggestions rather than silently dropping fields at deploy time.

- **Reproducibility**:
  Given the same blueprint and provider versions (or `.cf.lock`), generation must always produce the exact same byte-for-byte outputs.

- **The Round-Trip Rule**:
  Anything cf generates must survive Kubernetes and come back. Apply it to a real cluster, read it back with `kubectl get <kind> -o yaml`, and cf must be able to import *that* — the server round-tripped form, not the file cf wrote. The API server defaults fields, reorders maps, injects `managedFields`/`creationTimestamp`/`uid`/`resourceVersion`/`status`, and prunes what its schema does not know. An importer exercised only against cf's own output has never met the version of the document that matters operationally. The acceptance form is `cf gen` → `kubectl apply` → `kubectl get -o yaml` → `cf import` → `cf gen` reproducing the original bytes, with server-added fields scrubbed and named in a loss report. Lane C (§3, `make test-cluster`) is where this is proven. An artifact that cannot make the trip is an emitter bug, not an exception for the importer to special-case.

---

## 2. Port Contract & Environment Isolation

To prevent concurrent processes and test runners from trampling each other or the developer's live workspace:

- **Port 8080**: Human developer default (`cf serve` with default `--addr 127.0.0.1:8080`).
- **Dynamic Worktree Port (18000–27999)**: Automated Playwright e2e test suite (`make test-e2e`, managed via `playwright.config.js` and `tests/helpers.js` hashing the git worktree path; overridable via `CF_E2E_PORT`).
- **Dynamic Demo Port (28000–37999)**: Headless demo GIF recorder instance (`scripts/record-demos/`; overridable via `CF_DEMO_PORT`).
- **Cluster Namespace & Group Isolation**: When running in a shared kind cluster, each workspace uses namespace `cf-<slug>` and appends `--group-suffix=w<hash>.cf-test` to XRD groups (`platform.w<hash>.cf-test`) to prevent cluster-scoped CRD/XRD collisions. The group suffix carries only the 6-char path hash, not the full slug: Crossplane copies the Composition name into a CompositionRevision *label*, and label values cap at 63 characters, so a longer suffix silently stops revisions (and therefore all composition) from being created.

Never run test suites or recording harnesses against port 8080.

---

## 3. Make Targets & Verification Loop

The standard developer and CI workflows are encapsulated in `Makefile`:

- `make build`: Compile `bin/cf` with git version ldflags.
- `make test`: Fast unit tests (`go test ./... -short -count=1`).
- `make test-race`: Fast unit tests with race detector enabled (`go test ./... -short -race -count=1`).
- `make test-docker`: Acceptance tests requiring Docker and `crossplane` CLI (`go test ./... -run Acceptance -v -count=1`).
- `make test-e2e`: Playwright browser test suite against workspace-isolated engine (`npx playwright test`).
- `make cluster`: Idempotently create local kind cluster with Crossplane and required functions.
- `make cluster-down`: Tear down local kind cluster.
- `make deploy`: Deploy canvas to workspace namespace in the kind cluster via Skaffold.
- `make undeploy`: Delete workspace namespace and resources from kind cluster.
- `make test-cluster`: Lane C in-cluster verification testing XRD, Composition, and Function reconciliation.
- `make lint`: Code formatting verification (`gofmt`, over tracked files only) and Go vet
  analysis (`go vet ./...`).
- `make lint-strict`: staticcheck over the whole module at the version pinned in the
  `Makefile`, configured by `staticcheck.conf`. CI runs it alongside `make lint`;
  it must be clean before a merge.
- `make serve`: Launch local visual canvas server (`./bin/cf serve --blueprint $(BLUEPRINT) --out $(OUT)`).
- `make clean`: Clean up build artifacts and test outputs (`bin`, `out`, `.testrun*`, `.demorun*`, `test-results`, `playwright-report`).

---

## 4. Multi-Agent & Branch Merging Workflow

To prevent regressions and collisions between concurrent automation agents:

- **One-Driver Rule**: Exactly one driver merges to `main`. All other agents work in isolated topic branch worktrees and hand over PRs or clean branches.
- **Pre-Merge Synchronization**: Always run `git fetch && git log main..origin/main` before any merge to ensure no stale assumptions.
- **Post-Merge CI Check**: The driver that merges to `main` must watch the resulting `ci` run to completion (`gh run watch <id> --exit-status`) and fix or revert what it broke — a merge is not done until CI is green. Two of the five jobs (`cluster`, `e2e`) exercise a real kind cluster and a browser and cannot be reproduced by unit tests, so a locally-green tree says nothing about them. If `e2e` fails, rerun it once before treating it as a regression: its canvas drag tests are flaky in CI. Subagents working in topic-branch worktrees are exempt — they hand over branches and the driver owns the merge and its CI.
- **Test-First Backlog Ticking**: Never tick a backlog item without an automated test that fails without the change.
- **Closing a Backlog Item**: `BACKLOG.md` holds open work only — it is read into every agent's
  context, so its length is a running cost. When an item is done, move it (with its original
  wording and a `— completed <date>` note) into `docs/backlog-archive.md`; do not leave an `[x]`
  behind in `BACKLOG.md`.
- **No AI Attribution**: Commit messages and code comments must remain strictly professional and standard. Never add AI attribution tags (e.g., `Co-authored-by: Claude`, `Generated by AI`, etc.).

---

## 5. Git & Code Hygiene

- **Never Use Blanket Staging**:
  Never execute `git add -A` or `git add .` blindly. Explicitly review and stage only the specific files modified or created for the intended task.

- **Format Code Before Commit**:
  Always run `gofmt -w` (or `go fmt ./...`) on all modified Go files before committing. Keep formatting clean and idiomatic.

- **Preserve Documentation Integrity**:
  Maintain docstrings, architectural commentary, and inline explanation blocks. Do not strip unrelated comments or explanations when refactoring.
