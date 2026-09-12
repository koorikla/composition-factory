# CF-318 — effectivePipeline drops function-environment-configs when unrelated step is named environment-configs

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: function-environment-configs omitted when unrelated pipeline step is named environment-configs) |
| **Closes** | `#207` — `CF-318 — effectivePipeline drops function-environment-configs when unrelated step is named environment-configs` |
| **Worktree** | `.worktrees/CF-318` on branch `CF-318-effective-pipeline-unrelated-step-name` |
| **May write** | `internal/emit/pipeline.go`, `internal/emit/pipeline_test.go` |
| **Merges after** | `nothing` |

## Symptom

When a blueprint references environment variables (`env.<key>`), `effectivePipeline` is responsible for injecting `function-environment-configs` into the pipeline and `functions.yaml`.
However, if a user has defined a pipeline step whose `Name` is `"environment-configs"` but whose `FunctionRef` is a different function (e.g. `function-auto-ready` or a custom function), `hasEnvStep` evaluates to `true` simply based on `s.Name == "environment-configs"`.
As a result, `function-environment-configs` is never injected into `spec.pipeline` or `functions.yaml`. The emitted composition references `$env` variables that can never be resolved, failing at render time or in cluster.

## Evidence

In `internal/emit/pipeline.go`:
```go
hasEnvStep := false
for _, s := range b.Spec.Pipeline {
    if s.Name == "environment-configs" || s.FunctionRef == "function-environment-configs" {
        hasEnvStep = true
        break
    }
}
```
`hasEnvStep` is set to `true` if `s.Name == "environment-configs"` regardless of whether `s.FunctionRef` is actually `"function-environment-configs"`. If `s.FunctionRef` points to another function, the pipeline step does not execute `function-environment-configs`, and `function-environment-configs` is completely omitted from `functions.yaml` and the pipeline.

## Acceptance test

```go
// internal/emit/pipeline_test.go
func TestEffectivePipeline_UnrelatedStepNamedEnvironmentConfigsDropsFunction(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Fields["region"] = blueprint.Field{From: "env.region"}
	b.Spec.Environment = map[string]blueprint.EnvironmentKey{
		"region": {Type: "string"},
	}
	// A user pipeline has a step named "environment-configs", but its function is function-auto-ready
	b.Spec.Pipeline = []blueprint.PipelineStep{
		{
			Name:        "environment-configs",
			FunctionRef: "function-auto-ready",
			Package:     "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
		},
	}

	if err := b.Validate(); err != nil {
		t.Fatalf("blueprint validation failed: %v", err)
	}

	fns, err := Functions(b)
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	fnsStr := string(fns)
	if !strings.Contains(fnsStr, "function-environment-configs") {
		t.Errorf("functions.yaml must contain function-environment-configs, but it was dropped:\n%s", fnsStr)
	}

	compBytes, err := Composition(b, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	compStr := string(compBytes)
	if !strings.Contains(compStr, "function-environment-configs") {
		t.Errorf("Composition must contain a step referencing function-environment-configs, but it was dropped:\n%s", compStr)
	}
}
```

## Contract

1. In `internal/emit/pipeline.go`:
   - Check `s.FunctionRef == "function-environment-configs"` (or check both name and function ref appropriately). If a step is named `"environment-configs"` but has a different `FunctionRef`, generate a unique step name (e.g. `"environment-configs-fn"` or similar unique name) when injecting `function-environment-configs`, ensuring both steps are preserved and `function-environment-configs` is always present in `functions.yaml` and the composition pipeline.
2. Verify all pipeline and function emission tests continue to pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/emit/`.

## Handover

Branch `CF-318-effective-pipeline-unrelated-step-name`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
