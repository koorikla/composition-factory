# CF-304 — handleImportBlueprint ignores If-Match header, overwriting concurrent edits when persisting

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: concurrent edits overwritten without OCC protection during import) |
| **Closes** | `#192` — `CF-304 — handleImportBlueprint ignores If-Match header, overwriting concurrent edits when persisting` |
| **Worktree** | `.worktrees/CF-304` on branch `CF-304-api-import-if-match` |
| **May write** | `internal/api/blueprint.go`, `internal/api/blueprint_test.go` |
| **Merges after** | `CF-302` |

## Symptom

When a client imports a blueprint via `POST /api/blueprint/import` and provides an `If-Match` header to guard against stomping concurrent edits, `handleImportBlueprint` ignores the header and writes the imported blueprint to disk.
Concurrent changes made between when the client checked out the blueprint and when they imported are silently overwritten, returning HTTP 200 instead of HTTP 412 Precondition Failed.

## Evidence

In `internal/api/blueprint.go:202-255`:
```go
func (srv *server) handleImportBlueprint(w http.ResponseWriter, r *http.Request) {
...
	b, err := blueprint.ParseAny(body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
...
	if err := writeBlueprintFile(srv.Blueprint, b); err != nil {
...
```
Unlike `handlePutBlueprint` (lines 146-155), `handleImportBlueprint` never inspects `r.Header.Get("If-Match")`.

## Acceptance test

```go
// internal/api/blueprint_test.go
func TestCF304_ImportBlueprint_IfMatchStaleRejected(t *testing.T) {
	dir := t.TempDir()
	bpPath := filepath.Join(dir, "blueprint.yaml")
	initial := []byte("apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\nmetadata:\n  name: initial\nspec:\n  resources: []\n")
	if err := os.WriteFile(bpPath, initial, 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, bpPath)
	importedYAML := []byte("apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\nmetadata:\n  name: imported\nspec:\n  resources: []\n")

	req := httptest.NewRequest(http.MethodPost, "/api/blueprint/import", bytes.NewReader(importedYAML))
	req.Header.Set("If-Match", "\"stale-or-mismatched-etag\"")
	w := httptest.NewRecorder()

	srv.handleImportBlueprint(w, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected status 412 Precondition Failed, got %d", w.Code)
	}

	// Verify disk contents unchanged
	cur, _ := os.ReadFile(bpPath)
	if !bytes.Equal(cur, initial) {
		t.Fatalf("expected blueprint file to remain unchanged on precondition failure")
	}
}
```

**Fails today with:**
```
expected status 412 Precondition Failed, got 200
```

## Contract

1. In `internal/api/blueprint.go`:
   - Under `srv.mu.Lock()` in `handleImportBlueprint`:
   - If `ifMatch := r.Header.Get("If-Match"); ifMatch != ""` and `srv.Blueprint != ""`:
     - Load current blueprint from disk (`srv.loadBlueprintLocked()`).
     - Compare ETag via `etagMatches(ifMatch, etagFor(curBytes))`.
     - If it does not match, return `writeJSONError(w, http.StatusPreconditionFailed, "precondition failed: If-Match header does not match current blueprint revision")` and abort without writing to disk.
2. If `If-Match` matches (or is omitted for backwards compatibility), proceed with import.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- MCP tools (`internal/mcp`).

## Handover

Branch `CF-304-api-import-if-match`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
