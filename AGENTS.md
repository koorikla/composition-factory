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
- **Port 8090**: UX tester canvas (`.claude/skills/canvas-ux-tester`), seeded from scratch dir `.testrun-ux`. One tester at a time — the engine holds a single live document, so a second tester on the same port corrupts both runs.
- **Dynamic Worktree Port (18000–27999)**: Automated Playwright e2e test suite (`make test-e2e`, managed via `playwright.config.js` and `tests/helpers.js` hashing the git worktree path; overridable via `CF_E2E_PORT`). The engine runs against its own scratch cache (`.testrun-<hash>/cache`), leaving `~/Library/Caches/compositionfactory` untouched.
- **Dynamic Demo Port (28000–37999)**: Headless demo GIF recorder instance (`scripts/record-demos/`; overridable via `CF_DEMO_PORT`).
- **Cluster Namespace & Group Isolation**: When running in a shared kind cluster, each workspace uses namespace `cf-<slug>` and appends `--group-suffix=w<hash>.cf-test` to XRD groups (`platform.w<hash>.cf-test`) to prevent cluster-scoped CRD/XRD collisions. The group suffix carries only the 6-char path hash, not the full slug: Crossplane copies the Composition name into a CompositionRevision *label*, and label values cap at 63 characters, so a longer suffix silently stops revisions (and therefore all composition) from being created.
- **Machine Lock Pools** (`scripts/driver/lock.sh`): `claim` (1 slot, held by `scripts/driver/claim.sh`), `merge` (1 slot, held by `scripts/driver/land.sh` through CI and any revert) and `gate` (`GATE_SLOTS`, default 3, held by `make test-race`, `make test-e2e` and `make test-docker`). Slot files live under `$(git rev-parse --git-common-dir)/cf-locks/` — one set per clone, shared by all of its worktrees; a separate clone does not coordinate with this one. The kernel holds a slot while the command or any descendant keeps its file open and frees it on exit, `kill -9` included; `lsof <slot file>` names a holder, and a leftover `cf serve` from an interrupted `make test-e2e` keeps its gate slot until it is stopped. Never delete a slot file: a recreated file is a new lock. `GATE_SLOTS` is a make variable (also read from the environment); callers passing different values get the largest of them. `CF_GATE_SLOTS=off` bypasses the gate pool only, never `claim` or `merge`, and is for humans only — never drivers or subagents. Without `lockf` every pool runs unlocked with a warning; that is for Linux CI only, and drivers run where `lockf` exists.

Never run test suites or recording harnesses against port 8080.

---

## 3. Make Targets & Verification Loop

The standard developer and CI workflows are encapsulated in `Makefile`:

- `make build`: Compile `bin/cf` with git version ldflags.
- `make test`: Fast unit tests (`go test ./... -short -count=1`).
- `make test-race`: Fast unit tests with race detector enabled (`go test ./... -short -race -count=1`). Waits for a `gate` slot (§2).
- `make test-docker`: Acceptance tests requiring Docker and `crossplane` CLI (`go test ./... -run Acceptance -v -count=1`). Waits for a `gate` slot (§2).
- `make test-e2e`: Playwright browser test suite against workspace-isolated engine (`npx playwright test`). Waits for a `gate` slot (§2).
- `make test-driver`: Shell tests for `scripts/driver` (`lock.sh`, `claim.sh`, `land.sh`) against a fake `gh`; the `lock.sh` tests skip where `lockf` is absent.
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

- **The backlog is GitHub Issues** (`gh issue list --repo koorikla/composition-factory`).
  One issue per item, titled `CF-NNN — <one sentence>`; the `CF-NNN` id is permanent and
  never reused (next id = max over open+closed issue titles, `docs/tasks/`, and
  `docs/backlog-archive.md`, plus one). Labels carry the triage: `severity:P0..P3`,
  `scale:engine` or `scale:ux`, `verified` (re-verified by hand), `brief-ready` (a brief
  exists in `docs/tasks/`), `in-progress` (claimed), `handed-back` (branch pushed, handover
  on the issue, awaiting `land.sh`), `parked` (branch pushed but not landable yet; the issue
  says why), `lane:floci`. Filing an
  item is `.claude/skills/backlog-authoring/`. `BACKLOG.md` is a pointer, not a list;
  `docs/backlog-archive.md` is frozen history from before the migration.

