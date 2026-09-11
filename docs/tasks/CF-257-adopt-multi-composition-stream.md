# CF-257 — adopt silently drops earlier Compositions in multi-doc stream, while tree adopt mashes them together

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: multi-composition stream and tree disambiguation in adopt) |
| **Closes** | `CF-257 — adopt silently drops earlier Compositions in multi-doc stream, while tree adopt mashes them together` |
| **Worktree** | `.worktrees/CF-257` on branch `CF-257-adopt-multi-composition-stream` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/tree.go`, `internal/adopt/adopt_test.go`, `internal/adopt/tree_test.go` |
| **Merges after** | `CF-270` |

## Symptom

Because a `Blueprint` is a 1-to-1 intermediate representation of a single Crossplane Composition and its XRD, handling multi-Composition inputs currently causes silent data loss and corruption:
1. **Stream / file adopt (`adopt.Adopt`, `internal/adopt/adopt.go:209-216`)**:
   When given multiple Compositions (e.g. from `kubectl get composition -o yaml` or a multi-document file), every Composition before the last is silently overwritten and dropped. Exit code is 0, stderr is empty, and `LossReport` is completely empty.
2. **Directory tree adopt (`adopt.AdoptTree`, `internal/adopt/tree.go:224-282`)**:
   Loops over all `compDocs` in the tree and appends resources from all Compositions into a single `bp.Spec.Resources` list, while locking `bp.Metadata.Name` and `bp.Spec.XRD` to the first Composition. Unrelated resources from different Compositions targeting different XRDs are mashed together into a Frankenstein blueprint under one XRD. Exit code is 0, no loss report.

## Mechanism

1. In `internal/adopt/adopt.go:209-216`:
   ```go
   case "Composition":
       compDoc = d
   ```
   Overwrites `compDoc` on each iteration.
2. In `internal/adopt/tree.go:224-282`:
   Iterates through all `compDocs` and merges all discovered resources into a single slice without checking whether they belong to the same Composition or composite type reference.

## Contract

1. When multiple Compositions are detected in a stream or tree input:
   - If multiple Compositions are present and no specific composition is selected:
     - `cf adopt` must refuse to silently mash or discard them; it should return a clear error listing the discovered Composition names and requiring disambiguation (or an explicit `--composition <name>` selector).
   - If a specific composition is selected or if adopting a single target:
     - Any unselected Compositions must be recorded in `LossReport` as omitted items.
2. Verify that `Adopt` and `AdoptTree` never conflate multiple distinct Compositions into a single composite blueprint or silently drop Compositions without reporting loss.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go` and `internal/adopt/tree_test.go`:
1. Pass a multi-document stream with two Compositions (`comp-a` and `comp-b`) to `Adopt`. Verify it rejects the ambiguous input with an error identifying `comp-a` and `comp-b`.
2. Run `AdoptTree` on a directory containing two separate Composition files. Verify it does not mash resources into a single blueprint and reports an ambiguous multi-composition error or cleanly respects selection.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-257-adopt-multi-composition-stream`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
