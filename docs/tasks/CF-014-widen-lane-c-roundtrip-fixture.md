# CF-014 — The Lane C round-trip gate runs on the one blueprint shape that cannot fail it

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-014 — Widen the Lane C round-trip fixture to a blueprint with a managed resource, an atProvider status wire, an envelope, a forEach, and spec.environment. Do this first — it turns most of the items below into failing tests instead of prose.` |
| **Worktree** | `.worktrees/CF-014` on branch `CF-014-roundtrip-fixture` |
| **May write** | `internal/examples/testdata/roundtrip-full.cf.yaml`, `internal/examples/roundtrip_fixture_test.go`, `scripts/cluster/test-cluster.sh` |
| **Merges after** | nothing — this goes first |

## Symptom

`make test-cluster` is green, and has been green throughout. It proves nothing about
the adopt/round-trip losses filed under CF-015…CF-022 (several fixed on 2026-09-03;
CF-020 and CF-022 were still open when this brief was written — check), because the only blueprint it carries
through the API server is `internal/examples/k8s-workload.cf.yaml` — native kinds
wired by `metadata.name`, and nothing else. No managed resource, no `atProvider`
status wire, no envelope, no `forEach`, no `spec.environment`.

Those are exactly the five shapes that get lost. A real gate over a fixture that
cannot fail reads, from CI, identically to a passing gate.

## Evidence

The gate's own script names its fixture once, and the round-trip leg reads back what
that fixture produced:

```sh
$ grep -n 'examples/\|cf import\|kubectl get xrd\|kubectl get composition' scripts/cluster/test-cluster.sh
22:./bin/cf gen internal/examples/k8s-workload.cf.yaml --out "${OUT_DIR}" --group-suffix="${WORKSPACE_GROUP_SUFFIX}"
142:kubectl get xrd "${XRD_NAME}" -o yaml > "${LIVE_TREE}/apis/definition.yaml"
143:kubectl get composition "${COMP_NAME}" -o yaml > "${LIVE_TREE}/composition.yaml"
147:./bin/cf import "${LIVE_TREE}" -o "${ROUNDTRIP_BP}"
```

And no example in the tree carries the five features together:

```sh
$ for f in internal/examples/*.cf.yaml; do echo -n "$f: "; \
    grep -o "environment:\|forEach\|atProvider\|writeConnectionSecret" $f | sort -u | tr '\n' ' '; echo; done
internal/examples/irsa.cf.yaml: atProvider
internal/examples/k8s-app.cf.yaml: atProvider writeConnectionSecret
internal/examples/k8s-cronjob.cf.yaml:
internal/examples/k8s-workload.cf.yaml:
internal/examples/rds.cf.yaml: writeConnectionSecret
internal/examples/s3-bucket.cf.yaml: atProvider
internal/examples/sqs-queue.cf.yaml: atProvider
```

`forEach` and `spec.environment` appear in no example at all. Verified twice on
`3a3fef4`.

## Location

- `scripts/cluster/test-cluster.sh:22` — the single `cf gen` that seeds the whole
  lane, and `:142-151`, the round-trip leg that reads the XRD and Composition back
  out of the API server, imports them, and regenerates.
- `internal/examples/` — seven blueprints, none with the coverage this gate needs.

## Acceptance test

Write this first, verbatim, and watch it fail before you author the fixture.

```go
// internal/examples/roundtrip_fixture_test.go
package examples

import (
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// The Lane C round-trip gate is only as strong as the blueprint it carries through
// the API server. This pins the feature coverage of that fixture, so a later edit
// cannot quietly narrow it back to a shape that round-trips trivially.
func TestRoundTripFixtureCoversTheLossyFeatures(t *testing.T) {
	data, err := os.ReadFile("testdata/roundtrip-full.cf.yaml")
	if err != nil {
		t.Fatalf("read round-trip fixture: %v", err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	var managed, statusWire, envelope, forEach int
	for _, r := range b.Spec.Resources {
		if r.Provider != blueprint.NativeProvider {
			managed++
			envelope += len(r.Envelope)
		}
		if r.ForEach != "" {
			forEach++
		}
		for _, f := range r.Fields {
			if strings.Contains(f.From, ".status.atProvider.") {
				statusWire++
			}
		}
	}

	for _, c := range []struct {
		name string
		got  int
	}{
		{"managed resources", managed},
		{"atProvider status wires", statusWire},
		{"envelope entries on a managed resource", envelope},
		{"forEach resources", forEach},
		{"spec.environment keys", len(b.Spec.Environment)},
	} {
		if c.got == 0 {
			t.Errorf("round-trip fixture has no %s; the gate cannot fail on that loss", c.name)
		}
	}
}
```

**Fails today with:**

```
--- FAIL: TestRoundTripFixtureCoversTheLossyFeatures (0.00s)
    roundtrip_fixture_test.go:18: read round-trip fixture: open testdata/roundtrip-full.cf.yaml: no such file or directory
FAIL
```

Run on `3212106`.

## Contract

1. A new fixture at `internal/examples/testdata/roundtrip-full.cf.yaml` carrying all
   five shapes at once. It lives in `testdata/`, not beside the starter examples,
   because it is a gate fixture: it must not be embedded by `examples.go`, must not
   appear in the canvas Examples menu, and must not be held to the curation bar
   `TestAllExamplesAreValidBlueprints` applies.
2. `scripts/cluster/test-cluster.sh` round-trips **that** fixture. Keep
   `k8s-workload.cf.yaml` driving the reconcile leg — the existing assertions that a
   Deployment and Service actually come up are real coverage and must not be
   weakened. The two legs may need two `cf gen` invocations and two output trees.
3. The round-trip leg must fail loudly on a loss: `cf gen` → apply → `kubectl get -o
   yaml` → `cf import` → `cf gen` reproducing the original bytes, with server-added
   fields scrubbed. If it now fails, **that is the point** — do not weaken the
   comparison to get green. Report which of CF-015…CF-022 it caught and stop; those
   are separate tasks with their own briefs. Some of that set has been fixed since
   this brief was written; a loss the gate no longer catches is a result to report,
   not a defect to re-file.
4. Group-suffix isolation (`AGENTS.md` §2) applies to the new fixture exactly as to
   the old one. Keep the suffix inside the 63-character label ceiling.

**Judgement call flagged, not verified:** applying a Composition that references AWS
kinds should not require `provider-aws` in the kind cluster — the composed kinds live
inside go-templating input as strings, and the round-trip leg reads back only the XRD
and the Composition, never a composed managed resource. If the API server rejects it
anyway, report that rather than dropping the managed resource; a native-only fixture
reproduces the defect this task exists to fix.

## Verification

```sh
make lint && make lint-strict && make test-race
go test ./internal/examples/ -run TestRoundTripFixture -v
make test-cluster        # the gate itself — needs the kind cluster up
```

Do not run `make cluster-down`. Another agent is probably using that cluster.

## Out of scope

Fixing any loss the widened gate exposes. CF-015 through CF-022 each have their own
brief. Adding the fixture to `examples.go`, `All()`, or the canvas. Changing the
reconcile leg's assertions.

## Handover

Branch `CF-014-roundtrip-fixture`, committed, not pushed, not merged. Paste the
failing and passing runs of the acceptance test, the full `make test-cluster` output,
and a list of every loss the widened round-trip surfaced — with the exact diff line
that surfaced it, so each can be filed as its own item.
