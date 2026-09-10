# CF-129 — After one failed source fetch, every later write answers with the bare document and never mentions that the declared source is still unloaded

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: source-only knowledge / unsafe API contract — second write silently reports success while sources are missing) |
| **Closes** | `CF-129 — *(engine)* After one failed source fetch, every later write answers with the bare document and never mentions that the declared source is still unloaded. [V]` |
| **Worktree** | `.worktrees/CF-129` on branch `CF-129-failed-source-memo-retry` |
| **May write** | `internal/api/blueprint.go`, `internal/api/cf129_source_fetch_retry_test.go` (new) |
| **Merges after** | nothing |

## Symptom

When a document write (`PUT /api/blueprint`, `POST /api/blueprint/resources`, etc.) declares a provider source that cannot be fetched (e.g. offline, registry down, or invalid ref), the first write reports a 400 with `failed to sync sources: unable to fetch source "…"`.
However, because `syncBlueprintSourcesLocked` records the failed ref into `srv.failedSources[ref] = err` and lines 660-662 skip any provider present in `srv.failedSources`:
```go
if srv.failedSources != nil && srv.failedSources[s.Provider] != nil {
    continue
}
```
the second and every subsequent write of the same document skips attempting to fetch the source, collects zero `fetchErrs`, and returns HTTP 200 with the document as if all sources were successfully served, even though `GET /api/providers` still does not serve the provider.

A write must report an unloaded declared source every time until it loads, and the memo must not suppress a retry the caller asks for on an explicit write.

## Evidence

In `internal/api/blueprint.go:660-662`:
`srv.failedSources` was added in CF-088 to avoid hammering the network on repeated read requests (`GET /api/kinds`, `GET /api/providers`, `POST /api/generate`).
However, on mutating writes (`syncBlueprintSourcesLocked` called by `persistBlueprint` or `handlePutBlueprint`), skipping failed sources causes later writes to pretend the declared source loaded fine, discarding the error.

Furthermore, when the user explicitly saves or updates a blueprint, they are asking to reconcile sources. Suppressing the fetch on an explicit write prevents retrying after network recovery or credentials configuration.

## Acceptance Test

Write this test **first**, verbatim in `internal/api/cf129_source_fetch_retry_test.go`, and watch it fail before changing production code:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-129 — After one failed source fetch, subsequent writes must not silently
// succeed without reporting that the declared source is still unloaded, and
// must retry fetching when requested.
func TestCF129SubsequentWriteReportsUnloadedSourceAndRetries(t *testing.T) {
	const missing = "ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0"
	var fetchAttempts int32

	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		atomic.AddInt32(&fetchAttempts, 1)
		return nil, fmt.Errorf("registry unreachable for %s", ref)
	})

	current := mustLoadBlueprint(t, o.Blueprint)
	updated := *current
	updated.Spec.Sources = append(append([]blueprint.Source(nil), current.Spec.Sources...), blueprint.Source{Provider: missing})
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// First write fails as expected
	rec1 := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec1.Code != http.StatusBadRequest || !strings.Contains(rec1.Body.String(), "registry unreachable") {
		t.Fatalf("first PUT expected 400 with fetch error, got %d:\n%s", rec1.Code, rec1.Body.String())
	}

	// Second write with the same document must also report the fetch error, not 200 OK
	rec2 := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("second PUT expected 400 reporting unloaded source, got %d:\n%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), missing) || !strings.Contains(rec2.Body.String(), "registry unreachable") {
		t.Fatalf("second PUT body missing source failure detail:\n%s", rec2.Body.String())
	}

	// Verify that a retry was actually attempted on the second write
	if attempts := atomic.LoadInt32(&fetchAttempts); attempts < 2 {
		t.Fatalf("expected at least 2 fetch attempts across 2 PUTs, got %d", attempts)
	}
}
```

## Contract

- On explicit blueprint writes (`syncBlueprintSourcesLocked`), declared sources that are not yet in `srv.Providers` (or store) must not be suppressed by `srv.failedSources`. An explicit write must retry fetching the source.
- Every write that declares an uncached/unfetchable source must return an error naming the unloaded source, every time, until it successfully loads.
- If a fetch fails during a write, `srv.failedSources[ref]` may record the error for read-time suppression, but future write requests must attempt to fetch again. If the fetch succeeds on retry, the ref is cleared from `srv.failedSources` and added to `srv.Providers`.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- UI toast styling.
- Changing read-time background suppression (`ensureBlueprintSourcesLoadedLocked`).

## Handover

Branch `CF-129-failed-source-memo-retry`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
