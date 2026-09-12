# CF-329 — rawReferencesResource matches unanchored quoted strings, corrupting unrelated fields on rename

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Quoted strings in raw templates corrupt unrelated fields on resource rename and falsely block deletion) |
| **Closes** | `#218` — `CF-329 — rawReferencesResource matches unanchored quoted strings, corrupting unrelated fields on rename` |
| **Worktree** | `.worktrees/CF-329` on branch `CF-329-raw-references-resource-unanchored-quoted-strings` |
| **May write** | `internal/blueprint/edit.go`, `internal/blueprint/resource_edit_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/blueprint/edit.go`, `rawReferencesResource` and `rewriteRawResource` check bare quoted strings (`"name"`, `'name'`, `` `name` ``) without requiring an index or resource context (such as `(index .observed.resources "name")`).
Any raw template containing a string literal that matches a resource name (such as `{{ if eq $spec.tier "db" }}`) is falsely treated as a cross-resource reference to `db`:
1. `DeleteResource("db")` is blocked with `delete resource "db": its status, metadata, or raw reference is still wired into resources "app"`.
2. `RenameResource("db", "database")` corrupts the condition into `{{ if eq $spec.tier "database" }}`.

## Evidence

In `internal/blueprint/edit.go`:
```go
func rawReferencesResource(raw, name string) bool {
...
	if strings.Contains(raw, `"`+name+`"`) ||
		strings.Contains(raw, `'`+name+`'`) ||
		strings.Contains(raw, "`"+name+"`") {
		return true
	}
...
}

func rewriteRawResource(raw, from, to string) string {
...
	r = strings.ReplaceAll(r, `"`+from+`"`, `"`+to+`"`)
	r = strings.ReplaceAll(r, `'`+from+`'`, `'`+to+`'`)
	r = strings.ReplaceAll(r, "`"+from+"`", "`"+to+"`")
...
}
```

## Acceptance test

```go
// internal/blueprint/resource_edit_test.go
func TestDeleteResource_BlockedByUnrelatedQuotedStringInRaw(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = []Resource{
			{
				Name: "db",
				Kind: "Instance",
			},
			{
				Name: "app",
				Kind: "Deployment",
				Fields: map[string]Field{
					"tier": {
						Raw: `{{ if eq $spec.tier "db" }}primary{{ end }}`,
					},
				},
			},
		}
	})

	err := b.DeleteResource("db")
	if err != nil {
		t.Fatalf("DeleteResource(\"db\") failed: %v", err)
	}
}

func TestRenameResource_CorruptsUnrelatedQuotedStringInRaw(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = []Resource{
			{
				Name: "db",
				Kind: "Instance",
			},
			{
				Name: "app",
				Kind: "Deployment",
				Fields: map[string]Field{
					"tier": {
						Raw: `{{ if eq $spec.tier "db" }}primary{{ end }}`,
					},
				},
			},
		}
	})

	err := b.RenameResource("db", "database")
	if err != nil {
		t.Fatalf("RenameResource failed: %v", err)
	}

	gotRaw := b.Spec.Resources[1].Fields["tier"].Raw
	wantRaw := `{{ if eq $spec.tier "db" }}primary{{ end }}`
	if gotRaw != wantRaw {
		t.Errorf("app raw field corrupted by renaming db: got %q, want %q", gotRaw, wantRaw)
	}
}
```

## Contract

1. In `internal/blueprint/edit.go`:
   - Require resource context for quoted names: instead of bare `strings.Contains(raw, "\""+name+"\"")`, require index/resource context, e.g.:
     `index\s+(?:\$|\$\.|\.)?observed\.resources\s+["'` + regexp.QuoteMeta(name) + `["'` or `index\s+resources\s+...`
   - In `rewriteRawResource`, rewrite only in resource contexts (both `.observed.resources.<name>`/`resources.<name>` and `index ... "<name>"`), never unanchored quoted strings.
2. Add acceptance tests in `internal/blueprint/resource_edit_test.go`.
3. Verify all blueprint tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/blueprint/`.

## Handover

Branch `CF-329-raw-references-resource-unanchored-quoted-strings`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
