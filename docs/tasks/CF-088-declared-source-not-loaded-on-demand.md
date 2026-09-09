# CF-088 — `cf serve` promises an uncached declared source will "load on demand", but nothing loads it until a write; the fresh canvas opens on a red error prescribing a CLI command

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (UX scale: completable only with knowledge from outside the canvas; impossible in the published container without `docker exec`) |
| **Closes** | `CF-088 — Opening a blueprint whose declared source is not in the cache lands on a red generate error telling the user to run cf provider add; the startup log promised the schema would load on demand, and nothing does until a write happens. [V]` |
| **Worktree** | `.worktrees/CF-088` on branch `CF-088-declared-source-not-loaded-on-demand` |
| **May write** | `cmd/cf/options.go`, `internal/api/blueprint.go`, `internal/api/server.go`, `internal/api/generate.go`, `internal/cache/store.go` (the "run: cf provider add" text only), `internal/api/cf088_on_demand_source_test.go` (new), `web-proto/js/regions/output.js` (banner wording only), `tests/` (new spec only) |
| **Merges after** | `CF-087` (shared `internal/api/blueprint.go`) |

## Symptom

This is the user's "fresh pod: instant render error". Start the published image, or any
`cf serve`, against a blueprint that declares a provider the cache does not have (the
deployment's init container caches iam/rds/sqs but the starters also use s3 and
provider-kubernetes; a fresh volume caches nothing). The server prints

```
cf: warning: provider "…" is not in the cache; run: cf provider add … — continuing without it; schemas load on demand
```

and comes up. The canvas opens, the first generate runs, and the user sees the top bar
say `error` with a red banner:

```
provider "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
```

Nothing loaded on demand: reads never trigger `syncBlueprintSourcesLocked`, only writes
do. The banner tells a browser user to run a CLI they may not have (the container has no
shell access from the canvas). The canvas's own SOURCES → catalogue → Add path would
repair it, and the banner does not mention it. The KINDS rail shows only native kinds, so
the cards on the canvas have no schema behind them and the inspector cannot validate them.

## Evidence

`bin/cf` at `f45c2a8`, scratch dir, empty `--cache-dir`, blueprint = `internal/examples/sqs-queue.cf.yaml`:

```sh
$ cat serve.log
cf: warning: provider "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0 — continuing without it; schemas load on demand
cf serve: listening on http://127.0.0.1:19003
$ curl -s $U/api/providers
{"providers":[]}
$ curl -s -X POST $U/api/generate -H 'content-type: application/json' -d '{"write":false}'
{"error":"provider \"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0\" is not in the cache; run: cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"}
```

Browser on the same server: top bar `error`, red banner with the text above, console
`[API ERROR] 400 /api/generate …`. Reproduced twice (scratch server and the browser pane).
The Go test below fails twice on `f45c2a8`.

## Location

- `cmd/cf/options.go:33-41` — a missing cache entry is skipped with the "schemas load on demand" warning; the ref is left out of `Options.Providers`.
- `internal/api/blueprint.go:507-509`, `:509-615` — `syncBlueprintSourcesLocked` is the only code that fetches a declared source, and its callers are `persistBlueprint` (`:622`) and `handleLoadExample` (`examples.go:88`) — writes only.
- `internal/cache/store.go` — `Load` composes the `is not in the cache; run: cf provider add …` text that `POST /api/generate` forwards verbatim.
- `web-proto/js/regions/output.js:1050` — the `"error"` subscription renders the server text as the banner without interpretation.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before you change any
production code. It is the definition of done; do not paraphrase it, do not weaken
an assertion to make it pass, and do not delete it if it turns out to be
inconvenient - if it is wrong, say so in the handover and stop.

```go
// internal/api/cf088_on_demand_source_test.go
package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-088 — `cf serve` starts with a warning that an uncached declared source
// will "load on demand", but nothing loads it until a write happens: reads
// and generation on the freshly opened document fail with a CLI instruction.
func TestCF088DeclaredSourceLoadsOnDemand(t *testing.T) {
	store := cache.New(t.TempDir()) // empty: the declared provider is not cached
	idx, err := BuildIndex(store, nil, nil, "")
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	o := Options{
		Index:     idx,
		Store:     store,
		Blueprint: testBlueprintPath(t), // declares ghcr.io/x/provider-aws-sqs:v2.7.0
		OutDir:    t.TempDir(),
		Lock:      t.TempDir() + "/.cf.lock",
	}
	o.fetch = func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:ondemand", Docs: [][]byte{
			managedCRDDoc("sqs.aws.m.upbound.io", "Queue", "queues"),
		}}, nil
	}
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	kinds := do(t, h, "GET", "/api/kinds", "")
	if kinds.Code != http.StatusOK || !strings.Contains(kinds.Body.String(), `"Queue"`) {
		t.Errorf("GET /api/kinds = %d; the declared source's kinds are not served:\n%s", kinds.Code, kinds.Body)
	}
	gen := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if gen.Code != http.StatusOK {
		t.Errorf("POST /api/generate = %d: %s", gen.Code, gen.Body)
	}
	list := do(t, h, "GET", "/api/providers", "")
	if !strings.Contains(list.Body.String(), "provider-aws-sqs") {
		t.Errorf("GET /api/providers does not list the declared source once it has been loaded: %s", list.Body)
	}
}
```

**Fails today with** (two runs, `f45c2a8`):

```
--- FAIL: TestCF088DeclaredSourceLoadsOnDemand (0.02s)
    cf088_on_demand_source_test.go:40: GET /api/kinds = 200; the declared source's kinds are not served:
    cf088_on_demand_source_test.go:44: POST /api/generate = 400: {"error":"provider \"ghcr.io/x/provider-aws-sqs:v2.7.0\" is not in the cache; run: cf provider add ghcr.io/x/provider-aws-sqs:v2.7.0"}
    cf088_on_demand_source_test.go:48: GET /api/providers does not list the declared source once it has been loaded: {"providers":[]}
FAIL
```

## Contract

- A source the blueprint declares and the cache lacks is loaded the first time the
  server needs it after start — the first kinds read or the first generate — not only on
  a write. Once loaded it is listed by `GET /api/providers`, indexed, and pinned in the
  lock exactly as `POST /api/providers` would have done. Loading happens once; a failed
  attempt is not retried on every request in a tight loop.
- When it cannot be loaded, the error every route returns (and the banner the canvas
  shows) names the source, the fetch reason verbatim, and the repair available *where the
  user is*: in the canvas, the SOURCES tab's Add; on the CLI, `cf provider add`. The
  message must not send a browser user to a shell.
- The startup warning describes what actually happens.
- The MCP tools that bridge these routes get the same behaviour (shared handlers; verify).
- Generation output is byte-identical to today's once the schema is present.

## Verification

```sh
make lint && make lint-strict && make test-race
rm -rf .testrun* test-results && make test-e2e
```

## Out of scope

- The 200-with-stderr-warning on writes — CF-087, which lands first.
- Shipping the `crossplane` CLI in the container image so Validate works there (separate item).

## Handover

Branch `CF-088-declared-source-not-loaded-on-demand`, committed, not pushed, not merged.
In your final report: the failing run and the passing run of the acceptance test, both
pasted; every gate you ran; every judgement call you made where the brief was silent.
