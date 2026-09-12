# CF-317 — adopt extractForEachGuard leaves unnormalized resource names on status forEach references

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Validation failure on adoption of loops referencing unnormalized resource names) |
| **Closes** | `#206` — `CF-317 — adopt extractForEachGuard leaves unnormalized resource names on status forEach references` |
| **Worktree** | `.worktrees/CF-317` on branch `CF-317-adopt-foreach-status-unnormalized-resource` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

When adopting a Composition containing a Go template loop (`range`) over the observed status of another resource whose name requires DNS normalization (e.g. `Main_Queue` or uppercase characters), `extractForEachGuard` copies the raw name from the template string into `r.ForEach` (e.g. `resources.Main_Queue.status.atProvider.nodeCount`).
Because the referenced resource was normalized to `main-queue` and registered in `nameMapping`, `r.ForEach` references an unknown resource, causing subsequent blueprint validation `bp.Validate()` to fail with:
`validate adopted blueprint: resource "worker-bucket" forEach: references unknown resource "Main_Queue"`.

## Evidence

In `internal/adopt/adopt.go`:
1. When parsing resources from the template, `extractResourceName` calls `normalizeDNSLabel(rawName)` and records `nameMapping[rawName] = normalizedName`.
2. When parsing loop bounds, `extractForEachGuard` matches `reForEachStatusLoop`:
```go
} else if m := reForEachStatusLoop.FindStringSubmatch(text); len(m) >= 3 {
    resName := m[1]
    statusPath := m[2]
    if resName == "" && len(m) >= 5 {
        resName = m[3]
        statusPath = m[4]
    }
    return fmt.Sprintf("resources.%s.status.%s", resName, statusPath)
}
```
`resName` is copied directly without normalizing or consulting `nameMapping`.
3. In `rewriteStatusReferences`, only `Fields`, `Annotations`, and `Envelope` are rewritten; `r.ForEach` (and `r.When`) is omitted.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_StatusForEachResourceNameNormalization(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xcomplex.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XComplex
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                crossplane.io/composition-resource-name: Main_Queue
            spec:
              forProvider:
                region: us-east-1
            ---
            {{- range $i := until (int (index $.observed.resources "Main_Queue").resource.status.atProvider.nodeCount) }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: worker-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	queue := bp.ResourceNamed("main-queue")
	if queue == nil {
		t.Fatalf("expected resource main-queue, got: %+v", bp.Spec.Resources)
	}

	bucket := bp.ResourceNamed("worker-bucket")
	if bucket == nil {
		t.Fatalf("expected resource worker-bucket, got: %+v", bp.Spec.Resources)
	}

	if bucket.ForEach != "resources.main-queue.status.atProvider.nodeCount" {
		t.Errorf("bucket.ForEach = %q, want %q", bucket.ForEach, "resources.main-queue.status.atProvider.nodeCount")
	}
}
```

## Contract

1. In `internal/adopt/adopt.go`:
   - In `rewriteStatusReferences`, also rewrite status references in `r.ForEach` (and `r.When` if applicable) using `nameMapping`.
   - Alternatively or additionally, normalize or map resource names in `extractForEachGuard`.
2. Ensure adopted blueprints validate cleanly when status loops refer to resources requiring DNS normalization.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/adopt/`.

## Handover

Branch `CF-317-adopt-foreach-status-unnormalized-resource`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
