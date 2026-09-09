# CF-099 — A `crds:` source whose file is missing is skipped silently

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: missing declared source silently ignored, cards lose schema) |
| **Closes** | `CF-099 — *(engine)* A crds: source whose file is missing is skipped silently: its kinds vanish from the index with no error, no warning and no UI state. [V]` |
| **Worktree** | `.worktrees/CF-099` on branch `CF-099-missing-crds-source-error` |
| **May write** | `internal/api/server.go`, `internal/api/server_test.go`, `internal/api/blueprint.go` |
| **Merges after** | nothing |

## Symptom

When a blueprint declares a `crds:` source pointing to a missing or deleted file (e.g. `crds: manifests/custom-crds.yaml`), `BuildIndex` in `internal/api/server.go` ignores `os.IsNotExist(err)`:
```go
crdData, err := os.ReadFile(p)
if err != nil {
    if os.IsNotExist(err) {
        continue
    }
    return nil, fmt.Errorf("read crds %s: %w", p, err)
}
```
The kinds from that source silently vanish from the index without any error, warning, or UI indication. Cards bound to those kinds lose their schema and become uneditable.

A missing declared `crds:` source must surface an error loudly, matching how an unfetchable provider source surfaces.

## Acceptance Test

Write this test first in `internal/api/server_test.go`:

```go
func TestCF099BuildIndexErrorsOnMissingCRDsSource(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-missing-crds"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: "nonexistent-crds.yaml"},
			},
		},
	}

	_, err := BuildIndex(nil, nil, bp, "/tmp/some-nonexistent-dir")
	if err == nil {
		t.Fatal("expected BuildIndex to return error for missing crds source, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-crds.yaml") {
		t.Fatalf("expected error mentioning missing file path, got: %v", err)
	}
}
```

## Contract

- `BuildIndex` must not silently skip missing `crds:` source files. If `os.ReadFile` fails (including `os.IsNotExist`), `BuildIndex` must return an error naming the file path.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
go test ./internal/api -run TestCF099BuildIndexErrorsOnMissingCRDsSource -v
make lint
make lint-strict
make test-race
```

## Handover

Branch `CF-099-missing-crds-source-error`, committed, not pushed, not merged.
