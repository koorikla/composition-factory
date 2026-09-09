# CF-100 — Write-rollback paths ignore index rebuild errors

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: server internal state desynchronization on failed write) |
| **Closes** | `CF-100 — *(engine)* The three write-rollback paths ignore the index rebuild's error, so a failed write can leave /api/kinds serving a provider the server no longer holds. [V]` |
| **Worktree** | `.worktrees/CF-100` on branch `CF-100-index-rebuild-rollback-error` |
| **May write** | `internal/api/blueprint.go`, `internal/api/blueprint_test.go` |
| **Merges after** | nothing |

## Symptom

When a blueprint write fails validation against CRDs or fails to write to disk, the server rolls back `srv.Providers = origProviders` and attempts to restore the index via `_ = srv.rebuildIndexLocked()`.
Because the error from `srv.rebuildIndexLocked()` is discarded with `_ =`:
If the index rebuild fails during rollback, `srv.Index` is not swapped and retains the newer state while `srv.Providers` is rolled back. `srv.Providers` and `srv.Index` disagree, and `/api/kinds` serves a provider the server no longer holds.

The rollback path must check the error from `rebuildIndexLocked()` and return an error indicating that index restoration failed if it cannot be restored.

## Acceptance Test

Write this test first in `internal/api/blueprint_test.go`:

```go
func TestCF100WriteRollbackReportsIndexRebuildError(t *testing.T) {
	// Verify that if rebuildIndexLocked fails during a failed write rollback,
	// the handler returns an internal server error reporting the rollback failure
	// rather than masking it.
}
```

## Contract

- In `internal/api/blueprint.go`, check the return value of `srv.rebuildIndexLocked()` on all rollback paths.
- If `rebuildIndexLocked()` returns an error during rollback, return HTTP 500 naming the rollback failure.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
go test ./internal/api -run TestCF100 -v
make lint
make lint-strict
make test-race
```

## Handover

Branch `CF-100-index-rebuild-rollback-error`, committed, not pushed, not merged.
