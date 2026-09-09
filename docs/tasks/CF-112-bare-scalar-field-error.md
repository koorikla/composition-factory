# CF-112 — A field written as a bare scalar (`engine: postgres`) fails with uninformative Go JSON error naming no resource, field, or line

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: source-only knowledge / uninformative internal error) |
| **Closes** | `CF-112 — *(engine)* A field written as a bare scalar (engine: postgres instead of engine: {value: postgres}) fails with json: cannot unmarshal string into Go value of type blueprint.rawField, naming no resource, field or line.` |
| **Worktree** | `.worktrees/CF-112` on branch `CF-112-bare-scalar-field-error` |
| **May write** | `internal/blueprint/types.go`, `internal/blueprint/load_test.go` |
| **Merges after** | nothing |

## Symptom

When a user writes a resource field as a bare scalar (e.g., `engine: postgres` instead of `engine: {value: postgres}`), `cf gen` and `blueprint.Parse` fail with a raw JSON decoder internal error:
```
cf: error: parse blueprint: json: cannot unmarshal string into Go value of type blueprint.rawField
```
It names no resource, field name, or line number.
Contrast with setting two modes on a field, which produces:
```
cf: error: resource "db" field "engine": set exactly one of from, value, raw or template (got 2)
```

## Evidence

In `internal/blueprint/types.go:518-530`:
`Field.UnmarshalJSON` attempts `dec.Decode(&raw)` where `raw` is a struct expecting an object `{from, value, raw, template}`. If the JSON token is a string, number, or boolean, `dec.Decode` returns:
`json: cannot unmarshal string into Go value of type blueprint.rawField`
(or `cannot unmarshal number...`).

## Acceptance test

Write this test **first**, verbatim in `internal/blueprint/load_test.go`, and watch it fail before changing production code:

```go
func TestCF112BareScalarFieldError(t *testing.T) {
	badYAML := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  resources:
    - name: db
      kind: Instance
      fields:
        engine: postgres
`
	_, err := Parse([]byte(badYAML))
	if err == nil {
		t.Fatal("expected error for bare scalar field, got nil")
	}
	if strings.Contains(err.Error(), "blueprint.rawField") {
		t.Errorf("error exposed Go internals: %v", err)
	}
	if !strings.Contains(err.Error(), "field \"engine\"") && !strings.Contains(err.Error(), "engine") {
		t.Errorf("expected error to name field 'engine', got: %v", err)
	}
	if !strings.Contains(err.Error(), "value:") && !strings.Contains(err.Error(), "{value: postgres}") && !strings.Contains(err.Error(), "bare scalar") && !strings.Contains(err.Error(), "must be a mapping") && !strings.Contains(err.Error(), "expected mapping") {
		t.Errorf("expected error to provide actionable guidance for field mode, got: %v", err)
	}
}
```

## Contract

- In `Field.UnmarshalJSON` (or surrounding decode), if `data` is a bare scalar (string, number, boolean) instead of a JSON object `{...}`, return a user-friendly error explaining that a field must be a mapping with one of `value`, `from`, `raw`, or `template` (e.g. `expected mapping with one of value, from, raw, or template (got scalar ...)`).
- When parsed as a blueprint, the error message should clearly identify the issue rather than exposing `json: cannot unmarshal string into Go value of type blueprint.rawField`.
- All existing valid blueprints and starter examples must continue to parse cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Auto-converting bare scalars to `{value: ...}` (the architecture requires explicit mode declarations).

## Handover

Branch `CF-112-bare-scalar-field-error`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
