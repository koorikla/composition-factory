# CF-260 — RenameParameter rejects renaming an object parameter when an annotation wires from a nested member

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: blueprint mutation & validation consistency) |
| **Closes** | `CF-260 — RenameParameter rejects renaming an object parameter when an annotation wires from a nested member` |
| **Worktree** | `.worktrees/CF-260` on branch `CF-260-rename-param-nested-annotation` |
| **May write** | `internal/blueprint/edit.go`, `internal/blueprint/edit_test.go` |
| **Merges after** | nothing |

## Symptom

Renaming an object parameter (e.g. `tags` with member `env`) when a resource annotation wires to a nested property (`params.tags.env`) fails with HTTP 400 Bad Request / error:
```
rename parameter "tags" to "labels": resource "<name>" annotation "<key>": references unknown parameter "tags"
```

## Mechanism

In `internal/blueprint/edit.go:485-489` inside `RenameParameter`:
```go
// Annotation froms too, for exactly the same reason.
for key, f := range r.Annotations {
	if f.From == oldRef {
		f.From = newRef
		cp.Spec.Resources[i].Annotations[key] = f
	}
	if f.Raw != "" && rawReferencesParam(f.Raw, from) {
		f.Raw = rewriteRawParam(f.Raw, from, to)
		cp.Spec.Resources[i].Annotations[key] = f
	}
}
```
Unlike `r.Fields` (line 461) and `r.Envelope` (line 475) which call `renameParamRef(f.From, oldRef, newRef)` to handle prefix matching on nested paths (`params.<from>.<member>` -> `params.<to>.<member>`), annotations only check exact string equality `f.From == oldRef`. Any annotation referencing a nested property (`params.tags.env`) is left unmodified with the old parameter name, causing the subsequent `cp.Validate()` check to reject the mutation.

## Contract

1. In `internal/blueprint/edit.go`, annotation `From` references must use `renameParamRef(f.From, oldRef, newRef)` so that nested parameter references in annotations are rewritten consistently with fields and envelope mappings.
2. Renaming an object parameter whose nested properties are wired into resource annotations must succeed and update both top-level parameter declarations and all nested annotation wire paths.
3. Unit test in `internal/blueprint/edit_test.go` verifies renaming an object parameter with nested annotation references updates the annotation `From` and produces a valid blueprint.

## Acceptance Test

Unit test in `internal/blueprint/edit_test.go`:
Create a blueprint with parameter `tags` (type object, properties `env: {type: string}`) and a resource annotation `"deploy.environment": {From: "params.tags.env"}`. Call `b.RenameParameter("tags", "labels")`. Assert error is nil, `spec.xrd.parameters["labels"]` exists, `spec.xrd.parameters["tags"]` does not, and `res.Annotations["deploy.environment"].From` is `"params.labels.env"`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-260-rename-param-nested-annotation`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
