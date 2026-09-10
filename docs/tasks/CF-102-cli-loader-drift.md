# CF-102 — `cf kinds` and `cf fields` silently fall back to "every cached provider" on blueprint load failure, never warn on uncached sources, and drift from `cf gen`

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: source-only knowledge / inconsistent CLI loader policy) |
| **Closes** | `CF-102 — *(engine)* cf kinds and cf fields silently fall back to "every cached provider" when the blueprint fails to load, and never warn when a declared source is uncached, so they disagree with cf gen and the canvas on the same file.` |
| **Worktree** | `.worktrees/CF-102` on branch `CF-102-cli-loader-drift` |
| **May write** | `cmd/cf/kinds.go`, `cmd/cf/fields.go`, `cmd/cf/explore_test.go` |
| **Merges after** | nothing |

## Symptom

1. When a user runs `cf kinds` or `cf fields` pointing at an invalid or syntax-broken blueprint file (e.g. `cf kinds --blueprint bad.cf.yaml`), `cmd/cf/kinds.go:29-31` and `cmd/cf/fields.go:32-34`:
```go
if _, err := os.Stat(c.Blueprint); err == nil {
    if loaded, err := blueprint.Load(c.Blueprint); err == nil {
        b = loaded
        ...
```
silently ignore the load error and treat it as "no blueprint provided", immediately falling back to `store.List()` ("every cached provider in the cache directory").
Contrast with `cf gen`, which fails immediately with exit 1 if the blueprint cannot be loaded.

2. When a valid blueprint declares a source provider that is not in the schema cache, `cf kinds` and `cf fields` silently omit it without any warning on `os.Stderr`. In contrast, `cmd/cf/options.go:39` (used by `cf serve` and `cf mcp`) prints:
`cf: warning: provider "..." is not in the cache — continuing without it; schemas load on demand`.

3. If the default `doc.cf.yaml` does not exist on disk, running `cf kinds` without arguments should continue discovering all cached providers as expected. But if `--blueprint <file>` is explicitly specified and the file exists but fails to parse, it must return the parse/load error rather than silently masking it by listing everything. Furthermore, declared sources that are missing from cache must print a warning on `os.Stderr`.

## Evidence

In `cmd/cf/kinds.go:29-39` and `cmd/cf/fields.go:32-42`:
A broken blueprint file at `c.Blueprint` satisfies `os.Stat(c.Blueprint) == nil`, but `blueprint.Load` returns an error which is swallowed by `if loaded, err := ...; err == nil`. `b` remains `nil`, `len(refs)` remains 0, and line 42 loads all cached providers silently.

## Acceptance Test

Write this test **first**, verbatim in `cmd/cf/explore_test.go`, and watch it fail before changing production code:

```go
func TestCF102KindsAndFieldsErrorOnInvalidBlueprint(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	badBP := filepath.Join(dir, "bad.cf.yaml")
	if err := os.WriteFile(badBP, []byte("spec: [invalid yaml"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	kindsCmd := &KindsCmd{
		CacheDir:  cacheDir,
		Blueprint: badBP,
	}
	var out bytes.Buffer
	if err := kindsCmd.Run(&out); err == nil {
		t.Fatal("expected kindsCmd.Run to fail on invalid blueprint, got nil")
	}

	fieldsCmd := &FieldsCmd{
		Kind:      "Deployment",
		CacheDir:  cacheDir,
		Blueprint: badBP,
	}
	out.Reset()
	if err := fieldsCmd.Run(&out); err == nil {
		t.Fatal("expected fieldsCmd.Run to fail on invalid blueprint, got nil")
	}
}

func TestCF102KindsAndFieldsWarnOnUncachedSource(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	bpFile := filepath.Join(dir, "doc.cf.yaml")
	bpContent := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  sources:
    - provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0
`
	if err := os.WriteFile(bpFile, []byte(bpContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	kindsCmd := &KindsCmd{
		CacheDir:  cacheDir,
		Blueprint: bpFile,
	}
	var out bytes.Buffer
	// Should succeed (partial index with native kinds) but warn on stderr about uncached provider
	if err := kindsCmd.Run(&out); err != nil {
		t.Fatalf("kindsCmd.Run failed: %v", err)
	}
}
```

## Contract

- In `cmd/cf/kinds.go` and `cmd/cf/fields.go`:
  - If `c.Blueprint` exists on disk (or is non-default and does not exist), loading it via `blueprint.Load(c.Blueprint)` must propagate the load error instead of discarding it.
  - When blueprint sources are processed, if any provider is missing from `store`, print the warning on `os.Stderr`:
    `cf: warning: provider "<provider>" is not in the cache — continuing without it; schemas load on demand`
    matching `cmd/cf/options.go:39`.
  - When `c.Blueprint` default is not found on disk, discovery of all cached providers continues working as before.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- CLI output formatting of kinds/fields.

## Handover

Branch `CF-102-cli-loader-drift`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
