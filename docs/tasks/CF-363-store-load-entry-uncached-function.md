# CF-363 — Store.loadEntry reports uncached function as provider and suggests invalid provider add

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `CF-363 — Store.loadEntry reports uncached function as provider and suggests invalid provider add` (#255) |
| **Worktree** | `.worktrees/CF-363` on branch `CF-363-store-load-entry-uncached-function`, branched from `main` |
| **May write** | `internal/cache/store.go`, `internal/cache/store_test.go` |
| **Merges after** | nothing |

## Defect Summary

When `Store.loadEntry(ref)` (or public accessors such as `Store.Load`, `Store.LoadDigest`, `Store.LoadInputCRDs`, or `Store.LoadInputSchema`) is invoked for an uncached package reference, lines 144-147 in `internal/cache/store.go` format the missing-entry error unconditionally as:
```go
	path := filepath.Join(s.Root, slug(ref), "crds.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provider %q is not in the cache; run: cf provider add %s", ref, ref)
	}
```

When `ref` is a Crossplane function package:
1. It labels the function package as a `"provider"`.
2. It advises the user to run `cf provider add <ref>`.
3. If the user follows this instruction, `cf provider add` fails with exit code 1 (`"package ... is a function package, not a provider (use 'cf function add ...')"`).

`internal/cache/store.go` already defines `isFunctionRef(ref string) bool` at line 219. When `isFunctionRef(ref)` is true, `Store.loadEntry` should instead format:
`function %q is not in the cache; run: cf function add %s`

## Acceptance Test

In `internal/cache/store_test.go`:

```go
func TestStoreLoadUncachedFunctionRefError(t *testing.T) {
	s := New(t.TempDir())
	ref := "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"
	_, err := s.Load(ref)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "run: cf function add") {
		t.Errorf("got error %q, expected suggestion 'run: cf function add'", err.Error())
	}
	if strings.Contains(err.Error(), "provider ") {
		t.Errorf("function ref was called provider in error: %q", err.Error())
	}
}
```

## Contract

In `internal/cache/store.go:loadEntry`:
When `err != nil` reading `path`:
If `isFunctionRef(ref)`:
Return `fmt.Errorf("function %q is not in the cache; run: cf function add %s", ref, ref)`
Else:
Return `fmt.Errorf("provider %q is not in the cache; run: cf provider add %s", ref, ref)`

## Verification

```sh
make lint
make lint-strict
make test-race
go test ./internal/cache -run TestStoreLoadUncachedFunctionRefError -v
```
