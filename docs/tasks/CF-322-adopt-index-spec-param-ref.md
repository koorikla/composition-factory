# CF-322 — adopt drops parameter wires using index $spec syntax into raw strings, omitting XRD declarations

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Parameter wires using index $spec syntax in Go templates are dropped to raw strings and omitted from XRD) |
| **Closes** | `#211` — `CF-322 — adopt drops parameter wires using index $spec syntax into raw strings, omitting XRD declarations` |
| **Worktree** | `.worktrees/CF-322` on branch `CF-322-adopt-index-spec-param-ref` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/adopt/adopt.go`, parameter references in Go templates are matched via `reParamVar`, which only recognizes dot syntax (`$spec.<param>`).
Crossplane compositions frequently use `index` syntax, such as `{{ index $spec "param" }}`, `{{ (index $spec "param") }}`, or `{{ (index .observed.composite.resource.spec "param") }}`.
When adopting such a composition:
1. `reParamVar` fails to match the expression, falling back to a `Field{Raw: rawStr}` un-modeled raw template string.
2. The parameter is never discovered or declared in `bp.Spec.XRD.Parameters`.
This violates the Round-Trip Rule (`AGENTS.md` §1).

## Evidence

In `internal/adopt/adopt.go`:
`reParamVar` only checks dot-access:
```go
reParamVar = regexp.MustCompile(`\{\{-?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+?)(?:\s*\|\s*quote)?\s*-?\}\}`)
```
It does not support `index` syntax, unlike `reEnvVar` or `reObservedStatus`.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_IndexSpecParamRef(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-index
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: render-resources
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          {{- $spec := .observed.composite.resource.spec -}}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-bucket
          spec:
            forProvider:
              region: {{ index $spec "region" }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	f, ok := bp.Spec.Resources[0].Fields["region"]
	if !ok {
		t.Fatalf("field region not found: %+v", bp.Spec.Resources[0].Fields)
	}
	if f.From != "params.region" {
		t.Errorf("field region From = %q, Raw = %q, want From = %q", f.From, f.Raw, "params.region")
	}
	if _, ok := bp.Spec.XRD.Parameters["region"]; !ok {
		t.Errorf("parameter region not found in XRD parameters: %+v", bp.Spec.XRD.Parameters)
	}
}
```

## Contract

1. In `internal/adopt/adopt.go`:
   - Support `index` syntax in parameter matching (e.g. `reParamVar` and helper functions matching parameter access):
     `{{-?\s*(?:index\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)|\(?\s*index\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*["']([a-zA-Z0-9_.-]+)["']\s*\)?)...`
     Ensure both dot-access and index-access forms are supported in `matchParamVar` / `reParamVar`, and that parameter names are registered in `ensureParamDeclared`.
   - Remember the lessons of CF-313: exact trimmed-string matching when matching scalar fields and annotations to avoid matching substrings within larger interpolated strings.
2. Add acceptance test in `internal/adopt/adopt_test.go`.
3. Verify all adopt tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/adopt/`.

## Handover

Branch `CF-322-adopt-index-spec-param-ref`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
