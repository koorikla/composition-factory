# CF-094 — Server-side blueprint writes emit zero-value noise

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: zero-value noise pollutes YAML committed to git) |
| **Closes** | `CF-094 — Every server-side write of the blueprint fills the file with zero-value noise the editor never shows: conventions: null, templates: null, enum: null, from: "", raw: "", template: "". [V]` |
| **Worktree** | `.worktrees/CF-094` on branch `CF-094-omit-empty-blueprint-fields` |
| **May write** | `internal/blueprint/types.go`, `internal/api/blueprint.go`, `internal/api/blueprint_test.go` |
| **Merges after** | nothing |

## Symptom

Every server-side write of a blueprint (such as loading a starter example via `POST /api/examples/.../load`, editing parameters via `/api/blueprint/parameters`, or saving via `PUT /api/blueprint`) fills the blueprint file on disk with zero-value noise:
```yaml
conventions: null
templates: null
enum: null
from: ""
raw: ""
template: ""
```
Dozens of such lines are written. The drawer's edit view shows a clean form without these zero values, but the actual file on disk (and what the user commits to git) contains unwanted nulls and empty strings.

## Location

- `internal/blueprint/types.go`:
  - `Spec.Templates` and `Spec.Conventions` lack `omitempty` in JSON tags.
  - `Parameter.Enum`, `Parameter.Default`, `Parameter.Description` lack `omitempty` in JSON tags.
  - `Resource.ForEach`, `Resource.When`, `Resource.Provider` lack `omitempty` in JSON tags.
  - `Field.From`, `Field.Value`, `Field.Raw`, `Field.Template` lack `omitempty` in JSON tags.
- `internal/api/blueprint.go`: `marshalBlueprint` marshals the `blueprint.Blueprint` struct to YAML.

## Acceptance Test

Write this test first in `internal/api/blueprint_test.go`:

```go
func TestCF094MarshalBlueprintOmitsEmptyFields(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-clean"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Kind:    "XDatabase",
				Plural:  "xdatabases",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"region":       {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "bucket",
					Kind: "Bucket",
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-north-1"},
					},
				},
			},
		},
	}

	data, err := marshalBlueprint(b)
	if err != nil {
		t.Fatalf("marshalBlueprint: %v", err)
	}
	s := string(data)

	disallowed := []string{
		`from: ""`,
		`raw: ""`,
		`template: ""`,
		`conventions: null`,
		`templates: null`,
		`enum: null`,
	}
	for _, d := range disallowed {
		if strings.Contains(s, d) {
			t.Errorf("marshaled blueprint contains unwanted zero-value string %q:\n%s", d, s)
		}
	}
}
```

## Contract

- `marshalBlueprint` and any server-side write of `blueprint.Blueprint` must omit empty/nil fields:
  - `from: ""`
  - `raw: ""`
  - `template: ""`
  - `conventions: null`
  - `templates: null`
  - `enum: null`
- Blueprints with and without these fields must continue to load, round-trip, and validate identically.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
go test ./internal/api -run TestCF094MarshalBlueprintOmitsEmptyFields -v
make lint
make lint-strict
make test-race
```

## Handover

Branch `CF-094-omit-empty-blueprint-fields`, committed, not pushed, not merged.
