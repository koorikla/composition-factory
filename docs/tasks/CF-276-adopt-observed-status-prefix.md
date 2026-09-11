# CF-276 — adopt drops status wires using standard .observed.resources without $ and emits raw Go template

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: status wire parsing fidelity for Go template adoption) |
| **Closes** | `CF-276 — adopt drops status wires using standard .observed.resources without $ and emits raw Go template` |
| **Worktree** | `.worktrees/CF-276` on branch `CF-276-adopt-observed-status-prefix` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When adopting a Crossplane Composition using `function-go-templating` where resource status references use the standard root context syntax `.observed.resources` (without `$`, such as `{{ (index .observed.resources "test-bucket").resource.status.atProvider.arn }}` or `{{ .observed.resources.test_bucket.resource.status.atProvider.arn }}`):
1. `reObservedStatus` fails to match the expression in annotation wires, spec field wires, and envelope connection secret wires.
2. All three locations fall back to raw Go template strings (`Field{Raw: rawStr}`).
3. The visual canvas draws NO dependency wire between the resources.
4. Emitting to non-Go engines (KCL, Python) hard-fails with:
   `cf: error: resource "test-queue" field "redrivePolicy": raw "{{ (index .observed.resources \"test-bucket\").resource.status.atProvider.arn }}" contains Go-template syntax which is only supported with the go-templating engine (current engine is "kcl")`
5. This violates the Section 1 Round-Trip Rule (`AGENTS.md` §1).

## Mechanism

In `internal/adopt/adopt.go:996`:
```go
reObservedStatus = regexp.MustCompile(`\{\{-?\s*(?:\(index\s+(?:\$\.?observed(?:\.resources)?|\$observed)\s+"([^"]+)"\)|(?:\$\.?observed(?:\.resources)?|\$observed)\.([a-zA-Z0-9_-]+))\.resource\.(status(?:\.atProvider)?|metadata)\.([a-zA-Z0-9_.-]+?)(?:\s*\|\s*quote)?\s*-?\}\}`)
```
Notice `(?:\$\.?observed(?:\.resources)?|\$observed)` requires a mandatory leading `$`. By contrast, `reForEachStatusLoop` on line 1006 already made `$` optional with `\$?`.

## Contract

1. In `internal/adopt/adopt.go`:
   - Update `reObservedStatus` to support optional leading `$` for `.observed.resources` and `$observed.resources`.
2. Verify that status references with `.observed.resources`, `$.observed.resources`, and `$observed.resources` resolve into `blueprint.Field{From: "resources.<name>.status.<field>"}` in:
   - Resource `fields:`
   - Resource `annotations:`
   - Resource `envelope:` (connection secret refs)
3. Verify that adopting compositions using standard `.observed.resources` produces structured resource dependency wires and enables multi-engine emission (Go templating, KCL, Python).

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
1. Adopt a Composition where resource `queue` references `{{ (index .observed.resources "bucket").resource.status.atProvider.arn }}` on `spec.forProvider.redrivePolicy`.
2. Adopt a Composition where `ServiceAccount` references `{{ (index .observed.resources "role").resource.status.atProvider.arn }}` in annotations (`eks.amazonaws.com/role-arn`).
3. Verify:
   - `queue.Fields["redrivePolicy"].From == "resources.bucket.status.atProvider.arn"`.
   - `sa.Annotations["eks.amazonaws.com/role-arn"].From == "resources.role.status.atProvider.arn"`.
   - Neither field has `Raw` set.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-276-adopt-observed-status-prefix`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
