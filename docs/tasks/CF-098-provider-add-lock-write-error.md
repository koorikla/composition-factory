# CF-098 — `POST /api/providers` for an already-cached ref answers 200 while discarding the lockfile write error

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: unsafe API contract / silent loss of lockfile reproducibility) |
| **Closes** | `CF-098 — *(engine)* POST /api/providers for an already-cached ref answers 200 while discarding the lockfile write error.` |
| **Worktree** | `.worktrees/CF-098` on branch `CF-098-provider-add-lock-write-error` |
| **May write** | `internal/api/providers.go`, `internal/api/providers_test.go` |
| **Merges after** | nothing |

## Symptom

When `POST /api/providers` is called for a provider reference that is already cached in `srv.Store` (or already in `srv.Providers`), it attempts to record the digest pin in the lockfile (`srv.Lock`).
However, `internal/api/providers.go:178` executes:
```go
_ = l.Write(srv.Lock)
```
and ignores any error from `cache.ReadLock` or `l.Write`.
In contrast, `internal/api/blueprint.go:568-571` treats a lockfile write failure as an error that fails the operation (`return fmt.Errorf("write lock: %w", err)`).
Discarding the write error means a failed lock pin (e.g. read-only filesystem, invalid permissions, or corrupt lock path) answers HTTP 200, leaving `.cf.lock` without the digest that the reproducibility rule (AGENTS.md §1) depends on.

## Evidence

In `internal/api/providers.go:175-180`:
```go
if srv.Lock != "" {
    if l, err := cache.ReadLock(srv.Lock); err == nil {
        l.Set(req.Ref, digest)
        _ = l.Write(srv.Lock)
    }
}
```
If `srv.Lock` points to an unwritable file, `l.Write(srv.Lock)` fails silently and `handleAddProvider` proceeds to return HTTP 200 with `{ "added": ... }`.

## Acceptance test

Write this test **first**, verbatim in `internal/api/providers_test.go`, and watch it fail before changing production code:

```go
func TestCF098AddProviderReportsLockWriteError(t *testing.T) {
	_, store, _ := testHandlerWithStore(t)
	parentDir := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(parentDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parentDir, 0o755) })
	unwritableLock := filepath.Join(parentDir, ".cf.lock")

	idx, _ := BuildIndex(store, []string{testProviderRef}, nil, "")
	srv := &server{
		Store:     store,
		Index:     idx,
		Providers: []string{},
		Lock:      unwritableLock,
		Blueprint: testBlueprintPath(t),
	}
	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`{"ref":%q}`, testProviderRef)
	req := httptest.NewRequest("POST", "/api/providers", strings.NewReader(body))
	srv.handleAddProvider(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected HTTP error when lockfile cannot be written, got 200: %s", rec.Body.String())
	}
}
```

## Contract

- `POST /api/providers` must fail loudly (return HTTP 500 or appropriate 4xx/5xx error) when `srv.Lock` is configured and updating or writing `.cf.lock` fails.
- When `l.Write(srv.Lock)` or `cache.ReadLock(srv.Lock)` fails on an already-cached provider, return an error immediately and do not add the provider to `b.Spec.Sources` or answer 200.
- Existing tests in `internal/api/providers_test.go` must continue to pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- UI palette error toasts (already tested).
- Lockfile pruning during provider removal.

## Handover

Branch `CF-098-provider-add-lock-write-error`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
