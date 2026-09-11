# CF-315 — validateSchemaNode skips properties and required validation when additionalProperties is declared

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Strict CRD schema validation bypassed when additionalProperties is declared) |
| **Closes** | `#204` — `CF-315 — validateSchemaNode skips properties and required validation when additionalProperties is declared` |
| **Worktree** | `.worktrees/CF-315` on branch `CF-315-validate-schema-node-additional-properties` |
| **May write** | `internal/emit/render_validate.go`, `internal/emit/render_validate_test.go` |
| **Merges after** | `nothing` |

## Symptom

`validateSchemaNode` early returns when `additionalProperties` is declared on an object schema, bypassing required field checks and declared property schema validation.

## Evidence

In `internal/emit/render_validate.go:448-458`, object validation checks `additionalProperties` before declared properties and required fields:
```go
if addProps, ok := propSchema["additionalProperties"]; ok && addProps != false && addProps != nil {
	if addPropsMap, isMap := addProps.(map[string]any); isMap {
		for i := 0; i < len(valNode.Content); i += 2 {
			kNode := valNode.Content[i]
			vNode := valNode.Content[i+1]
			childPath := fmt.Sprintf("%s[%s]", path, kNode.Value)
			errs = append(errs, validateSchemaNode(vNode, addPropsMap, childPath, resourceName, kind, where, b)...)
		}
	}
	return errs
}
```

When an object schema defines `additionalProperties` (e.g. `true` or a schema map) alongside `properties` and `required`:
- The block returns immediately (`return errs`), bypassing checking missing `required` fields and validating child fields against declared `properties[kName]`.
- If `additionalProperties: true`, `isMap` is false, so all validation is skipped entirely.
- If `additionalProperties` is a schema map, declared properties with distinct schemas are validated against `addPropsMap`, producing false positives.

## Acceptance test

```go
// internal/emit/render_validate_test.go
func TestValidateRenderedObjectWithPropertiesAndAdditionalProperties(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:  "example.org",
			Kind:   "ExtensibleConfig",
			Plural: "extensibleconfigs",
			Scope:  "Namespaced",
			Versions: []schema.Version{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Properties: map[string]any{
						"spec": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"forProvider": map[string]any{
									"type": "object",
									"required": []any{"name"},
									"properties": map[string]any{
										"name": map[string]any{"type": "string"},
										"port": map[string]any{"type": "integer"},
									},
									"additionalProperties": true,
								},
							},
						},
					},
				},
			},
		},
	}

	stream := `---
apiVersion: example.org/v1alpha1
kind: ExtensibleConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-cfg
spec:
  forProvider:
    port: "not-an-int"
`
	err := ValidateRendered([]byte(stream), crds)
	if err == nil {
		t.Fatal("expected validation error for invalid port type and missing required name, got nil")
	}
}
```

**Fails today with:**
```
expected validation error for invalid port type and missing required name, got nil
```

## Contract

1. In `internal/emit/render_validate.go`:
   - `validateSchemaNode` must check declared `required` fields and validate child nodes against declared `properties` schemas even when `additionalProperties` is present.
   - If `additionalProperties` is present as a schema map, only properties that are NOT declared under `properties` should be validated against `additionalProperties`.
   - If `additionalProperties: false`, reject any property not declared in `properties`.
2. Ensure existing rendered manifest validation continues to pass without regression.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/emit/`.

## Handover

Branch `CF-315-validate-schema-node-additional-properties`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
