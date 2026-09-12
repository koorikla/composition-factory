# CF-325 — rawReferencesResource matches prefix-sharing resource names, corrupting and blocking raw references

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Renaming resources corrupts prefix-sharing raw references; deleting resources is falsely blocked) |
| **Closes** | `#214` — `CF-325 — rawReferencesResource matches prefix-sharing resource names, corrupting and blocking raw references` |
| **Worktree** | `.worktrees/CF-325` on branch `CF-325-raw-references-resource-prefix-matching` |
| **May write** | `internal/blueprint/edit.go`, `internal/blueprint/resource_edit_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/blueprint/edit.go`, `rawReferencesResource` and `rewriteRawResource` perform un-delimited substring searches for `.observed.resources.<name>` and `$observed.resources.<name>`.
When a resource name is a prefix of another resource's name (e.g. `main` and `main-queue`):
1. **RenameResource Corruption**: Renaming `main` to `primary` causes `.observed.resources.main-queue...` to be rewritten to `.observed.resources.primary-queue...`, corrupting unrelated references.
2. **DeleteResource Lockout**: Deleting `main` causes `StatusReferencingResources("main")` to falsely match `.observed.resources.main-queue...`, claiming `main` is still wired and refusing deletion.

## Evidence

In `internal/blueprint/edit.go`:
```go
func rawReferencesResource(raw, name string) bool {
	if raw == "" || name == "" {
		return false
	}
	return strings.Contains(raw, `"`+name+`"`) ||
		strings.Contains(raw, `'`+name+`'`) ||
		strings.Contains(raw, "`"+name+"`") ||
		strings.Contains(raw, "resources."+name+".") ||
		strings.Contains(raw, "resources."+name+" ") ||
		strings.Contains(raw, "resources."+name+"}") ||
		strings.Contains(raw, ".observed.resources."+name) ||
		strings.Contains(raw, "$observed.resources."+name)
}
```
`strings.Contains(raw, ".observed.resources."+name)` matches `name = "main"` inside `.observed.resources.main-queue`.
Similarly, `rewriteRawResource` uses `strings.ReplaceAll(r, ".observed.resources."+from, ".observed.resources."+to)`.

## Acceptance test

```go
// internal/blueprint/resource_edit_test.go
func TestRenameResourceDoesNotRewritePrefixSharingRawReferences(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ .observed.resources.main-queue.resource.status.url }}`,
		}
	})

	if err := b.RenameResource("main", "primary"); err != nil {
		t.Fatalf("RenameResource: %v", err)
	}

	got := b.Spec.Resources[1].Fields["rawField"].Raw
	want := `{{ .observed.resources.main-queue.resource.status.url }}`
	if got != want {
		t.Errorf("raw field = %q, want %q", got, want)
	}
}

func TestDeleteResourceDoesNotRefuseOnPrefixSharingRawReference(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources = append(b.Spec.Resources, Resource{
			Name: "main", Kind: "Queue", Fields: map[string]Field{},
		})
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "static"}
		b.Spec.Resources[1].Fields["rawField"] = Field{
			Raw: `{{ .observed.resources.main-queue.resource.status.url }}`,
		}
	})

	if err := b.DeleteResource("main"); err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}
}
```

## Contract

1. In `internal/blueprint/edit.go`:
   - Enforce boundary delimiters on resource references in `rawReferencesResource` and `rewriteRawResource` (similar to how `rawReferencesParam` uses boundary checks or regex). Specifically, after `.observed.resources.<name>` or `$observed.resources.<name>`, the next character must be a non-resource-name character (e.g. `.`, ` `, `}`, `"`, `'`, `)`, or end of string), not part of an identifier like `-` or alphanumeric characters.
   - Also ensure `resources.<name>` in `rawReferencesResource` requires a delimiter.
2. Verify all unit tests in `internal/blueprint/` pass without regressions.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/blueprint/`.

## Handover

Branch `CF-325-raw-references-resource-prefix-matching`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
