# CF-272 — effectivePipeline emits duplicate environment-configs step when custom step uses that name

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: pipeline deduplication and step collision prevention) |
| **Closes** | `CF-272 — effectivePipeline emits duplicate environment-configs step when custom step uses that name` |
| **Worktree** | `.worktrees/CF-272` on branch `CF-272-duplicate-env-configs-step` |
| **May write** | `internal/blueprint/pipeline.go`, `internal/emit/pipeline.go`, `internal/emit/pipeline_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When a blueprint defines `spec.environment` (which requires environment configs injection) and also defines a pipeline step named `environment-configs` in `spec.pipeline` (e.g. from an adopted composition or custom pipeline step), `cf gen` emits a Composition YAML with two duplicate `step: environment-configs` entries.

Crossplane Compositions require pipeline steps to have unique step names. Applying a Composition with duplicate step names fails schema validation or produces undefined behavior during pipeline reconciliation.

## Mechanism

1. In `internal/emit/pipeline.go:37-55`:
   ```go
   func effectivePipeline(b *blueprint.Blueprint) []blueprint.PipelineStep {
       var steps []blueprint.PipelineStep
       if b.HasEnvironment() {
           steps = append(steps, blueprint.PipelineStep{
               Name: "environment-configs",
               ...
           })
       }
       steps = append(steps, b.Spec.Pipeline...)
       return steps
   }
   ```
2. If `b.Spec.Pipeline` already contains a step with `Name: "environment-configs"`, it is appended without checking for step name collision or replacing/merging the step.
3. No validation check verifies that step names across the merged pipeline are unique.

## Contract

1. In `internal/blueprint/pipeline.go` / `internal/emit/pipeline.go`:
   - In `effectivePipeline`, if `b.HasEnvironment()` is true and `b.Spec.Pipeline` already defines a step named `environment-configs`, deduplicate by updating/merging or preventing duplicate step injection.
   - Alternatively, enforce in `blueprint.Validate()` that custom pipeline steps cannot collide with the reserved `environment-configs` step name, or rename the injected step if a conflict exists.
2. Ensure that emitted Composition YAML always contains strictly unique pipeline step names.

## Acceptance Test

Go unit test in `internal/emit/pipeline_test.go`:
1. Construct a blueprint with `spec.environment` and a custom step in `spec.pipeline` named `environment-configs`.
2. Generate Composition via `emit.Generate`.
3. Unmarshal the resulting Composition and verify that `spec.pipeline` contains exactly one step named `environment-configs`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-272-duplicate-env-configs-step`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
