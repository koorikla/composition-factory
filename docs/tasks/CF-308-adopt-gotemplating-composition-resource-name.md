# CF-308 — adopt fails on gotemplating composition-resource-name annotation with blueprint validation error

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: blueprint validation fails on adopted gotemplating composition-resource-name annotations) |
| **Closes** | `#197` — `CF-308 — adopt fails on gotemplating composition-resource-name annotation with blueprint validation error` |
| **Worktree** | `.worktrees/CF-308` on branch `CF-308-adopt-gotemplating-composition-resource-name` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-310` |

## Symptom

Compositions using `function-go-templating` frequently write or contain the `gotemplating.fn.crossplane.io/composition-resource-name` annotation on composed resources.
While `internal/blueprint/annotations.go` defines this key as reserved node identity alongside `crossplane.io/composition-resource-name`, `internal/adopt/adopt.go` fails to recognize or scrub it, adding it to `res.Annotations` and triggering an unrecoverable blueprint validation failure upon adoption.

## Evidence

1. In `internal/adopt/adopt.go:1325` (`extractResourceName`), `adopt.go:1837` (`identifyChunkTarget`), and `reChunkResNameAnn` (`adopt.go:1132`), adopt only checks for `crossplane.io/composition-resource-name`.
2. In `internal/adopt/adopt.go:2542` (`resourceFromMap`), annotation filtering explicitly checks:
   ```go
   if k == "crossplane.io/composition-resource-name" || strings.Contains(rawK, "setResourceNameAnnotation") || strings.Contains(rawStr, "setResourceNameAnnotation") {
       continue
   }
   ```
   It omits `gotemplating.fn.crossplane.io/composition-resource-name`, so the key is placed directly into `res.Annotations[rawK]`.
3. When `Adopt` calls `bp.Validate()`, `internal/blueprint/annotations.go:120` checks `reservedAnnotationKeys[k]` and rejects the blueprint because `gotemplating.fn.crossplane.io/composition-resource-name` is in `reservedAnnotationKeys`.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_GotemplatingAnnotation(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xbuckets.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XBucket
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: my-bucket
            spec:
              forProvider:
                region: us-east-1
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Name != "my-bucket" {
		t.Errorf("expected resource name %q, got %q", "my-bucket", bp.Spec.Resources[0].Name)
	}
	if _, exists := bp.Spec.Resources[0].Annotations["gotemplating.fn.crossplane.io/composition-resource-name"]; exists {
		t.Errorf("expected gotemplating.fn.crossplane.io/composition-resource-name to be stripped from res.Annotations")
	}
}
```

**Fails today with:**
```
Adopt failed: validate adopted blueprint: resource "bucket" annotation "gotemplating.fn.crossplane.io/composition-resource-name": this key is the composition-resource-name annotation...
```

## Contract

1. In `internal/adopt/adopt.go`:
   - In `extractResourceName` and `identifyChunkTarget`, also recognize `gotemplating.fn.crossplane.io/composition-resource-name` as a source of resource name.
   - In `reChunkResNameAnn`, match both `crossplane.io/composition-resource-name` and `gotemplating.fn.crossplane.io/composition-resource-name`.
   - In `resourceFromMap` (line 2542), scrub `gotemplating.fn.crossplane.io/composition-resource-name` just like `crossplane.io/composition-resource-name` so it is not added to `res.Annotations`.
2. `Adopt` must succeed, setting resource name to the annotation value and leaving `res.Annotations` clean of reserved identity annotations.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changing `internal/blueprint/annotations.go` (already reserves both annotation keys).

## Handover

Branch `CF-308-adopt-gotemplating-composition-resource-name`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
