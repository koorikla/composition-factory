# CF-369 — Blueprint.Validate accepts Go-template features and environment under non-Go engines

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-369 — Blueprint.Validate accepts Go-template features and environment under non-Go engines` (#261) |
| **Worktree** | `.worktrees/CF-369` on branch `CF-369-validate-engine-features`, branched from `CF-394-xrd-param-type-switch-clean` |
| **May write** | `internal/blueprint/`, `cmd/cf/` |
| **Merges after** | CF-394 |

## Symptom

`Blueprint.Validate()` in `internal/blueprint` permits `spec.templates`, `spec.conventions`, and resource field/annotation `template:` references when `spec.emit.engine` is `kcl` or `python`, as well as `spec.environment` when engine is `kcl`. These are later rejected by `internal/emit` during generation, causing blueprints accepted by `cf package` and `PUT /api/blueprint` to fail at emit time.

## Contract

1. In `internal/blueprint/templates.go`:
   If `b.Engine() != EngineGoTemplating`:
   - If `len(b.Spec.Templates) > 0`, return `fmt.Errorf("spec.templates: engine %q does not support template: blocks", b.Engine())`.
   - If `len(b.Spec.Conventions) > 0`, return `fmt.Errorf("spec.conventions: engine %q does not support template: conventions", b.Engine())`.
2. In `internal/blueprint/validate_resources.go` and `internal/blueprint/annotations.go`:
   When `f.Template != ""` or `a.Template != ""`, check `if b.Engine() != EngineGoTemplating` and return error matching `internal/emit/plan.go`:
   `fmt.Errorf("resource %q field %q: engine %q does not support template: fields", ...)` and
   `fmt.Errorf("resource %q annotation %q: engine %q does not support template: fields", ...)`.
3. In `internal/blueprint/validate_environment.go`:
   When `len(b.Spec.Environment) > 0 && b.Engine() == EngineKCL`, return `fmt.Errorf("spec.environment: engine %q does not support spec.environment", b.Engine())`.
4. Guard with unit tests in `internal/blueprint`.
5. Pass all gates: `make lint && make lint-strict && make test-race && make test-docker`.
