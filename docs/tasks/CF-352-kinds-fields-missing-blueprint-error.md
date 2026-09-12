# CF-352 — cf kinds and cf fields silently exit 0 on missing blueprint when filename matches doc.cf.yaml

## 1. Context & Invariant

In `cmd/cf/kinds.go:28` and `cmd/cf/fields.go:32`, `isDefault` is defined as:
```go
isDefault := c.Blueprint == "" || c.Blueprint == "doc.cf.yaml" || filepath.Base(c.Blueprint) == "doc.cf.yaml"
```

Because `filepath.Base(...) == "doc.cf.yaml"` matches any path ending in `doc.cf.yaml` (e.g. `/missing/dir/doc.cf.yaml`), any explicitly provided path with that filename is treated as the default blueprint. When the specified file does not exist, `os.Stat` fails, `!isDefault` is false, and the error branch is skipped without reporting file-not-found, exiting 0. In contrast, passing `--blueprint /missing/dir/other.yaml` correctly fails with exit 1 (`cf: error: read blueprint: open ...: no such file or directory`).

This silently masks typos in user/script paths and prevents declared provider sources from being loaded.

The fix must only treat the default flag value in the working directory as optional, ensuring explicit paths that do not exist fail loudly with an error regardless of their filename.

## 2. Requirements & Contract

1. In `cmd/cf/kinds.go` and `cmd/cf/fields.go`:
   - Redefine `isDefault := c.Blueprint == "" || c.Blueprint == "doc.cf.yaml"` (remove `filepath.Base(c.Blueprint) == "doc.cf.yaml"`).
   - Ensure that when `c.Blueprint` is set to any explicit non-default path (or non-empty explicit path), if `os.Stat` fails or `blueprint.Load` fails, an error is returned.
2. Guard with automated unit tests in `cmd/cf/kinds_test.go` and/or `cmd/cf/fields_test.go`:
   - Verifying that running `KindsCmd` and `FieldsCmd` with `--blueprint /nonexistent/dir/doc.cf.yaml` returns an error rather than exiting 0.
3. Ensure `make lint && make lint-strict && make test-race` passes.

## 3. Verbatim Repro Test

```go
func TestKindsCmd_MissingExplicitBlueprintWithDocName(t *testing.T) {
	cmd := &KindsCmd{
		Blueprint: filepath.Join(t.TempDir(), "nonexistent", "doc.cf.yaml"),
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatal("KindsCmd.Run = nil, want error on nonexistent explicit blueprint path ending in doc.cf.yaml")
	}
}

func TestFieldsCmd_MissingExplicitBlueprintWithDocName(t *testing.T) {
	cmd := &FieldsCmd{
		Blueprint: filepath.Join(t.TempDir(), "nonexistent", "doc.cf.yaml"),
		Kind:      "Queue",
	}
	var buf bytes.Buffer
	err := cmd.Run(&buf)
	if err == nil {
		t.Fatal("FieldsCmd.Run = nil, want error on nonexistent explicit blueprint path ending in doc.cf.yaml")
	}
}
```
