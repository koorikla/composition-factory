# CF-328 — adopt drops Go template boolean when guards like eq $spec.enabled true into unconditional resources

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Boolean when guards in Go templates are dropped into unconditional resources) |
| **Closes** | `#217` — `CF-328 — adopt drops Go template boolean when guards like eq $spec.enabled true into unconditional resources` |
| **Worktree** | `.worktrees/CF-328` on branch `CF-328-adopt-boolean-when-guards` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/adopt/adopt.go`, `extractWhenGuard` uses regexes (`reWhenIfEq`, `reWhenIfNe`, `reWhenIfEqRev`, `reWhenIfNeRev`) that strictly require quoted string literals (`"([^"]*)"`).
In Go templates, boolean conditions are often written as:
- `{{- if eq $spec.enabled true -}}`
- `{{- if eq true $spec.enabled -}}`
- `{{- if eq .spec.enabled true -}}`
- `{{- if default false $spec.enabled -}}`

Because boolean literals (`true`, `false`) are unquoted and `default` is not supported by `reWhenIfSimple`, `extractWhenGuard` fails to match and returns `""`, causing the resource to be adopted unconditionally.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_BooleanWhenGuard(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
	}{
		{
			name:      "eq spec true",
			condition: `eq $spec.enabled true`,
			wantWhen:  "params.enabled",
		},
		{
			name:      "eq true spec",
			condition: `eq true $spec.enabled`,
			wantWhen:  "params.enabled",
		},
		{
			name:      "dot spec eq true",
			condition: `eq .spec.enabled true`,
			wantWhen:  "params.enabled",
		},
		{
			name:      "default false spec",
			condition: `default false $spec.enabled`,
			wantWhen:  "params.enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-boolean-when
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- if %s }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
            data:
              key: value
            {{- end }}
`, tc.condition)

			bp, report, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			res := bp.Spec.Resources[0]
			if res.When != tc.wantWhen {
				t.Errorf("res.When = %q, want %q (report drops: %+v)", res.When, tc.wantWhen, report.Drops)
			}
		})
	}
}
```

## Contract

1. In `internal/adopt/adopt.go`:
   - In `extractWhenGuard`, support boolean comparisons (`eq <spec> true`, `eq true <spec>`) and default boolean expressions (`default false <spec>`).
   - Also ensure `matchesParamRef` and parameter discovery register the parameter in `bp.Spec.XRD.Parameters` as a `boolean` (or declared type) so it is not pruned.
2. Add acceptance test in `internal/adopt/adopt_test.go`.
3. Verify all adopt tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/adopt/`.

## Handover

Branch `CF-328-adopt-boolean-when-guards`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
