# CF-223 — adopt extracts metadata.labels on managed resources into fields, producing ungenerable blueprint

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: internal/adopt/adopt.go, internal/adopt/*_test.go

## Background & Problem
Adopting a Composition that sets metadata.labels on a managed resource places the labels into res.Fields["metadata.labels[...]"] with zero loss reported, producing a blueprint that cf gen permanently rejects.

In internal/adopt/adopt.go:
- Lines 1850-1865 (patches): when toPath starts with metadata.labels., it sets res.Fields["metadata.labels[...]"].
- Line 2064 (base manifests): when otherMeta contains labels, extractFields("metadata", otherMeta, res.Fields, ...) extracts it into res.Fields["metadata.labels[...]"].

For managed resources (where crd.Native is false / res.Provider != "k8s"), internal/emit/composition.go strictly validates every field in res.Fields against the CRD schema's spec.forProvider. Because metadata.labels is not a forProvider field in provider CRDs, cf gen refuses to emit the blueprint:
cf: error: resource "bucket": field "metadata.labels[env]" is not in Bucket spec.forProvider (an unknown field is silently pruned by the API server on apply, so it must be caught here)

This violates the Round-Trip Rule: cf adopt exits 0 with zero loss reported, but emits a blueprint that cannot be generated.

## Contract & Expected Behavior
1. For managed resources, metadata.labels (whether present in inline base manifests or targeted by patches) must NOT be extracted into res.Fields.
2. Any metadata.labels on managed resources must be recorded in the adopt LossReport as dropped items (e.g. metadata.labels on managed resources are not supported in blueprint).
3. For native resources (provider == "k8s" / crd.Native), metadata fields in res.Fields continue to be supported as they are emitted via writeNativeFields.
4. Blueprints generated from adopting compositions with managed resource labels must be valid and generable by cf gen without errors.

## Acceptance Test
Add an automated Go test in internal/adopt (e.g. internal/adopt/cf223_labels_test.go):
- Adopt a composition with a managed resource that has:
  1. Base manifest with metadata.labels
  2. Patch with toFieldPath: metadata.labels.env
- Verify that adopt reports loss for the dropped labels.
- Verify that res.Fields does NOT contain metadata.labels[...].
- Verify that cf gen (or emit.Emit) succeeds on the resulting blueprint without is not in ... spec.forProvider error.
