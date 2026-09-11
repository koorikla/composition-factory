# CF-312 — adopt drops Go template when guards with reversed operands like eq "prod" $spec.tier

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Round-trip rule violation when adopting conditional resources with reversed comparison operands) |
| **Closes** | `#201` — `CF-312 — adopt drops Go template when guards with reversed operands like eq "prod" $spec.tier` |
| **Worktree** | `.worktrees/CF-312` on branch `CF-312-adopt-reversed-when-guards` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/adopt/adopt.go`, `extractWhenGuard` extracts conditional resource guards from pipeline Go templates (like `function-go-templating`) and maps them to blueprint `when` conditions.
However, `reWhenIfEq` and `reWhenIfNe` only match Go template conditions where the parameter path precedes the literal string:
```go
reWhenIfEq = regexp.MustCompile(`\{\{-?\s*if\s+eq\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+\"([^\"]*)\"\s*-?\}\}`)
reWhenIfNe = regexp.MustCompile(`\{\{-?\s*if\s+ne\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+\"([^\"]*)\"\s*-?\}\}`)
```

Go templates evaluate `eq` and `ne` symmetrically (`{{ if eq "prod" $.spec.tier }}` is standard Go template syntax). When an adopted composition has the literal operand first:
1. `reWhenIfEq` / `reWhenIfNe` fail to match.
2. `extractWhenGuard` returns `""`.
3. `res.When` is omitted, causing the resource to be adopted unconditionally.
4. Downstream emission (`cf gen`) generates the resource unconditionally, violating the round-trip rule.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_ReversedWhenGuard(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: reversed-when-guard
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XReversed
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
            {{- if eq "prod" $.spec.tier }}
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: pro-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
            {{- if ne "basic" $.observed.composite.resource.spec.tier }}
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: nonbasic-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var proBucket, nonbasicBucket *blueprint.Resource
	for i := range bp.Spec.Resources {
		switch bp.Spec.Resources[i].Name {
		case "pro-bucket":
			proBucket = &bp.Spec.Resources[i]
		case "nonbasic-bucket":
			nonbasicBucket = &bp.Spec.Resources[i]
		}
	}

	if proBucket == nil {
		t.Fatalf("pro-bucket resource not found: %+v", bp.Spec.Resources)
	}
	if proBucket.When != `params.tier == "prod"` {
		t.Errorf("proBucket.When = %q, want %q", proBucket.When, `params.tier == "prod"`)
	}

	if nonbasicBucket == nil {
		t.Fatalf("nonbasic-bucket resource not found: %+v", bp.Spec.Resources)
	}
	if nonbasicBucket.When != `params.tier != "basic"` {
		t.Errorf("nonbasicBucket.When = %q, want %q", nonbasicBucket.When, `params.tier != "basic"`)
	}
}
```

**Fails today with:**
```
proBucket.When = "", want "params.tier == \"prod\""
nonbasicBucket.When = "", want "params.tier != \"basic\""
```

## Contract

1. In `internal/adopt/adopt.go`:
   - Support reversed operands for parameter string equality and inequality (`eq "value" $.spec.path` and `ne "value" $.spec.path`, with all supported prefix variants: `$spec`, `$.spec`, `.spec`, `$.observed.composite.resource.spec`, `.observed.composite.resource.spec`).
   - Also support reversed operands for environment variable comparisons if applicable.
   - Map them to the canonical blueprint format: `params.<name> == "<value>"` or `params.<name> != "<value>"`.
2. Existing parameter and environment when-guard patterns must continue to work without regression.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/adopt/`.

## Handover

Branch `CF-312-adopt-reversed-when-guards`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
