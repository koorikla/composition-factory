# CF-118 — No test renders the seven starter examples

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `#14` — `CF-118 — No test renders the seven starter examples; CF-084 was closed by editing YAML and TestAllExamplesAreValidBlueprints only calls Validate().` |
| **Worktree** | `.worktrees/CF-118` on branch `CF-118-render-starter-examples` |
| **May write** | `acceptance_test.go` |
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

In `acceptance_test.go`:
1. In `TestMain`, add the remaining starter example provider schemas to the pre-cached `providers` slice:
   - `ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0`
   - `ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0`
2. Add `TestAcceptanceAllStarterExamplesRender(t *testing.T)` table-driven across all starter examples from `examples.All()`.
3. Use the established `acceptance_test.go` pattern (`testBin`, `testCacheDir`, `testLockFile`, `renderComposition`) to generate and render each starter example, asserting successful rendering with no `<no value>` or `<nil>`.

## Acceptance test

Write this test in `acceptance_test.go`:

```go
func TestAcceptanceAllStarterExamplesRender(t *testing.T) {
	if testing.Short() {
		unavailable(t, "acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	for _, ex := range examples.All() {
		t.Run(ex.ID, func(t *testing.T) {
			// Write ex.YAML to temp blueprint file
			// Run testBin gen with --cache-dir testCacheDir --lock testLockFile
			// Render with crossplane composition render
			// Assert exit 0 and no "<no value>"
		})
	}
}
```

## Contract

1. A table-driven acceptance test covers all starter examples returned by `examples.All()`.
2. The test executes within `acceptance_test.go` and runs under `make test-docker`.
3. When prerequisites are missing, it gracefully skips unless `CF_REQUIRE_ACCEPTANCE=1` is set.
4. All current starter examples must pass cleanly.

## Verification

```sh
make lint && make test-race
make test-docker
```

## Out of scope

Modifying the starter blueprints unless an actual render defect is uncovered.

## Handover

Branch `CF-118-render-starter-examples`, committed, not pushed, not merged. In your final report: the passing run of `make test-docker` and every gate run.
