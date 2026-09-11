# CF-301 — Emitters ignore forEach loop index when metadata.name is set, emitting duplicate resource names

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: emitted manifests have duplicate metadata.name for looped resources) |
| **Closes** | `#189` — `CF-301 — Emitters ignore forEach loop index when metadata.name is set, emitting duplicate resource names` |
| **Worktree** | `.worktrees/CF-301` on branch `CF-301-emitter-looped-custom-metadata-name` |
| **May write** | `internal/emit/composition.go`, `internal/emit/kcl.go`, `internal/emit/python.go`, `internal/emit/composition_test.go`, `internal/emit/kcl_test.go`, `internal/emit/python_test.go` |
| **Merges after** | `nothing` |

## Symptom

When a resource defines both `forEach` (iterating over an array) and a custom `metadata.name` (e.g., via `fields["metadata.name"]` or envelope `name`), all emitters (Go templating, KCL, Python) emit the static `metadata.name` verbatim inside the loop, ignoring the loop iteration index.
At runtime, this emits identical Kubernetes resource names for every iteration in the loop, causing Crossplane or Kubernetes to reject the rendered resources due to name collision.

## Evidence

In `internal/emit/composition.go:295-316`:
```go
		if metaName != nil {
			if metaName.guard != "" {
				d.Line(ti, "  {{- if %s }}", metaName.guard)
			}
			d.Line(ti, "  name: %s", metaName.rhs)
			if metaName.guard != "" {
				d.Line(ti, "  {{- end }}")
			}
		} else {
			if looped {
				d.Line(ti, `  name: {{ printf "%%s-%s-%%d" $xr $i }}`, r.Name)
			} else {
				d.Line(ti, "  name: {{ $xr }}-%s", r.Name)
			}
		}
```
Notice that when `metaName != nil`, `looped` is ignored, and `d.Line(ti, "  name: %s", metaName.rhs)` is written without `$i`.
The same pattern exists in `internal/emit/kcl.go:80-105` and `internal/emit/python.go:80-105`.

## Acceptance test

```go
// internal/emit/composition_test.go
func TestCF301_LoopedCustomMetadataNameIncludesIndex(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-looped-name"},
		Spec: blueprint.Spec{
			Parameters: map[string]blueprint.Parameter{
				"prefix": {Type: "string"},
			},
			Resources: []blueprint.Resource{
				{
					Name:    "worker",
					Type:    "apps/v1/Deployment",
					ForEach: "params.replicas",
					Fields: map[string]blueprint.FieldRule{
						"metadata.name": {From: "params.prefix"},
					},
				},
			},
		},
	}
	out, err := RenderComposition(bp, EngineGoTemplate)
	if err != nil {
		t.Fatalf("RenderComposition failed: %v", err)
	}
	// The generated template must index the custom name with $i when looped
	compStr := string(out)
	if !strings.Contains(compStr, `$i`) || !strings.Contains(compStr, `name:`) {
		t.Fatalf("expected looped custom metadata.name to incorporate loop index $i, got:\n%s", compStr)
	}
}
```

## Contract

1. In `internal/emit/composition.go`:
   - When `looped` is true and `metaName != nil`, the emitted `metadata.name` expression must format with the loop index `$i` (e.g. `printf "%s-%d"`).
2. In `internal/emit/kcl.go`:
   - When `r.ForEach != ""` and `metaName != nil`, the emitted `metadata.name` expression must append `-{i}` or format with index `i`.
3. In `internal/emit/python.go`:
   - When `r.ForEach != ""` and `metaName != nil`, the emitted `metadata.name` expression must append `-{i}` or format with index `i`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Static resources without `forEach`.

## Handover

Branch `CF-301-emitter-looped-custom-metadata-name`, committed, not pushed, not merged. Include passing test outputs across all three engines in your handover report.