- **Taking an item**: only through `scripts/driver/claim.sh`, which adds `in-progress` and
  posts the claim `taking — <branch> · driver <driver-id> · lease until <YYYY-MM-DDTHH:MMZ> ·
  files: <paths>` — a 120-minute lease, refused while a live claim or a handed-back branch
  holds one of those files. Work that stops without landing is parked, not unlabelled. Two
  agents on one issue is the collision this exists to prevent.

- **Task Briefs & The Execution Contract**:
  Work is dispatched as a brief in `docs/tasks/CF-NNN-<slug>.md`, linked from the issue and
  carrying the repro, the verbatim acceptance test, and the contract the implementation must
  satisfy. Any agent
  executing one — spawned by any tool — reads
  [`docs/task-execution-contract.md`](docs/task-execution-contract.md) first: it governs
  worktree isolation, port allocation, the test-first loop, the gates, and the handover.
  The authoring side of that loop is `.claude/skills/backlog-authoring/`.

- **One Merge at a Time**: Many drivers may run at once. They are described in
  [`docs/routines/issue-driver.md`](docs/routines/issue-driver.md) (claiming, subagents in
  parallel by disjoint file sets, landing, the global cap and shift reports on the pinned Driver
  log issue); the scheduled oracle that
  files and re-verifies issues (and audits Dependabot MR-s/PRs) is [`docs/routines/oracle.md`](docs/routines/oracle.md)
  (skill in `.claude/skills/oracle/`). Only `scripts/driver/land.sh` pushes `main`, one landing at a time: it holds the merge lock for its whole run, through the landing's CI and any revert. All other agents work in isolated topic branch worktrees, push only their own topic branch, and hand back on the issue.
- **Pre-Merge Synchronization**: `land.sh` fetches and pins `origin/main` before it rebases a branch; branch from `origin/main`, never from local `main`.
- **Post-Merge CI Check**: `scripts/driver/land.sh` watches the landing's `ci` run to completion and reverts the landing if it is red — a landing is not done until CI is green, and the driver acts on the result line `land.sh` prints. Two of the five jobs (`cluster`, `e2e`) exercise a real kind cluster and a browser and cannot be reproduced by unit tests, so a locally-green tree says nothing about them. If `e2e` is the only failed job, `land.sh` reruns it once before treating it as a regression: its canvas drag tests are flaky in CI. No other job, `acceptance` included, is ever rerun, and nothing is rerun twice. Subagents working in topic-branch worktrees are exempt — they hand back branches and the driver lands them through `land.sh`.
- **Test-First Backlog Ticking**: Never tick a backlog item without an automated test that fails without the change.
- **Closing a Backlog Item**: only the driver whose `land.sh` reported `LANDED` closes the issue,
  with a comment naming the landed commit and the test that guards it (`gh issue close <n>
  --comment "completed in <sha>; guarded by <test>"`). `land.sh` puts the issue reference
  (`(CF-NNN, #n)`) in the landed commit's subject. Do not close from a topic branch; do not close without a guarding test.
  Half-fixes are not closed: file the residue as a new id and close the original pointing at it.
- **No AI Attribution**: Commit messages and code comments must remain strictly professional and standard. Never add AI attribution tags (e.g., `Co-authored-by: Claude`, `Generated by AI`, etc.).

---

## 5. Git & Code Hygiene

- **Never Use Blanket Staging**:
  Never execute `git add -A` or `git add .` blindly. Explicitly review and stage only the specific files modified or created for the intended task.

- **Format Code Before Commit**:
  Always run `gofmt -w` (or `go fmt ./...`) on all modified Go files before committing. Keep formatting clean and idiomatic.

- **Preserve Documentation Integrity**:
  Maintain docstrings, architectural commentary, and inline explanation blocks. Do not strip unrelated comments or explanations when refactoring.
