# CF-118 — No test renders the seven starter examples

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `#14` — `CF-118 — No test renders the seven starter examples; CF-084 was closed by editing YAML and TestAllExamplesAreValidBlueprints only calls Validate().` |
| **Worktree** | `.worktrees/CF-118` on branch `CF-118-render-starter-examples` |
| **May write** | `internal/examples/render_test.go` |
| **Merges after** | `nothing` |

## Symptom

Starter examples can regress to failing real composition rendering or validation without any CI gate noticing, because existing unit tests in `internal/examples/examples_test.go` only parse and validate the blueprint structs in-memory (`Validate()`) rather than executing a full Crossplane render.

## Evidence

In `internal/examples/examples_test.go:9`:
```go
func TestAllExamplesAreValidBlueprints(t *testing.T) {
	exs := All()
...
			b, err := blueprint.Parse([]byte(ex.YAML))
			if err != nil {
				t.Fatalf("failed to parse blueprint YAML: %v", err)
			}
			if err := b.Validate(); err != nil {
				t.Fatalf("blueprint validation failed: %v", err)
			}
```
None of the seven starter examples (`irsa`, `rds-postgres`, `k8s-app`, `k8s-workload`, `k8s-cronjob`, `s3-bucket`, `sqs-queue`) are tested against full emission and render verification in the test suite.

## Location

Add `internal/examples/render_test.go` (or acceptance test in that package) gated behind acceptance test prerequisites (`docker` and `crossplane`). Use the existing acceptance testing pattern (e.g. `rendertest.Render` or `emit.Generate` + `emit.SampleXR` + `crossplane composition render`) to verify each of the starter examples.

## Acceptance test

Write this test in `internal/examples/render_test.go`:

```go
package examples_test

import (
	"testing"
	// ...
)

func TestAllStarterExamplesRender(t *testing.T) {
	// Table-driven test over all starter examples in internal/examples.
	// For each starter example:
	// 1. Skip if docker / crossplane CLI unavailable (using requireTool pattern).
	// 2. Generate artifacts (Composition, XRD, functions, ProviderConfig).
	// 3. Render composition with crossplane composition render using sample XR.
	// 4. Assert render succeeds and contains no "<no value>" or render errors.
}
```

**Fails today with:**
```
Test does not exist today.
```

## Contract

1. A table-driven test covers all starter examples returned by `examples.All()`.
2. The test executes the real render pipeline (or invokes the render helper) under `make test-docker`.
3. When prerequisites are missing, it gracefully skips unless `CF_REQUIRE_ACCEPTANCE=1` is set.
4. All current starter examples must pass cleanly.

## Verification

```sh
make lint && make test-race
make test-docker
```

## Out of scope

Modifying the starter blueprints unless a demonstrable defect in their output is uncovered.

## Handover

Branch `CF-118-render-starter-examples`, committed, not pushed, not merged. In your final report: the failing run (before adding the test or when asserting invalid input) and passing run of the test, and output of `make test-docker`.
