# CF-468 — Inspector bottom becomes a schema-validated manifest editor with field search; the 848-row list is demoted behind a Manifest/Fields toggle

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `#368` — `CF-468 — Inspector bottom becomes a schema-validated manifest editor with field search; 848-row list demoted behind a Manifest/Fields toggle` |
| **Worktree** | `.worktrees/prefill` on branch `prefill-fields` (slice 3 of `docs/superpowers/specs/2026-09-12-prefilled-fields-design.md`) |
| **May write** | `internal/manifest/` (new), `internal/api/manifest.go` (new), `internal/api/manifest_test.go` (new), `internal/api/server.go`, `internal/api/blueprint.go`, `internal/api/contract_fixtures_test.go`, `internal/api/testdata/contract/manifest.json` (new), `internal/emit/composition.go` (exported wrapper only), `internal/mcp/tools.go`, `internal/mcp/server_test.go`, `docs/mcp.md`, `docs/dsl.md`, `docs/guide.md`, `web-proto/README.md`, `web-proto/index.html`, `web-proto/js/api.js`, `web-proto/js/store.js`, `web-proto/js/types.js`, `web-proto/js/utils.js`, `web-proto/js/regions/output.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/regions/inspector/state.js`, `web-proto/js/regions/inspector/events.js`, `web-proto/js/regions/inspector/manifest.js` (new), `web-proto/js/tour.js`, `playwright.config.js`, `tests/cf468-manifest-editor.spec.js` (new) |
| **Merges after** | CF-467 |

## Symptom

The only way to set a field the essentials form does not cover is to find it among 848
rows whose paths truncate to `spec…` in the pane, with no search. Nested structures are
authored as raw JSON strings. There is no way to see a resource the way it will be emitted
while editing it.

## Evidence

Screenshot on 1335360 (2026-09-12): All view rows read `s… boolean`, `spec… string`; no
search input in `#region-inspector`; `renderResource` prints "expand via All / search".

## Location

`web-proto/js/regions/inspector.js:998-1012` — branch rows + `fields.map(fieldRow)`;
`web-proto/index.html:79-88` — the Required / Set / All segment; `web-proto/js/regions/output.js:975-1035`
— the blueprint Edit tab the manifest editor mirrors; `internal/api/blueprint.go:479-508` —
`handleSetResource` whose validation the new PUT reuses; `internal/emit/composition.go:1590`
— `resolveKind`.

## Acceptance test

Go: `internal/manifest/manifest_test.go` and `internal/api/manifest_test.go` exactly as in
`docs/superpowers/plans/2026-09-12-prefilled-fields.md` Tasks 6 and 8; MCP additions as in
Task 10. Playwright: `tests/cf468-manifest-editor.spec.js` as in Task 11.

**Fails today with:** not run by the brief author; the implementer pastes the first failing
runs into the handover.

## Contract

Design doc §3 verbatim. In short: `GET`/`PUT /api/blueprint/resources/{name}/manifest`
convert between the flat field map and nested manifest YAML with the kind's schema tree as
the grammar authority; unknown key, scalar at a non-leaf, and malformed wrapper return 400
with `error`, `path`, `line`; PUT replaces fields in full and runs the same CRD validation as
`PUT /api/blueprint/resources/{name}`. MCP tools `get_resource_manifest` and
`set_resource_manifest` bridge the same handlers. The inspector defaults to a Manifest view
(read-only highlighted YAML, edit → textarea, Apply/Cancel, Cmd/Ctrl+Enter, Escape, Tab,
snippet insert, error line highlighted, editor survives re-renders), a Manifest / Fields
toggle persisted in localStorage, and a search box that filters the Fields list and lists
schema hits in Manifest view. The Playwright config seeds `cf-insp-view=fields` via
`storageState` so the existing suite runs unchanged.

## Verification

```sh
go test ./internal/manifest/ ./internal/api/ ./internal/mcp/ -count=1
npx playwright test tests/cf468-manifest-editor.spec.js
make lint && make test-race && make test-e2e
```

## Out of scope

Inserting a schema hit into the manifest at the right nesting; "+ element" in the Fields view.

## Handover

Commits on `prefill-fields`; failing and passing runs pasted on the issue.
