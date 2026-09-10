# CF-104 — Dead weight still tracked: internal/manifest, schema.RequiredLeaves, cmd/cf/serve.go:85 defaults, node_modules in module

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `#11` — `CF-104 — Dead weight still tracked: internal/manifest (808 lines, no importer), schema.RequiredLeaves, cmd/cf/serve.go:85 defaults, Go code under node_modules inside the module (reaches lint-strict/test-docker), make clean removing web/dist...` |
| **Worktree** | `.worktrees/CF-104` on branch `CF-104-dead-weight-cleanup` |
| **May write** | `internal/schema/tree.go`, `internal/schema/tree_test.go`, `cmd/cf/serve.go`, `cmd/cf/serve_test.go`, `Makefile`, `docs/research/2026-09-02-cf-dialect-specification.md` |
| **May delete** | `internal/manifest/` |
| **Merges after** | `nothing` |

## Symptom

Dead and unreachable code is still tracked and tested:
1. `internal/manifest`: 808 lines across 6 files with zero importers in the module.
2. `internal/schema/tree.go:167 RequiredLeaves` and `:173 appendRequiredLeaves`: unreachable func flagged by deadcode, tested only by `tree_test.go:485`.
3. `cmd/cf/serve.go:85 defaults`: test-only seam living in production file.
4. `Makefile`: `make clean` removes `web/dist` which does not exist; `lint-strict` and `test-docker` do not filter out Go code under `node_modules`.
5. `docs/research/2026-09-02-cf-dialect-specification.md:3`: Claims "authoritative reference ... Applies to: internal/emit, internal/manifest".

## Evidence

Documented in `docs/code-audit.md` §2 (X1–X4, X6):
- `grep -rln 'internal/manifest"' --include='*.go' .` -> no importers outside its own package.
- `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...`
- `go list ./... | grep node_modules` -> `github.com/koorikla/compositionfactory/node_modules/flatted/golang/pkg/flatted` reaches staticcheck and test-docker if not filtered.

## Location

- Delete directory `internal/manifest/`
- `internal/schema/tree.go`: remove `RequiredLeaves` and `appendRequiredLeaves`
- `internal/schema/tree_test.go`: remove test calling `RequiredLeaves`
- `cmd/cf/serve.go`: move `defaults` struct/helper to `cmd/cf/serve_test.go`
- `Makefile`:
  - Update `lint-strict:` to filter out `node_modules`: `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 $(go list ./... | grep -v /node_modules/)`
  - Update `test-docker:` to filter out `node_modules`: `go test $(go list ./... | grep -v /node_modules/) -run Acceptance -v -count=1`
  - Remove `web/dist` from `clean:`
- `docs/research/2026-09-02-cf-dialect-specification.md`: update status to note `internal/manifest` was removed.

## Acceptance test

Acceptance test: none — cleanup.
Verify that `make lint`, `make lint-strict`, and `make test-race` all pass cleanly with zero deadcode / staticcheck errors and no compilation failures.

## Contract

1. Completely remove `internal/manifest/`.
2. Remove `RequiredLeaves` from `internal/schema/tree.go` and its unit test.
3. Move `defaults` out of `cmd/cf/serve.go` into `cmd/cf/serve_test.go`.
4. Ensure `make lint`, `make lint-strict`, and `make test-race` stay 100% green.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

Deleting other packages or refactoring working emission/validation logic.

## Handover

Branch `CF-104-dead-weight-cleanup`, committed, not pushed, not merged. In your final report: list all removed files/symbols and outputs of `make lint` and `make lint-strict`.
