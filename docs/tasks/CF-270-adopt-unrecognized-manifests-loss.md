# CF-270 — cf adopt silently drops unrecognized manifests in multi-document streams without loss reporting

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: loss reporting fidelity for multi-document adopt) |
| **Closes** | `CF-270 — cf adopt silently drops unrecognized manifests in multi-document streams without loss reporting` |
| **Worktree** | `.worktrees/CF-270` on branch `CF-270-adopt-unrecognized-manifests-loss` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When a user passes a multi-document YAML stream (e.g. from `kubectl get -o yaml` or a multi-resource release manifest) to `cf adopt`, any document whose `kind` is not explicitly handled by `Adopt` (such as `Secret`, `ConfigMap`, `ProviderConfig`, or custom workload resources) is silently skipped. `cf adopt` exits with code 0 and produces a Blueprint with no loss comments (`# adopt: dropped ...`) mentioning the omitted manifests.

Users experience silent data loss with zero indication in the adoption report or exit code.

## Mechanism

In `internal/adopt/adopt.go:209-259`:
`Adopt` iterates through parsed documents in a multi-document YAML stream and switches on `kind`:
```go
for _, d := range docs {
    kind, _ := d["kind"].(string)
    switch kind {
    case "Composition":
        compDoc = d
    case "CompositeResourceDefinition":
        xrdDoc = d
    case "EnvironmentConfig":
        envConfigDocs = append(envConfigDocs, d)
    case "Function":
        ...
    case "Configuration":
        ...
    }
}
```
Any document with an unhandled kind (e.g. `Secret`, `ConfigMap`, `ProviderConfig`) hits the default branch (no-op) and is omitted without recording an entry in `LossReport`.

## Contract

1. In `internal/adopt/adopt.go`:
   - In the multi-document iteration loop of `Adopt`, record any unhandled document kind in `LossReport`.
   - Record the document's `kind`, `apiVersion`, and `metadata.name` (e.g. `report.Record("manifest."+kind+"/"+name, "unhandled resource kind omitted from blueprint adoption")`).
2. When non-Crossplane or unhandled manifests are dropped from a multi-document stream:
   - The loss report rendered in the blueprint comments or CLI output must explicitly list the omitted manifests.
   - `cf adopt` must exit with the standard loss exit code (exit 2) when unhandled manifests are omitted, informing the user of the omission.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
1. Feed a multi-document stream containing a `Secret` (`kind: Secret`, `metadata.name: my-secret`) and a valid `Composition` to `Adopt`.
2. Verify that `report.Drops` contains an entry identifying `Secret/my-secret` as an unhandled omitted manifest.
3. Verify that the adopted blueprint comments include `# adopt: dropped manifest.Secret/my-secret`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-270-adopt-unrecognized-manifests-loss`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
