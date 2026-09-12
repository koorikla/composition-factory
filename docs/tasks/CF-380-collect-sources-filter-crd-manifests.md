# CF-380 — collectSources appends cluster and CRD manifest paths as package sources corrupting Configuration

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-380 — collectSources appends cluster and CRD manifest paths as package sources corrupting Configuration` (#271) |
| **Worktree** | `.worktrees/CF-380` on branch `CF-380-collect-sources-filter-crd-manifests`, branched from `main` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Defect Summary

In `internal/adopt/adopt.go:collectSources`, `bp.Spec.Sources` is populated by inspecting existing sources and resources.
While `collectSources` correctly checks `r.Provider == blueprint.NativeProvider`, it fails to skip:
1. `r.Provider == "cluster"` (live cluster scanned resource), and
2. `r.Provider` pointing to a local CRD manifest file (`.yaml` / `.yml` or matching an existing `s.CRDs` entry).
3. In addition, the first loop never tracks `seen[s.CRDs]`, so duplicate `crds:` sources are not deduplicated.

As a result:
- `collectSources` appends `{Provider: "cluster"}` and `{Provider: "crds/custom.yaml"}` to `bp.Spec.Sources`.
- `emit.ConfigurationMeta` (`internal/emit/configuration.go:104-119`) reads `b.Spec.Sources` and generates invalid Crossplane Provider package dependencies in `crossplane.yaml`:
  ```yaml
  dependsOn:
  - apiVersion: pkg.crossplane.io/v1
    kind: Provider
    package: crds/custom.yaml
  - apiVersion: pkg.crossplane.io/v1
    kind: Provider
    package: cluster
  ```

## Acceptance Test

In `internal/adopt/adopt_test.go`:

```go
func TestCollectSourcesWithClusterAndCRDManifestResource(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "app"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "App",
				Plural:  "apps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:     "string",
						Required: true,
					},
				},
			},
			Sources: []blueprint.Source{
				{CRDs: "crds/custom.yaml"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "custom-res",
					Kind:     "CustomResource",
					Provider: "crds/custom.yaml",
				},
				{
					Name:     "cluster-res",
					Kind:     "ClusterResource",
					Provider: "cluster",
				},
			},
		},
	}

	collectSources(bp, "")

	for _, s := range bp.Spec.Sources {
		if s.Provider == "cluster" {
			t.Errorf("collectSources incorrectly appended pseudo-provider \"cluster\" to spec.sources: %+v", bp.Spec.Sources)
		}
		if s.Provider == "crds/custom.yaml" {
			t.Errorf("collectSources incorrectly appended CRD manifest path %q as a provider source: %+v", s.Provider, bp.Spec.Sources)
		}
	}

	meta, err := emit.ConfigurationMeta(bp, nil)
	if err != nil {
		t.Fatalf("emit.ConfigurationMeta failed: %v", err)
	}
	if strings.Contains(string(meta), "crds/custom.yaml") {
		t.Errorf("emit.ConfigurationMeta emitted CRD manifest path as package dependency in crossplane.yaml:\n%s", string(meta))
	}
	if strings.Contains(string(meta), "cluster") {
		t.Errorf("emit.ConfigurationMeta emitted pseudo-provider cluster as package dependency in crossplane.yaml:\n%s", string(meta))
	}
}
```

## Contract

In `internal/adopt/adopt.go:collectSources`:
1. Deduplicate `s.CRDs` in the initial sources loop so duplicate CRD manifest sources are not appended.
2. In the `bp.Spec.Resources` loop, skip `r.Provider == "cluster"` and any `r.Provider` ending in `.yaml` or `.yml` (or matching an existing `s.CRDs` entry).

## Verification

```sh
make lint
make lint-strict
make test-race
go test ./internal/adopt -run TestCollectSourcesWithClusterAndCRDManifestResource -v
```
