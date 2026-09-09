# CF-110 — Resource routes accept unknown kind or misspelled fields and persist them

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: per-resource mutation bypasses schema validation, persisting invalid blueprints) |
| **Closes** | `CF-110 — *(engine)* POST /api/blueprint/resources, PUT /api/blueprint/resources/{name} and the MCP add_resource/update_resource tools accept an unknown kind or a misspelt field path, answer success and persist it; PUT /api/blueprint on the same server rejects the same content with the nearest-match error. [V]` |
| **Worktree** | `.worktrees/CF-110` on branch `CF-110-validate-resource-endpoints` |
| **May write** | `internal/api/blueprint.go`, `internal/api/blueprint_test.go` |
| **Merges after** | nothing |

## Symptom

When a user or agent creates or updates a resource via `POST /api/blueprint/resources`, `PUT /api/blueprint/resources/{name}`, or the MCP `add_resource`/`update_resource` tools:
- A typo'd kind (`kind: "Instanze"`) or misspelled field path (`fields: {"instanceClas": ...}`) is accepted with HTTP 200 and written to disk.
- In contrast, submitting the exact same document to `PUT /api/blueprint` rejects it with HTTP 400 and a nearest-match suggestion.
- Because the per-resource routes persist the invalid document, subsequent generate calls fail until the file is hand-edited.

The per-resource routes must validate the resulting blueprint against the CRD schemas just like `PUT /api/blueprint` does.

## Acceptance Test

Write this test first in `internal/api/blueprint_test.go`:

```go
func TestCF110ResourceEndpointsRejectUnknownKindAndField(t *testing.T) {
	// 1. POST /api/blueprint/resources with unknown kind "Instanze" must return HTTP 400
	// 2. PUT /api/blueprint/resources/{name} with misspelled field "instanceClas" must return HTTP 400 with nearest match
}
```

## Contract

- In `internal/api/blueprint.go`, validate the modified blueprint against CRDs before persisting it on per-resource mutation routes (or inside `persistBlueprint` / `mutate`).
- If validation fails against CRDs (e.g. unknown kind or unknown field), return HTTP 400 Bad Request with the validation error message and do not persist the invalid state.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
go test ./internal/api -run TestCF110 -v
make lint
make lint-strict
make test-race
```

## Handover

Branch `CF-110-validate-resource-endpoints`, committed, not pushed, not merged.
