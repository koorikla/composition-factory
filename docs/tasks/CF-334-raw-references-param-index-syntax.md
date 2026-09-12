# CF-334 — rawReferencesParam misses index syntax, corrupting raw parameter renames and deletes

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Parameter index access in raw fields and templates bypasses delete check and corrupts rename) |
| **Closes** | `#223` — `CF-334 — rawReferencesParam misses index syntax, corrupting raw parameter renames and deletes` |
| **Worktree** | `.worktrees/CF-334` on branch `CF-334-raw-references-param-index-syntax` |
| **May write** | `internal/blueprint/edit.go`, `internal/blueprint/edit_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/blueprint/edit.go`, parameter references in raw templates (`Field.Raw` and `spec.templates`) are scanned and rewritten using `rawReferencesParam` and `rewriteRawParam`.
Because both functions only check dot-notation accesses (`(?:$spec|\.spec|\$params|\.params|params)\.<name>`), Go template index expressions like `(index $spec "name")`, `(index .spec "name")`, or `(index $params "name")`:
1. **Bypass Delete Checks**: `DeleteParameter(name)` allows deleting parameters that are referenced in raw index expressions.
2. **Corrupt Renames**: `RenameParameter(from, to)` ignores index expressions, leaving stale parameter references.

## Acceptance test

```go
// internal/blueprint/edit_test.go
func TestDeleteParameter_RefusesRawIndexSpec(t *testing.T) {
	b := editable()
	b.Spec.XRD.Parameters["tier"] = Parameter{Type: "string"}
	b.Spec.Resources[0].Fields["rawField"] = Field{Raw: `{{ (index $spec "tier") }}`}

	err := b.DeleteParameter("tier")
	if err == nil {
		t.Fatal("DeleteParameter = nil, want refusal when raw field references parameter via index")
	}
	if !strings.Contains(err.Error(), "main-queue") {
		t.Errorf("err = %v, want it to mention main-queue", err)
	}
}

func TestRenameParameter_RewritesRawIndexSpec(t *testing.T) {
	b := editable()
	b.Spec.XRD.Parameters["tier"] = Parameter{Type: "string"}
	b.Spec.Resources[0].Fields["rawField"] = Field{Raw: `{{ (index $spec "tier") }}`}
	b.Spec.Templates = map[string]string{
		"helper": `{{ (index .spec "tier") }}`,
	}

	if err := b.RenameParameter("tier", "ranking"); err != nil {
		t.Fatalf("RenameParameter: %v", err)
	}
	if got := b.Spec.Resources[0].Fields["rawField"].Raw; got != `{{ (index $spec "ranking") }}` {
		t.Errorf("raw field = %q, want {{ (index $spec \"ranking\") }}", got)
	}
	if got := b.Spec.Templates["helper"]; got != `{{ (index .spec "ranking") }}` {
		t.Errorf("template = %q, want {{ (index .spec \"ranking\") }}", got)
	}
}
```

## Contract

1. In `internal/blueprint/edit.go`:
   - Extend `rawReferencesParam` to match Go template index notation with double, single, or backtick quotes, e.g. `index\s+(?:\$spec|\.spec|\$params|\.params|params)\s+["'` + regexp.QuoteMeta(name) + `["']`.
   - Extend `rewriteRawParam` to rewrite both dot-notation and index-notation parameter references.
2. Add acceptance tests in `internal/blueprint/edit_test.go`.
3. Verify all blueprint tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/blueprint/`.

## Handover

Branch `CF-334-raw-references-param-index-syntax`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
