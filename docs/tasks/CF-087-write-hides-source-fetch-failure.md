# CF-087 — A document write whose declared source cannot be fetched answers 200 and reports the failure only on the server's stderr

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: an API contract an agent cannot use safely — the write claims success) |
| **Closes** | `CF-087 — PUT /api/blueprint, POST /api/examples/{id}/load and every other write that declares a source the server cannot fetch return 200 and warn only on stderr; the caller learns of it from the next generate's 400. [V]` |
| **Worktree** | `.worktrees/CF-087` on branch `CF-087-write-hides-source-fetch-failure` |
| **May write** | `internal/api/blueprint.go`, `internal/api/examples.go`, `internal/api/cf087_source_fetch_failure_test.go` (new), `web-proto/js/store.js`, `web-proto/js/regions/output.js`, `web-proto/js/regions/palette.js` (only if the Sources tab must show the state), `tests/` (new spec only) |
| **Merges after** | nothing. **CF-088 merges after this** — both touch `internal/api/blueprint.go`. |

## Symptom

A user saves a blueprint that declares a provider the server does not have cached, on a
machine that cannot reach the registry (the published container has no docker credential
helper; laptops go offline; ghcr rate-limits). The save succeeds, the file on disk now
declares the source, the canvas shows nothing wrong. The only record is a line on the
server's stderr:

```
cf: warning: unable to fetch source "…": … — continuing offline
```

In the container that line is in `docker logs`, which the canvas user never opens. The
next generate answers 400 with `provider "…" is not in the cache; run: cf provider add …`
and the top bar turns to `error` — attributed to generation, not to the save that
caused it. An MCP or HTTP client (an agent) has no way to tell a good write from one that
left the document unbuildable.

## Evidence

Scratch server, empty cache, `PATH` without the docker credential helper, `bin/cf` at `f45c2a8`:

```sh
$ curl -s -o /dev/null -w '%{http_code}\n' -X PUT $U/api/blueprint -H 'content-type: application/json' -d @doc.json
200
$ curl -s $U/api/providers
{"providers":[]}
$ cat serve.log
cf: warning: unable to fetch source "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0": fetch "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0": error getting credentials - err: exec: "docker-credential-desktop": executable file not found in $PATH, out: `` — continuing offline
$ curl -s -X POST $U/api/generate -H 'content-type: application/json' -d '{"write":false}'
{"error":"provider \"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0\" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"}
```

Reproduced twice. The Go test below fails twice on `f45c2a8`.

## Location

- `internal/api/blueprint.go:556-560` — in `syncBlueprintSourcesLocked`, a fetch error is printed with `fmt.Fprintf(os.Stderr, …)` and `continue`d; the function returns nil.
- `internal/api/blueprint.go:622-636` — `persistBlueprint` treats a nil return as success and writes the file.
- `internal/api/examples.go:88-93` — `handleLoadExample` calls the same sync and then `persistBlueprint`; a failed fetch is likewise invisible in the 200 body.
- `internal/api/blueprint.go:132` `handlePutBlueprint`, the parameter/resource routes and adopt all end in `persistBlueprint`.
- Client side: `web-proto/js/store.js` treats any 2xx as a clean write; nothing in `web-proto/js/regions/output.js` reads a warning off a write response.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before you change any
production code. It is the definition of done; do not paraphrase it, do not weaken
an assertion to make it pass, and do not delete it if it turns out to be
inconvenient - if it is wrong, say so in the handover and stop.

```go
// internal/api/cf087_source_fetch_failure_test.go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-087 — a document write that declares a source the server cannot fetch
// must tell the caller so; today the failure goes to stderr only and the
// write reports success.
func TestCF087WriteReportsSourceFetchFailure(t *testing.T) {
	const missing = "ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0"
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return nil, fmt.Errorf("registry unreachable for %s", ref)
	})

	current := mustLoadBlueprint(t, o.Blueprint)
	updated := *current
	updated.Spec.Sources = append(append([]blueprint.Source(nil), current.Spec.Sources...), blueprint.Source{Provider: missing})
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	got := rec.Body.String()
	if !strings.Contains(got, missing) || !strings.Contains(got, "registry unreachable") {
		t.Fatalf("PUT answered %d without naming the source that could not be fetched:\n%s", rec.Code, got)
	}

	list := do(t, h, "GET", "/api/providers", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), missing) {
		t.Fatalf("GET /api/providers = %d %s; the unfetched source must not be listed as served", list.Code, list.Body)
	}
}
```

**Fails today with** (two runs, `f45c2a8`):

```
cf: warning: unable to fetch source "ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0": registry unreachable for ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0 — continuing offline
--- FAIL: TestCF087WriteReportsSourceFetchFailure (0.02s)
    cf087_source_fetch_failure_test.go:34: PUT answered 200 without naming the source that could not be fetched:
        {"apiVersion":"factory.crossplane.io/v1alpha1","kind":"Blueprint","metadata":{"name":"xqueue"},"spec":{"sources":[{"provider":"ghcr.io/x/provider-aws-sqs:v2.7.0"},{"provider":"ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0"}], …
FAIL
```

## Contract

- Every write route that declares sources (`PUT /api/blueprint`, the parameter and
  resource routes, `POST /api/examples/{id}/load`, adopt) answers in a way that names each
  source it could not load and the reason, verbatim from the fetcher. Whether the document
  is still persisted is your call — offline authoring is legitimate — but the answer must
  make the outcome unmistakable, and the same information must reach the MCP tools that
  bridge these routes (they share the handler, so it should come for free; verify).
- `GET /api/providers` never lists a source that was not loaded (already true; keep it).
- The canvas shows the warning at the moment of the save, attributed to the save, with the
  fetch reason. It does not wait for the next generate to fail.
- The stderr line may stay; it is not the contract.
- Byte output of generation is unaffected.

## Verification

```sh
make lint && make lint-strict && make test-race
rm -rf .testrun* test-results && make test-e2e     # you touched web-proto/
```

## Out of scope

- Loading a declared-but-uncached source on demand at open time, and the wording of the
  `cf provider add` instruction — CF-088.
- The stale Sources tab — CF-086.

## Handover

Branch `CF-087-write-hides-source-fetch-failure`, committed, not pushed, not merged. In
your final report: the failing run and the passing run of the acceptance test, both
pasted; every gate you ran; every judgement call you made where the brief was silent.
