# CF-275 — adopt assigns Go template when and forEach guards to next chunk, miswiring or dropping resource conditions

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Go template conditional chunk splitting in adopt) |
| **Closes** | `CF-275 — adopt assigns Go template when and forEach guards to next chunk, miswiring or dropping resource conditions` |
| **Worktree** | `.worktrees/CF-275` on branch `CF-275-adopt-gotemplate-guard-splitting` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When adopting a Composition rendered via `function-go-templating` where document chunks begin with `---` followed by a conditional guard (`{{- if $spec.enableCache }}`) or loop (`{{- range $spec.instances }}`):
1. The `when` / `forEach` guard is not applied to the resource inside the chunk where it was declared.
2. Instead, it is assigned to the *subsequent* resource chunk.
3. If the conditioned resource is the last document in the template, the guard is completely discarded at loop termination, the resource is emitted unconditionally, and the parameter is pruned as orphaned (`# adopt: dropped xrd.parameters.enableCache (parameter orphaned by pruned unknown field dropped)`).

This leads to catastrophic misconfiguration in adopted blueprints: optional resources become unconditional, and unrelated resources inherit erroneous activation guards.

## Mechanism

In `internal/adopt/adopt.go:1667-1725` (`parseGoTemplateBody`):
```go
chunks := reDocSeparator.Split(cleanTmpl, -1)
var nextWhen, nextForEach string
for _, chunk := range chunks {
    trimmedChunk := strings.TrimSpace(chunk)
    if trimmedChunk == "" {
        continue
    }

    when := nextWhen
    forEach := nextForEach
    nextWhen = ""
    nextForEach = ""

    if m := reWhenIfEnvEq.FindStringSubmatch(chunk); len(m) >= 3 {
        nextWhen = ...
    }
    ...
    if m := reForEachLoop.FindStringSubmatch(chunk); len(m) >= 2 {
        nextForEach = fmt.Sprintf("params.%s", m[1])
    }
    ...
    for _, doc := range docs {
        if when != "" {
            res.When = when
        }
        if forEach != "" {
            res.ForEach = forEach
        }
        bp.Spec.Resources = append(bp.Spec.Resources, *res)
    }
}
```
1. `when := nextWhen` reads what the *previous* chunk deferred.
2. Then the regex matches `chunk` and stores the newly found guard in `nextWhen`.
3. The inner loop over `docs` in the *current* chunk applies `when` (which was `nextWhen` from the previous chunk!).
4. Therefore, any guard located inside `chunk` is applied to the NEXT chunk instead of the current one.

## Contract

1. In `internal/adopt/adopt.go` (`parseGoTemplateBody`):
   - Evaluate whether the guard in `chunk` applies to the resource defined in `chunk` itself.
   - If a chunk contains an `if` or `range` block wrapping its YAML manifest, extract and apply `when` / `forEach` directly to the resource(s) parsed from that chunk.
   - Only defer to `nextWhen` / `nextForEach` if the guard appears after the resource manifest (or at the tail of the chunk).
2. Verify that resources wrapped in conditional blocks receive their correct `when` condition.
3. Verify that trailing conditional resources do not lose their guards or cause parameters to be pruned as orphaned.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
1. Adopt a Composition with a Go template containing:
   - Resource `cache` wrapped in `{{- if $spec.enableCache }}`.
   - Resource `deployment` defined unconditionally.
2. Verify:
   - `cache` has `When: "params.enableCache"`.
   - `deployment` has `When: ""` (empty).
   - Parameter `enableCache` is preserved in `bp.Spec.XRD.Parameters`.
3. Adopt a Composition where the only resource is wrapped in a conditional guard; verify the condition is preserved and the parameter is not dropped.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-275-adopt-gotemplate-guard-splitting`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
