# CF-306 — parseGoTemplateBody omits uniqueName deduplication, failing adoption on duplicate resource names

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: adoption fails validation when GoTemplate composition contains duplicate resource names) |
| **Closes** | `#195` — `CF-306 — parseGoTemplateBody omits uniqueName deduplication, failing adoption on duplicate resource names` |
| **Worktree** | `.worktrees/CF-306` on branch `CF-306-adopt-gotemplate-duplicate-resource-name` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-299` |

## Symptom

When adopting a Crossplane Composition using `function-go-templating` where multiple documents define resources without an explicit `crossplane.io/composition-resource-name` annotation (or share duplicate names derived from their kind), `parseGoTemplateBody` in `internal/adopt/adopt.go` directly appends each resource to `bp.Spec.Resources` without invoking `uniqueName(bp, res)`.
Subsequent `bp.Validate()` fails with duplicate resource name errors:
`resource name "bucket" is not unique across resources`.

## Evidence

In `internal/adopt/adopt.go:2104-2122`:
```go
		for _, doc := range docs {
			ScrubDocument(doc, "", report)
			res := resourceFromMap(doc, opts, placeholderTable, report, nameMapping, bp)
			if res == nil {
...
				continue
			}
			if when != "" {
				res.When = when
			}
			if forEach != "" {
				res.ForEach = forEach
			}
			bp.Spec.Resources = append(bp.Spec.Resources, *res)
		}
```
Contrast this with classic patch adoption in `internal/adopt/adopt.go:2229-2230`, where `uniqueName(bp, res)` is explicitly called before adding to the blueprint.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestCF306_AdoptGoTemplateDuplicateResourceNames(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-duplicate-names
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XStorage
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
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            name: primary
          spec:
            forProvider:
              region: us-east-1
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            name: secondary
          spec:
            forProvider:
              region: us-west-2
`
	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}

	if bp.Spec.Resources[0].Name == bp.Spec.Resources[1].Name {
		t.Errorf("resources must have distinct names, both are %q", bp.Spec.Resources[0].Name)
	}

	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed on adopted resources: %v", err)
	}
}
```

**Fails today with:**
```
bp.Validate() failed: resource name "bucket" is not unique across resources
```

## Contract

1. In `internal/adopt/adopt.go`:
   - In `parseGoTemplateBody`, call `uniqueName(bp, res)` before appending `*res` to `bp.Spec.Resources`.
   - If `res.Name` changes and a composition resource name or mapping was tracked, update `nameMapping` accordingly so downstream status reference rewriting finds the renamed resource.
2. `bp.Validate()` must succeed on compositions containing multiple resources of the same kind without explicit composition resource names.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Renaming resources in classic composition mode (already handled).

## Handover

Branch `CF-306-adopt-gotemplate-duplicate-resource-name`, committed, not pushed, not merged. Include failing and passing test output in the handover report.
