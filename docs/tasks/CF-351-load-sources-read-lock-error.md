# CF-351 — LoadSources silently discards ReadLock errors, generating unpinned artifacts on corrupt lockfiles

## 1. Context & Invariant

In `internal/cache/sources.go:22`, `LoadSources` reads the lockfile via:
```go
lock, _ = ReadLock(filepath.Join(blueprintDir, ".cf.lock"))
```
discarding the returned error. While `cache.ReadLock` treats a missing file as an empty lock (`&Lock{}, nil`), it returns an error when the file exists but contains invalid JSON or merge conflict markers.

Because `LoadSources` ignores the error, `lock` is set to `nil`, and generation proceeds as if no lockfile existed. For example, placing merge conflict markers (`<<<<<<< HEAD`) in `.cf.lock` causes `cf gen` and `cf package` to exit 0 and emit artifacts with unpinned digests without any warning or error.

This violates the Engine Truth §1 Reproducibility guarantee ("Given the same blueprint and provider versions (or .cf.lock), generation must always produce the exact same byte-for-byte outputs"). A corrupt or unparseable lockfile must not silently decay into unpinned generation.

## 2. Requirements & Contract

1. In `internal/cache/sources.go`:
   - Check the error returned by `ReadLock(filepath.Join(blueprintDir, ".cf.lock"))`.
   - If `err != nil`, return `nil, err` immediately.
2. Guard with automated unit tests in `internal/cache/sources_test.go`:
   - `TestLoadSources_CorruptLockfile`: create a temporary directory with a corrupt `.cf.lock` (e.g. invalid JSON or merge conflict markers) and verify `LoadSources` returns an error naming the lockfile/parse failure.
3. Ensure `make lint && make lint-strict && make test-race` passes.

## 3. Verbatim Repro Test

```go
func TestLoadSources_CorruptLockfile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	if err := os.WriteFile(lockPath, []byte("<<<<<<< HEAD\ncorrupt json\n=======\n>>>>>>> branch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0"},
			},
		},
	}

	store := New(t.TempDir())
	_, err := LoadSources(store, bp, dir)
	if err == nil {
		t.Fatal("LoadSources = nil, want error on corrupt .cf.lock")
	}
}
```
