# CF-244 — add_resource silently overwrites resource.name with the name argument where update_resource refuses

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/mcp/tools.go`, `internal/mcp/tools_test.go`

## Background & Problem
In `internal/mcp/tools.go:440-460`:
`addResource` unmarshals the raw `resource` object into `rawMap` and forcefully sets `rawMap["name"] = nameBytes` without checking if `rawMap["name"]` was already present.
If an agent passes a resource object with a `"name"` field inside it (e.g. `{"name": "foo", "resource": {"name": "bar", ...}}`), `add_resource` silently overwrites `"bar"` with `"foo"` and succeeds without warning.
By contrast, `update_resource` forwards the body untouched to `PUT /api/blueprint/resources/{name}`, which checks whether `bodyName != urlPath` and returns 400 `resource name in body "bar" does not match URL path "foo"`.

## Expected Behavior & Contract
When calling `add_resource`:
- If `resource` object contains a `name` property whose value does not match the `name` argument, `add_resource` must refuse the call with an error (`isError: true`):
  `resource name in body <bodyName> does not match name argument <argName>`.

## Acceptance Test
- Test in `internal/mcp/tools_test.go`:
  Calling `add_resource` with argument `name: "resA"` and `resource: {"name": "resB", ...}` returns an error result (`isError: true`) with text indicating the mismatch.
