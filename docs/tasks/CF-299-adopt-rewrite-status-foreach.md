# CF-299 — adopt rewriteStatusReferences skips forEach status wires, failing validation on renamed resources

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: adoption fails validation when renamed resources are referenced in forEach) |
| **Closes** | `#187` — `CF-299 — adopt rewriteStatusReferences skips forEach status wires, failing validation on renamed resources` |
| **Worktree** | `.worktrees/CF-299` on branch `CF-299-adopt-rewrite-status-foreach` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-298` |

## Symptom

When adopting a Crossplane Composition where resource names are normalized (e.g. `my_vpc` -> `my-vpc`) or deduplicated, `rewriteStatusReferences` in `internal/adopt/adopt.go` rewrites status references in `r.Fields`, `r.Annotations`, and `r.Envelope`, but ignores `r.ForEach`.
As a result, `r.ForEach` retains the pre-normalized or stale resource name (e.g. `resources.my_vpc.status.atProvider.nodeCount`), and subsequent blueprint validation (`bp.Validate()`) rejects the blueprint because `my_vpc` does not exist in the adopted resources.

## Evidence

Given a Composition with a GoTemplate `range` over an observed status field of a resource with an underscore in its name:

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XCluster
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
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: VPC
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my_vpc
            name: my-vpc
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/16
          ---
          {{- range $i := until (int (index $.observed.resources "my_vpc").resource.status.atProvider.nodeCount) }}
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-subnet
            name: my-subnet
          spec:
            forProvider:
              vpcId: test
          {{- end }}
```

Adopting this manifest fails with:
```
resource "my-subnet" forEach references unknown resource "my_vpc"
```
because `my_vpc` was normalized to `my-vpc`, but `r.ForEach` was not rewritten.

## Location

`internal/adopt/adopt.go:3149-3172`:
```go
func rewriteStatusReferences(bp *blueprint.Blueprint, nameMapping map[string]string) {
	if len(nameMapping) == 0 {
		return
	}
	for i := range bp.Spec.Resources {
		r := &bp.Spec.Resources[i]
		for fName, f := range r.Fields {
			if f.From != "" {
				f.From = rewriteFromWire(f.From, nameMapping)
				r.Fields[fName] = f
			}
		}
		for aName, a := range r.Annotations {
			if a.From != "" {
				a.From = rewriteFromWire(a.From, nameMapping)
				r.Annotations[aName] = a
			}
		}
		for eName, e := range r.Envelope {
			if e.From != "" {
				e.From = rewriteFromWire(e.From, nameMapping)
				r.Envelope[eName] = e
			}
		}
	}
}
```
Notice `r.ForEach` is never checked or rewritten with `rewriteFromWire`.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestCF299_AdoptForEachNormalizedStatusReference(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XCluster
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
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: VPC
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my_vpc
            name: my-vpc
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/16
          ---
          {{- range $i := until (int (index $.observed.resources "my_vpc").resource.status.atProvider.nodeCount) }}
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-subnet
            name: my-subnet
          spec:
            forProvider:
              vpcId: test
          {{- end }}
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt error: %v", err)
	}

	var subnetForEach string
	for _, r := range bp.Spec.Resources {
		if r.Name == "my-subnet" {
			subnetForEach = r.ForEach
		}
	}
	want := "resources.my-vpc.status.atProvider.nodeCount"
	if subnetForEach != want {
		t.Errorf("expected subnet ForEach to be %q, got %q", want, subnetForEach)
	}
	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}
}
```

## Contract

1. In `internal/adopt/adopt.go`:
   - In `rewriteStatusReferences`, inspect each `r.ForEach`.
   - If `r.ForEach != ""`:
     Rewrite using `r.ForEach = rewriteFromWire(r.ForEach, nameMapping)`.
2. Blueprint validation (`bp.Validate()`) must pass when adopted resources with normalized or deduplicated names are referenced in `forEach`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Status references inside Go template conditionals (`r.When`).

## Handover

Branch `CF-299-adopt-rewrite-status-foreach`, committed, not pushed, not merged. Include failing and passing test runs in your final handover report.
