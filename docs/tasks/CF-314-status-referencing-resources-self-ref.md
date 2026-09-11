# CF-314 — StatusReferencingResources includes the target resource on self raw references, blocking deletion

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: DeleteResource fails when a resource contains a self-referencing raw string) |
| **Closes** | `#203` — `CF-314 — StatusReferencingResources includes the target resource on self raw references, blocking deletion` |
| **Worktree** | `.worktrees/CF-314` on branch `CF-314-status-referencing-resources-self-ref` |
| **May write** | `internal/blueprint/edit.go`, `internal/blueprint/resource_edit_test.go` |
| **Merges after** | `nothing` |

## Symptom

`StatusReferencingResources` includes the target resource itself when evaluating self-referential raw expressions, causing `DeleteResource` and `DELETE /api/blueprint/resources/{name}` to fail with `delete resource "X": its status, metadata, or raw reference is still wired into resources "X"`.

## Evidence

In `internal/blueprint/edit.go:186-202`:
```go
for _, r := range b.Spec.Resources {
	if target, _, ok := StatusRef(r.ForEach); ok && target == name {
		refs = append(refs, r.Name)
		continue
	}
	if anyStatusFrom(r.Fields, name) || anyStatusFrom(r.Annotations, name) || anyStatusFrom(r.Envelope, name) {
		refs = append(refs, r.Name)
	}
}
```
`StatusReferencingResources(name)` does not check `if r.Name == name { continue }`. When a resource defines a raw expression containing its own name (e.g., `Raw: "\"main-queue\""` in fields, annotations, or envelope), `anyStatusFrom` delegates to `rawReferencesResource(f.Raw, name)` which matches `strings.Contains(raw, "\""+name+"\"")`. Because `name` matches the resource itself, `StatusReferencingResources` appends the target resource to `refs`.

In `DeleteResource(name)` (`internal/blueprint/edit.go:408-415`):
```go
if refs := b.StatusReferencingResources(name); len(refs) > 0 {
    ...
    return fmt.Errorf("delete resource %q: its status, metadata, or raw reference is still wired into resources %s",
        name, strings.Join(quoted, ", "))
}
```
The deletion is blocked because the resource references itself.

## Acceptance test

```go
// internal/blueprint/resource_edit_test.go
func TestDeleteResourceAllowsSelfRawReference(t *testing.T) {
	b := wiredBlueprint(func(b *Blueprint) {
		b.Spec.Resources[1].Fields["queueUrl"] = Field{Value: "static"}
		b.Spec.Resources[0].Fields["selfTag"] = Field{
			Raw: `"main-queue"`,
		}
	})

	refs := b.StatusReferencingResources("main-queue")
	for _, r := range refs {
		if r == "main-queue" {
			t.Errorf("StatusReferencingResources returned target resource itself: %v", refs)
		}
	}

	if err := b.DeleteResource("main-queue"); err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}
}
```

**Fails today with:**
```
StatusReferencingResources returned target resource itself: [main-queue]
DeleteResource failed: delete resource "main-queue": its status, metadata, or raw reference is still wired into resources "main-queue"
```

## Contract

1. In `internal/blueprint/edit.go`:
   - In `StatusReferencingResources(name string)`, skip the resource itself (`if r.Name == name { continue }`). The purpose of `StatusReferencingResources` is to identify *other* resources that would be broken if `name` is deleted or renamed.
2. `DeleteResource(name)` must succeed when a resource contains self-referencing raw expressions or status refs, provided no *other* resource references it.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/blueprint/`.

## Handover

Branch `CF-314-status-referencing-resources-self-ref`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
