# CF-331 — adopt drops Go template when guards using index syntax into unconditional resources

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Go template when guards using index $spec syntax are dropped into unconditional resources) |
| **Closes** | `#220` — `CF-331 — adopt drops Go template when guards using index syntax into unconditional resources` |
| **Worktree** | `.worktrees/CF-331` on branch `CF-331-adopt-when-guards-index-syntax` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/adopt/adopt.go`, `extractWhenGuard` parses `when` conditions using regexes (`reWhenIfSimple`, `reWhenIfEq`, `reWhenIfNe`, `reWhenIfEqRev`, `reWhenIfNeRev`) that only recognize dot-notation field access on composite spec, e.g.:
`(?:$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)`

When a Go template composition uses index notation for parameters in when conditions, e.g.:
- `eq (index $spec "tier") "prod"`
- `ne (index $spec "tier") "dev"`
- `eq "prod" (index $spec "tier")`
- `ne "dev" (index $spec "tier")`
- `(index .spec "tier")` or `(index $.spec "tier")`

`extractWhenGuard` fails to match the condition and returns `""`. As a result, the resource is adopted unconditionally and the parameter is pruned from `bp.Spec.XRD.Parameters` during orphaned parameter pruning, violating the Round-Trip Rule.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_WhenParamIndexSpec(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantParam string
	}{
		{
			name:      "param eq index $spec",
			condition: `eq (index $spec "tier") "prod"`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq index .spec",
			condition: `eq (index .spec "tier") "prod"`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq index $.spec",
			condition: `eq (index $.spec "tier") "prod"`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param ne index $spec",
			condition: `ne (index $spec "tier") "dev"`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		{
			name:      "param eq reversed index $spec",
			condition: `eq "prod" (index $spec "tier")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed index $spec",
			condition: `ne "dev" (index $spec "tier")`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-when-index
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.condition)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			if r.When != tc.wantWhen {
				t.Errorf("r.When = %q, want %q", r.When, tc.wantWhen)
			}
			if _, ok := bp.Spec.XRD.Parameters[tc.wantParam]; !ok {
				t.Errorf("parameter %q not declared in XRD parameters: %+v", tc.wantParam, bp.Spec.XRD.Parameters)
			}
		})
	}
}
```

## Contract

1. In `internal/adopt/adopt.go`:
   - In `extractWhenGuard`, support `(index <spec> "key")` / `index <spec> "key"` in `eq`, `ne`, and reversed comparisons, as well as simple truthiness tests.
   - Ensure the parameter is declared in `bp.Spec.XRD.Parameters` (e.g. via `reEvidenceIndexSpec` or `matchesParamRef`) so it is preserved across adoption.
2. Add acceptance test in `internal/adopt/adopt_test.go`.
3. Verify all adopt tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/adopt/`.

## Handover

Branch `CF-331-adopt-when-guards-index-syntax`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
