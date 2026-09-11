# CF-283 — Blueprint deepCopy aliases Environment, EnvironmentConfigs, Pipeline, and Emit pointers

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: in-memory Blueprint transactional isolation) |
| **Closes** | `CF-283 — Blueprint deepCopy aliases Environment, EnvironmentConfigs, Pipeline, and Emit pointers` |
| **Worktree** | `.worktrees/CF-283` on branch `CF-283-deepcopy-aliased-fields` |
| **May write** | `internal/blueprint/edit.go`, `internal/blueprint/edit_test.go`, `internal/blueprint/templates_test.go` |
| **Merges after** | `CF-282` |

## Symptom

When cloning a Blueprint via `b.deepCopy()` in `internal/blueprint/edit.go`:
1. `cp.Spec.Environment` is shallow-copied, sharing its map pointer with `b.Spec.Environment`.
2. `cp.Spec.EnvironmentConfigs` is shallow-copied, sharing its slice backing array with `b.Spec.EnvironmentConfigs`.
3. `cp.Spec.Pipeline` is shallow-copied, sharing its slice backing array with `b.Spec.Pipeline`.
4. `cp.Spec.Emit` is shallow-copied, sharing its `*Emit` pointer with `b.Spec.Emit`.

Any mutation on `cp` against these fields mutates the original receiver `b` directly. As documented in `internal/blueprint/edit.go:18-23`:
"Maintenance note: if Parameter or Resource ever grows another slice- or map-typed field, it must be deep-copied here too. Missing one does not fail to compile and does not fail a test until someone writes a mutating test against that specific field -- until then, a 'rejected' edit would silently mutate the receiver through the aliased backing store, exactly the failure mode this function exists to close."
Because transactional mutation methods clone `b` via `b.deepCopy()`, validate, and only commit on success, any mutation to these aliased fields breaks transactional rollback isolation.

## Mechanism

In `internal/blueprint/edit.go:24-67`:
```go
func (b *Blueprint) deepCopy() *Blueprint {
	cp := *b

	cp.Spec.Sources = append([]Source(nil), b.Spec.Sources...)
	cp.Spec.Conventions = append([]Convention(nil), b.Spec.Conventions...)
...
```
`deepCopy()` copies `Sources`, `Conventions`, `Templates`, `XRD.Parameters`, and `Resources` (including `Fields`, `Envelope`, and `Annotations`). However, when `Environment`, `EnvironmentConfigs`, `Pipeline`, and `Emit` were added to `Spec` in `internal/blueprint/types.go`, `b.deepCopy()` was never updated to deep-copy them.

## Contract

1. In `internal/blueprint/edit.go`:
   - If `b.Spec.Environment != nil`, allocate a new map and copy all `EnvironmentKey` entries.
   - If `b.Spec.EnvironmentConfigs != nil`, allocate a new slice and deep-copy each `EnvironmentConfig` (including its `Data`, `Values`, and `MatchLabels` maps).
   - If `b.Spec.Pipeline != nil`, allocate a new slice and copy each `PipelineStep`.
   - If `b.Spec.Emit != nil`, allocate a new `Emit` struct pointer and copy its fields (`*cp.Spec.Emit = *b.Spec.Emit`).
2. Update tests in `internal/blueprint/templates_test.go` or `internal/blueprint/edit_test.go` to assert that mutating `Environment`, `EnvironmentConfigs`, `Pipeline`, and `Emit` on a clone produced by `deepCopy()` leaves the original receiver completely unchanged.

## Acceptance Test

```go
func TestDeepCopyDoesNotAliasEnvironmentPipelineOrEmit(t *testing.T) {
	orig := &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "test-bp"},
		Spec: Spec{
			Environment: map[string]EnvironmentKey{
				"region": {Type: "string", Default: "us-east-1"},
			},
			EnvironmentConfigs: []EnvironmentConfig{
				{
					Name: "env-conf",
					Data: map[string]any{"tier": "prod"},
				},
			},
			Pipeline: []PipelineStep{
				{Step: "auto-ready", Function: "function-auto-ready"},
			},
			Emit: &Emit{
				Engine:         EngineGoTemplating,
				TemplateSource: TemplateSourceInline,
			},
		},
	}

	want := &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "test-bp"},
		Spec: Spec{
			Environment: map[string]EnvironmentKey{
				"region": {Type: "string", Default: "us-east-1"},
			},
			EnvironmentConfigs: []EnvironmentConfig{
				{
					Name: "env-conf",
					Data: map[string]any{"tier": "prod"},
				},
			},
			Pipeline: []PipelineStep{
				{Step: "auto-ready", Function: "function-auto-ready"},
			},
			Emit: &Emit{
				Engine:         EngineGoTemplating,
				TemplateSource: TemplateSourceInline,
			},
		},
	}

	cp := orig.deepCopy()
	cp.Spec.Environment["region"] = EnvironmentKey{Type: "integer"}
	cp.Spec.Environment["newKey"] = EnvironmentKey{Type: "boolean"}
	cp.Spec.EnvironmentConfigs[0].Name = "mutated-conf"
	cp.Spec.EnvironmentConfigs[0].Data["tier"] = "dev"
	cp.Spec.Pipeline[0].Step = "mutated-step"
	cp.Spec.Emit.Engine = EnginePython
	cp.Spec.Emit.TemplateSource = TemplateSourceFileSystem

	if diff := cmp.Diff(want, orig); diff != "" {
		t.Errorf("mutating deepCopy modified original receiver (-want +got):\n%s", diff)
	}
}
```
