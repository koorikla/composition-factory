# CF-277 — adopt drops when guards, forEach loops, and param wires using $.spec or $.observed syntax

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: template parsing fidelity for $.spec and $.observed syntax in adopt) |
| **Closes** | `CF-277 — adopt drops when guards, forEach loops, and param wires using $.spec or $.observed syntax` |
| **Worktree** | `.worktrees/CF-277` on branch `CF-277-adopt-dotspec-dotobserved-syntax` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/xrdless.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When adopting a Crossplane Composition using `function-go-templating` where parameter references, conditional guards, or replication loops access fields via root context `$.spec` or `$.observed.composite.resource.spec`:
1. `when` guards using `{{- if $.spec.enabled }}` or `{{- if eq $.spec.tier "pro" }}` fail to match and are completely discarded. The guarded resource is adopted as **unconditional**, and the parameter is pruned as orphaned (`# adopt: dropped xrd.parameters.enabled (parameter orphaned by pruned unknown field dropped)`).
2. `forEach` loops using `{{- range $i := until (int $.spec.count) }}` fail to match and are completely discarded. The resource is adopted as a single static resource, and `count` is pruned as orphaned.
3. Spec field wires and annotation wires using `{{ $.spec.region }}` or `{{ $.observed.composite.resource.spec.region }}` fail to match `reParamVar` and fall back to raw Go template strings (`Field{Raw: rawStr}`). The canvas draws no parameter wire, and multi-engine generation (`cf gen --engine kcl`) fails with `contains Go-template syntax`.

## Mechanism

In `internal/adopt/adopt.go` and `internal/adopt/xrdless.go`:
```go
reParamVar        = regexp.MustCompile(`\{\{-?\s*(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+?)(?:\s*\|\s*quote)?\s*-?\}\}`)
reWhenIfSimple    = regexp.MustCompile(`\{\{-?\s*if\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
reWhenIfEq        = regexp.MustCompile(`\{\{-?\s*if\s+eq\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+"([^"]+)"\s*-?\}\}`)
reWhenIfNe        = regexp.MustCompile(`\{\{-?\s*if\s+ne\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+"([^"]+)"\s*-?\}\}`)
reForEachLoop     = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s+(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\)\s*-?\}\}`)
reEvidenceAnySpec = regexp.MustCompile(`(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)`)
```
All six regexes require `$spec` (no dot) or `.spec` (no leading `$`). Neither `$.spec` nor `$.observed.composite.resource.spec` is accepted because they contain both `$` and `.`.

## Contract

1. In `internal/adopt/adopt.go` and `internal/adopt/xrdless.go`:
   - Update `reParamVar`, `reWhenIfSimple`, `reWhenIfEq`, `reWhenIfNe`, `reForEachLoop`, and `reEvidenceAnySpec` to match `$.spec` and `$.observed.composite.resource.spec` in addition to `$spec`, `.spec`, and `.observed.composite.resource.spec`.
2. Ensure `when` conditions, `forEach` loops, and `params.<name>` wires are properly generated and declared on the adopted `Blueprint`.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
1. Adopt a Composition with:
   - Resource `conditioned` guarded by `{{- if $.spec.enabled }}`
   - Resource `replicated` looped by `{{- range $i := until (int $.spec.count) }}`
   - Resource `wired` with `spec.forProvider.region: "{{ $.spec.region }}"`
2. Assert:
   - `conditioned.When == "params.enabled"`
   - `replicated.ForEach == "params.count"`
   - `wired.Fields["region"].From == "params.region"`
   - `bp.Spec.XRD.Parameters` contains `enabled`, `count`, and `region`.
