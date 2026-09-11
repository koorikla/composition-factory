# CF-290 — adopt drops matching XRDs and binds mismatched XRD in multi-document streams without loss report

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: multi-XRD stream matching and loss reporting in adopt) |
| **Closes** | `CF-290 — adopt drops matching XRDs and binds mismatched XRD in multi-document streams without loss report` |
| **Worktree** | `.worktrees/CF-290` on branch `CF-290-adopt-multi-xrd-stream-binding` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-257` |

## Symptom

In `internal/adopt/adopt.go:209-216`, `Adopt` iterates documents in a multi-document YAML stream and sets `xrdDoc = d` unconditionally, silently dropping any earlier `CompositeResourceDefinition` documents without recording anything in `LossReport`. Subsequently, at lines 341-343, `Adopt` calls `parseXRDDoc(xrdDoc, bp, report)` without validating that `xrdDoc`'s `spec.names.kind` matches the Composition's `spec.compositeTypeRef.kind`. In `internal/adopt/adopt.go:762-777`, `parseXRDDoc` sets `bp.Spec.XRD.Plural` and parses `openAPIV3Schema` parameters into `bp.Spec.XRD.Parameters` from the non-matching XRD.

Exit code is 0, stderr is empty, and `LossReport` contains no dropped XRD entries. The resulting blueprint binds schema and parameters from an unrelated XRD while dropping the matching XRD's schema.

## Mechanism

1. Multi-document scanning sets `xrdDoc = d` for every XRD encountered, keeping only the last document.
2. `parseXRDDoc` does not check whether `spec.names.kind` matches `bp.Spec.XRD.Kind` (derived from the Composition's `spec.compositeTypeRef.kind`).
3. Non-matching or discarded XRDs are not added to `LossReport.DroppedFields` or `LossReport.Unmapped`.

## Contract

1. In multi-document streams with one or more XRDs:
   - Match `CompositeResourceDefinition` documents against the Composition's `spec.compositeTypeRef.kind`.
   - If a matching XRD is found, bind its schema, parameters, and plural to `bp.Spec.XRD`.
   - Any non-matching XRDs present in the stream must be recorded in `bp.LossReport.Unmapped` or `DroppedFields` indicating they were omitted.
   - If an XRD document is provided that does not match the Composition and no matching XRD is present, do NOT bind its parameters or plural to the blueprint's XRD; record it in `LossReport`.
2. Ensure round-trip fidelity when adopting multi-manifest outputs from `kubectl get xrd,composition -o yaml`.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
- Pass a multi-document stream containing `XDatabase` XRD, `XOther` XRD, and a Composition referencing `XDatabase`.
- Verify the blueprint has `XDatabase` parameters (`dbStorage`) and plural (`xdatabases`).
- Verify `unrelatedKey` and `xothers` are not bound.
- Verify `bp.LossReport` records `CompositeResourceDefinition/xothers.example.org` as unmapped/dropped.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-290-adopt-multi-xrd-stream-binding`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
