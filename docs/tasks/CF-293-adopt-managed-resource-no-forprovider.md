# CF-293 — adopt puts managed resource spec in fields when forProvider absent, dropping envelope and failing

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: managed resource envelope routing when forProvider is absent) |
| **Closes** | `#181` — `CF-293 — adopt puts managed resource spec in fields when forProvider absent, dropping envelope and failing` |
| **Worktree** | `.worktrees/CF-293` on branch `CF-293-adopt-managed-resource-no-forprovider` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-290` |

## Symptom

When adopting a Composition containing a managed resource whose `spec` lacks a `forProvider` mapping (for example, when provider fields are defaulted or wired by patches, and only envelope fields like `deletionPolicy` or `writeConnectionSecretToRef` are defined under `spec`), `cf adopt` routes all `spec` keys into `resource.Fields` prefixed with `spec.`. During schema validation, `pruneAgainstSchema` evaluates these fields against `spec.forProvider`, flags them as unknown fields, drops them with loss report entries, and causes `cf adopt` to exit with code 2.

## Evidence

Given a Composition with a managed resource specifying envelope fields under `spec` without `forProvider`:

```sh
$ cat << 'EOF' > /tmp/test_managed_no_forprovider.yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
  mode: Pipeline
  pipeline:
  - step: go-templating
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
              crossplane.io/composition-resource-name: test-bucket
          spec:
            deletionPolicy: Orphan
            writeConnectionSecretToRef:
              name: test-secret
              namespace: default
EOF
$ ./bin/cf adopt /tmp/test_managed_no_forprovider.yaml
```

Output:
```
# adopt: dropped resource.test-bucket.fields.spec.deletionPolicy (field "spec.deletionPolicy" is not in Bucket spec.forProvider (unknown field pruned by schema))
# adopt: dropped resource.test-bucket.fields.spec.writeConnectionSecretToRef.name (field "spec.writeConnectionSecretToRef.name" is not in Bucket spec.forProvider (unknown field pruned by schema))
# adopt: dropped resource.test-bucket.fields.spec.writeConnectionSecretToRef.namespace (field "spec.writeConnectionSecretToRef.namespace" is not in Bucket spec.forProvider (unknown field pruned by schema))
```

Exits with code 2 (`loss during adoption`).

Reproduced on commit `1c1374e`.

## Location

`internal/adopt/adopt.go:2535-2585` (`resourceFromMap`):
```go
	// Extract spec fields
	if spec, ok := m["spec"].(map[string]any); ok {
		if forProvider, ok := spec["forProvider"].(map[string]any); ok {
			extractFields("", forProvider, res.Fields, placeholders, res.Name, report, nameMapping, bp, isNative)
			...
			extractEnvelopeFields("", map[string]any{k: v}, res.Envelope, placeholders, res.Name, report, nameMapping, bp)
		} else {
			extractFields("spec", spec, res.Fields, placeholders, res.Name, report, nameMapping, bp, isNative)
		}
	}
```

When `forProvider` is absent and `!isNative`, the code branches to `extractFields("spec", spec, ...)` instead of parsing envelope keys into `res.Envelope`.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestCF293_ManagedResourceWithoutForProvider(t *testing.T) {
	manifest := []byte(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
  mode: Pipeline
  pipeline:
  - step: go-templating
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
              crossplane.io/composition-resource-name: test-bucket
          spec:
            deletionPolicy: Orphan
            writeConnectionSecretToRef:
              name: test-secret
              namespace: default
`)
	bp, report, err := Adopt(manifest, Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.Envelope.DeletionPolicy != "Orphan" {
		t.Errorf("expected envelope deletionPolicy Orphan, got %q", res.Envelope.DeletionPolicy)
	}
	if res.Envelope.WriteConnectionSecretToRef == nil || res.Envelope.WriteConnectionSecretToRef.Name != "test-secret" {
		t.Errorf("expected writeConnectionSecretToRef.name test-secret, got %+v", res.Envelope.WriteConnectionSecretToRef)
	}
	if len(res.Fields) != 0 {
		t.Errorf("expected 0 fields in Fields map, got %+v", res.Fields)
	}
	if len(report.DroppedFields) != 0 {
		t.Errorf("expected 0 dropped fields, got %v", report.DroppedFields)
	}
}
```

**Fails today with:**
```
adopt_test.go: expected 0 dropped fields, got [resource.test-bucket.fields.spec.deletionPolicy (field "spec.deletionPolicy" is not in Bucket spec.forProvider (unknown field pruned by schema)) resource.test-bucket.fields.spec.writeConnectionSecretToRef.name (field "spec.writeConnectionSecretToRef.name" is not in Bucket spec.forProvider (unknown field pruned by schema)) resource.test-bucket.fields.spec.writeConnectionSecretToRef.namespace (field "spec.writeConnectionSecretToRef.namespace" is not in Bucket spec.forProvider (unknown field pruned by schema))]
```

## Contract

1. In `resourceFromMap`:
   - When `!isNative`:
     - If `spec.forProvider` exists, extract provider fields into `res.Fields` as before.
     - Extract all envelope fields under `spec` (other than `forProvider` and `initProvider`) into `res.Envelope` via `extractEnvelopeFields`, regardless of whether `forProvider` was present.
     - Do NOT route `spec` keys into `res.Fields` for managed resources.
   - When `isNative`:
     - Extract `spec` fields into `res.Fields` prefixed with `"spec"`, as before.
2. Ensure round-trip generation and schema validation succeed without dropping valid envelope fields or reporting unknown field loss.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changing schema validation rules for `isNative` resources.
- Native Kubernetes envelope fields outside `spec`.

## Handover

Branch `CF-293-adopt-managed-resource-no-forprovider`, committed, not pushed, not merged. Final report includes failing and passing runs of the acceptance test and all standard gates.
