# CF-111 — `cf adopt` refuses `--engine kcl` and `--engine python` output with a pipeline collision error instead of naming supported engines

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: source-only knowledge / uninformative failure message) |
| **Closes** | `CF-111 — *(engine)* cf adopt refuses cf's own --engine kcl and --engine python output with spec.pipeline[0].name: "render-resources" collides with the built-in templating step's name, which names the adopter's own construction, not the cause (only function-go-templating and patch-and-transform are adoptable).` |
| **Worktree** | `.worktrees/CF-111` on branch `CF-111-adopt-engine-refusal` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Symptom

When a user runs `cf adopt` on a Composition generated with `--engine kcl` or `--engine python`, adoption fails with:
```
cf: error: adopt composition: validate adopted blueprint: spec.pipeline[0].name: "render-resources" collides with the built-in templating step's name; pick another -- Crossplane requires pipeline step names to be unique
```
This message is misleading and uninformative: the user did not author a custom pipeline with a colliding step name.
Rather, the adopter treated `function-kcl` (or `function-python`) as an unknown pipeline step named `render-resources` and added it to `bp.Spec.Pipeline`, causing `bp.Validate()` to reject the blueprint because `render-resources` is the reserved name of the templating step.

## Evidence

In `internal/adopt/adopt.go:972-992`:
`parsePipelineComposition` handles only:
- `function-go-templating`
- `function-patch-and-transform`
Any other function (including `function-kcl` and `function-python`) falls through to the generic `else` block at `:992-1036`, adding a step with `stepName = "render-resources"` (the default name emitted by `kcl.go` and `python.go`). Then `bp.Validate()` fails at `pipeline.go:63`.

## Acceptance test

Write this test **first**, verbatim in `internal/adopt/adopt_test.go`, and watch it fail before changing production code:

```go
func TestCF111AdoptRefusesKCLAndPythonEnginesClearly(t *testing.T) {
	for _, engine := range []string{"function-kcl", "function-python"} {
		compYAML := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xdatabases.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XDatabase
  pipeline:
    - step: render-resources
      functionRef:
        name: %s
`, engine)
		_, _, err := Adopt([]byte(compYAML), Options{})
		if err == nil {
			t.Fatalf("expected error adopting %s composition, got nil", engine)
		}
		if strings.Contains(err.Error(), "collides with the built-in templating step's name") {
			t.Errorf("expected clear engine refusal for %s, got collision error: %v", engine, err)
		}
		if !strings.Contains(err.Error(), "function-go-templating") || !strings.Contains(err.Error(), engine) {
			t.Errorf("expected error to name %s and supported engines, got: %v", engine, err)
		}
	}
}
```

## Contract

- In `internal/adopt/adopt.go`, when `parsePipelineComposition` encounters an engine function that is not adoptable (such as `function-kcl`, `function-python`, or steps with `render-resources` using non-go-templating engines):
  Return a clean, actionable error explaining that `cf adopt` supports `function-go-templating` and `function-patch-and-transform`, and does not support adopting the specified engine (e.g. `cannot adopt composition with function %q: cf adopt supports function-go-templating and function-patch-and-transform`).
- Do not let the step fall through to `bp.Spec.Pipeline` and trigger a confusing `render-resources` step name collision error.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Adding full AST parsing and adoption for KCL or Python scripts.

## Handover

Branch `CF-111-adopt-engine-refusal`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
