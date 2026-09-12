# CF-320 — refuseGoTemplateOnlyFeatures permits spec.templates with non-Go engines, dropping them silently

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: spec.templates silently dropped when emitting for KCL or Python engines) |
| **Closes** | `#209` — `CF-320 — refuseGoTemplateOnlyFeatures permits spec.templates with non-Go engines, dropping them silently` |
| **Worktree** | `.worktrees/CF-320` on branch `CF-320-refuse-spec-templates-non-go-engines` |
| **May write** | `internal/emit/plan.go`, `internal/emit/kcl_test.go` |
| **Merges after** | `nothing` |

## Symptom

When emitting a composition for non-Go-templating engines (`kcl`, `python`), `refuseGoTemplateOnlyFeatures` in `internal/emit/plan.go:99-129` validates that Go-template-only features are not present on the blueprint.
It checks `spec.conventions`, `template:` fields, `template:` annotations, and Go template expressions in `raw:` values.
However, it fails to check `b.Spec.Templates`.
If a blueprint declares `spec.templates` and sets `spec.emit.engine: kcl` or `python`, `Composition(b, crds)` succeeds without warning or error, and the emitted composition silently drops the declared templates.

## Evidence

In `internal/emit/plan.go`:
```go
func refuseGoTemplateOnlyFeatures(b *blueprint.Blueprint) error {
	engine := b.Engine()
	if engine == blueprint.EngineGoTemplate {
		return nil
	}
	if len(b.Spec.Conventions) > 0 {
		return fmt.Errorf("spec.conventions: engine %q does not support conventions", engine)
	}
    // omits checking len(b.Spec.Templates) > 0
```
`b.Spec.Templates` is never checked.

## Acceptance test

```go
// internal/emit/kcl_test.go
func TestKCLRefusesSpecTemplates(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-tmpl
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      region:
        type: string
        required: true
  templates:
    cf.tags: "{{ .xr }}-tags"
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          from: params.region
`
	b, err := blueprint.LoadBytes([]byte(bpYAML))
	if err != nil {
		t.Fatal(err)
	}
	crds, err := schema.ParseCRDs([][]byte{fakeQueueCRD})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Composition(b, crds)
	if err == nil {
		t.Fatal("expected error on KCL with spec.templates, got nil")
	}
}
```

## Contract

1. In `internal/emit/plan.go`:
   - In `refuseGoTemplateOnlyFeatures`, check `if len(b.Spec.Templates) > 0` and return an informative error (e.g. `fmt.Errorf("spec.templates: engine %q does not support template: blocks", engine)`).
2. Add acceptance test in `internal/emit/kcl_test.go`.
3. Verify all emit tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/emit/`.

## Handover

Branch `CF-320-refuse-spec-templates-non-go-engines`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
