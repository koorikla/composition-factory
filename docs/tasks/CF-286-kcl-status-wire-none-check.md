# CF-286 — KCL emitter uses truthiness check on status wires instead of None check, dropping zero and false values

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: KCL status wire conditional emission) |
| **Closes** | `CF-286 — KCL emitter uses truthiness check on status wires instead of None check, dropping zero and false values` |
| **Worktree** | `.worktrees/CF-286` on branch `CF-286-kcl-status-wire-none-check` |
| **May write** | `internal/emit/kcl.go`, `internal/emit/kcl_test.go` |
| **Merges after** | |

## Summary
In `internal/emit/kcl.go`, status wire conditions are generated as:
```kcl
if ocds?["res"]?.Resource?.status?.path:
    target = ocds?["res"]?.Resource?.status?.path
```
In KCL, conditional expressions `if <expr>:` evaluate boolean truthiness. Under KCL semantics, `0`, `False`, `""`, `[]`, and `{}` are all falsy. Consequently, when an observed resource status field contains a valid falsy value (e.g. `status.ready: false`, `status.delaySeconds: 0`, `status.replicaCount: 0`), KCL evaluates the guard to false and silently omits the assignment at runtime.

In contrast:
- `internal/emit/python.go` uses `if ... is not None:` (e.g., line 201, line 353, line 584).
- `internal/emit/kcl.go` itself uses `!= None:` for parameter guards in `kclNestedParamGuard` (line 253).

## Affected Locations
- `internal/emit/kcl.go:86`
- `internal/emit/kcl.go:113`
- `internal/emit/kcl.go:249`
- `internal/emit/kcl.go:462`

## Steps to Reproduce
1. Create a blueprint with a status wire from an observed resource status field (e.g. `status.ready` or `status.count` or `status.atProvider.status`).
2. Run `cf gen --engine kcl`.
3. Inspect the emitted `kcl.yaml`.
4. Observe:
   ```kcl
   if ocds?["my-res"]?.Resource?.status?.ready:
       dxr.Resource.status.ready = ocds?["my-res"]?.Resource?.status?.ready
   ```
   When `status.ready` is `False`, the condition evaluates to `False` and Crossplane never sets `status.ready` on the composite resource.

## Expected Behavior
The guard should check for existence/non-null:
```kcl
if ocds?["my-res"]?.Resource?.status?.ready != None:
    dxr.Resource.status.ready = ocds?["my-res"]?.Resource?.status?.ready
```

## Severity
`severity:P1` — Multi-engine parity defect and silent data loss at runtime for booleans, zeros, and empty strings.
