# CF-109 — A spec.conventions entry matching a native-kind field is silently ignored instead of refused

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (engine scale: loss that survives to the cluster; user conventions for native kinds silently never apply) |
| **Closes** | `CF-109 — *(engine)* A spec.conventions entry that matches a native-kind field is silently ignored — not applied, not refused — although docs/dsl.md:342 and :418 say it is refused with "conventions cannot match native Kubernetes kind".` |
| **Worktree** | `.worktrees/CF-109` on branch `CF-109-conventions-native-kind` |
| **May write** | `internal/emit/plan.go`, `internal/emit/composition.go`, `internal/emit/templates_test.go` |
| **Merges after** | nothing |

## Symptom

A user configures `spec.conventions` expecting conventions (such as labels or metadata) to apply across all resources.
When a convention's `match` matches a top-level field on a native Kubernetes kind (e.g. `match: immutable` or `match: type` on a `Secret`), `cf gen` exits 0.
The emitted Secret contains no convention-applied field, and the `{{- define "<name>" }}` block is emitted into the template but never called.
The user relying on conventions for native kinds gets nothing on the cluster and no warning or error.

Meanwhile, `docs/dsl.md:342` and `docs/dsl.md:418` explicitly promise that:
`resource "<name>": conventions cannot match native Kubernetes kind`
and that conventions on native kinds are refused.

## Evidence

In Mission J3 finding F4 (`docs/comp-runs/2026-09-09-cli-mcp-journey.md`):
Running `cf gen err/h2-conv-native.cf.yaml -o err/out-h2-conv-native` with `templates: {imm: "true\n"}` and `conventions: [{match: immutable, template: imm}]` against a native `Secret`:
Generates manifests with exit 0.
The emitted Secret has no `immutable:` key.
The documented error `resource "<name>": conventions cannot match native Kubernetes kind` never appears.

## Location

- `internal/emit/plan.go:56-62`:
  ```go
  fields := r.Fields
  if !crd.Native {
      var cerr error
      fields, cerr = conventionFields(r, b, crd)
      if cerr != nil {
          return plannedResource{}, cerr
      }
  }
  ```
  `planSingleResource` unconditionally skips `conventionFields` when `crd.Native` is true, with no check whether any convention would have matched.
- `internal/emit/composition.go:561-563`:
  `conventionFields` also silently returns `r.Fields, nil` if `crd.Native`.
- `internal/emit/templates_test.go:344-383`:
  `TestConventionsSkipNativeKinds` currently asserts that conventions skip native kinds without error when no convention matches native fields.

## Acceptance test

Write this test **first**, verbatim in `internal/emit/templates_test.go`, and watch it fail before changing production code:

```go
func TestCF109ConventionMatchingNativeKindIsRefused(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	b := conventionTestBlueprint()
	// Add a convention that matches a top-level leaf field on Secret (e.g. "immutable" or "type")
	b.Spec.Conventions = append(b.Spec.Conventions, blueprint.Convention{
		Match:    "immutable",
		Template: "cf.tags",
	})
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name:     "secret",
		Kind:     "Secret",
		Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"type": {Value: "Opaque"},
		},
	})

	_, err = Composition(b, native)
	if err == nil {
		t.Fatal("expected error when convention matches native kind field, got nil")
	}
	wantMsg := `resource "secret": conventions cannot match native Kubernetes kind`
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("expected error containing %q, got: %v", wantMsg, err)
	}
}
```

## Contract

- When a resource is a native Kubernetes kind (`crd.Native`), the emitter must inspect `b.Spec.Conventions` against the native kind's schema nodes (`crd.ForProvider()`).
- If any convention's `Match` suffix matches any top-level leaf of the native CRD that is not explicitly overridden in `r.Fields`, `Composition` (and `planSingleResource`) must return an error:
  `resource "<name>": conventions cannot match native Kubernetes kind`
- If a convention exists in `b.Spec.Conventions` but does NOT match any top-level leaf of a native resource in the blueprint, native resources continue to be safely skipped without error (preserving `TestConventionsSkipNativeKinds`).
- The error message must match `docs/dsl.md:418` verbatim.

## Verification

```sh
gofmt -w internal/emit/
make lint && make lint-strict && make test-race
```

## Out of scope

- Modifying `blueprint.Validate` (CRD schema resolution occurs during emission in `internal/emit`).
- Non-Go engines (already handled by `refuseGoTemplateOnlyFeatures`).

## Handover

Branch `CF-109-conventions-native-kind`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
