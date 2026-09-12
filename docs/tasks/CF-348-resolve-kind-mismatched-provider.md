# CF-348 — resolveKind binds mismatched provider CRD instead of failing when resource provider is declared

## 1. Context & Invariant

When a blueprint declares managed resources and explicitly specifies `provider: <oci-package-ref>` on a resource (to disambiguate or pin the resource to a specific provider declared in `spec.sources`), the emitter must resolve the resource against a CRD belonging to that provider. If the requested kind does not belong to the specified provider, emission must fail loudly with an error rather than silently resolving and binding to an unrelated provider's CRD.

In `internal/emit/composition.go`:
1. When exactly one candidate CRD with the matching kind exists (`len(candidates) == 1`), `resolveKind` unconditionally returns `crds[candidates[0]]` without checking whether `r.Provider` matches the CRD's group.
2. When multiple candidate CRDs with the matching kind exist (`len(candidates) > 1`), `resolveKind` loops over candidates looking for a provider match via `matchesProvider(crds[idx].Group, r.Provider)`. If no candidate matches `r.Provider`, `resolveKind` falls back to `return crds[candidates[0]], nil`, silently selecting the first candidate.

In both cases, a resource declaring `provider: "xpkg.upbound.io/upbound/provider-azure-storage:v1.0.0"` silently binds to an AWS CRD (such as `sqs.aws.m.upbound.io`) or GCP CRD, generating a Composition with the wrong provider and wrong `apiVersion`.

## 2. Requirements & Contract

1. In `internal/emit/composition.go`:
   - In `resolveKind(r blueprint.Resource, crds []schema.CRD)`:
     - When `len(candidates) == 1`:
       - If `r.Provider != ""` and `!matchesProvider(crds[candidates[0]].Group, r.Provider)`:
         Return `schema.CRD{}, fmt.Errorf("resource %q: kind %q not found in provider %q", r.Name, r.Kind, r.Provider)`
     - When `len(candidates) > 1`:
       - After checking for matching candidates:
         If `r.Provider != ""` (and no candidate matched):
           Return `schema.CRD{}, fmt.Errorf("resource %q: kind %q not found in provider %q", r.Name, r.Kind, r.Provider)`
2. Also inspect `internal/adopt/xrdless.go` to check if `resolveKind` exists there and ensure consistent behavior if applicable.
3. Guard with automated unit tests in `internal/emit/composition_test.go`:
   - `TestResolveKindMismatchedProviderSingleCandidate`: verifies error when single candidate CRD does not match `r.Provider`.
   - `TestResolveKindMismatchedProviderMultipleCandidates`: verifies error when multiple candidate CRDs do not match `r.Provider`.
4. Ensure `make lint && make lint-strict && make test-race` passes.

## 3. Verbatim Repro Test

```go
func TestResolveKindMismatchedProviderSingleCandidate(t *testing.T) {
	bp := testBlueprint()
	bp.Spec.Sources = []blueprint.Source{
		{Provider: "xpkg.upbound.io/upbound/provider-azure-storage:v1.0.0"},
	}
	bp.Spec.Resources[0].Provider = "xpkg.upbound.io/upbound/provider-azure-storage:v1.0.0"

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate: %v", err)
	}

	crds := testCRDs(t)
	_, err := Composition(bp, crds)
	if err == nil {
		t.Fatal("Composition: expected error when resource provider does not match resolved CRD, got nil")
	}
}

func TestResolveKindMismatchedProviderMultipleCandidates(t *testing.T) {
	bp := testBlueprint()
	bp.Spec.Sources = []blueprint.Source{
		{Provider: "xpkg.upbound.io/upbound/provider-azure-storage:v1.0.0"},
	}
	bp.Spec.Resources[0].Provider = "xpkg.upbound.io/upbound/provider-azure-storage:v1.0.0"

	crd1 := testCRDs(t)[0]
	crd2 := crd1
	crd2.Group = "sqs2.aws.m.upbound.io"

	_, err := Composition(bp, []schema.CRD{crd1, crd2})
	if err == nil {
		t.Fatal("Composition: expected error when resource provider does not match any candidate CRD, got nil")
	}
}
```
