# CF-258 — adopt puts spec.initProvider patches into invalid envelope and drops base initProvider without loss

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (engine scale: round-trip fidelity & schema validity) |
| **Closes** | `CF-258 — adopt puts spec.initProvider patches into invalid envelope and drops base initProvider without loss` (#146) |
| **Worktree** | `.worktrees/CF-258` on branch `CF-258-adopt-initprovider` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Symptom

Adopting a Composition where resources use `base.spec.initProvider` or patches target `spec.initProvider.*` causes two bugs:
1. `base.spec.initProvider` fields are silently dropped without being recorded in `report.Record(...)`.
2. Patches targeting `spec.initProvider.*` are incorrectly routed to `res.Envelope`, producing invalid blueprint envelopes like `initProvider.tags` which are rejected during generation or validation because `initProvider` is outside the CRD envelope schema.

## Mechanism

In `internal/adopt/adopt.go`:
- `resourceFromMap`:
  ```go
  if k == "forProvider" || k == "initProvider" {
      continue
  }
  ```
  `spec.initProvider` is skipped from envelope extraction, but its fields are never extracted or recorded in `report.Record(...)`.
- `applyPatch`:
  ```go
  } else if strings.HasPrefix(toPath, "spec.") {
      targetField := strings.TrimPrefix(toPath, "spec.")
      ...
      res.Envelope[targetField] = blueprint.Field{From: "params." + paramName}
  ```
  Any `toFieldPath` starting with `spec.` (including `spec.initProvider.*`) is treated as an envelope field. In `internal/emit/envelope.go:34` (`checkEnvelopePaths`), envelope paths are validated against `crd.Envelope()`, which only permits `spec` properties excluding `forProvider` and `initProvider`, triggering validation failure.

## Contract

1. In `internal/adopt/adopt.go`, when `spec.initProvider` is present in a resource `base`, each dropped field under `spec.initProvider` must be explicitly recorded in `report.Record(...)` (e.g. `resource.<resName>.initProvider.<key>`).
2. Patches targeting `toFieldPath: spec.initProvider.*` must not be placed into `res.Envelope`. They must be recorded in `report.Record(...)` as unsupported patches.
3. Adoption of Compositions with `initProvider` constructs produces a valid blueprint without invalid envelope entries, and report has true loss reflecting the dropped fields and patches.

## Acceptance Test

Unit tests in `internal/adopt/adopt_test.go`:
- Adopts a Composition with `base.spec.initProvider` and patches targeting `spec.initProvider.tags`.
- Asserts that `res.Envelope` does NOT contain `initProvider` or `initProvider.tags`.
- Asserts that `report.HasTrueLoss()` is true and `report.Drops` records drops for the `initProvider` base fields and patches.
- Asserts that the resulting blueprint passes `bp.Validate()`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-258-adopt-initprovider`, committed, not pushed, not merged.
