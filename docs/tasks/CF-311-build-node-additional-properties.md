# CF-311 — buildNode treats additionalProperties on objects as a map leaf, dropping all declared properties

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Declared OpenAPI properties dropped from schema tree when additionalProperties is specified) |
| **Closes** | `#200` — `CF-311 — buildNode treats additionalProperties on objects as a map leaf, dropping all declared properties` |
| **Worktree** | `.worktrees/CF-311` on branch `CF-311-build-node-additional-properties` |
| **May write** | `internal/schema/tree.go`, `internal/schema/tree_test.go` |
| **Merges after** | `nothing` |

## Symptom

`buildNode` in `internal/schema/tree.go:217-220` inspects `raw["additionalProperties"]` and immediately classifies any object containing this key as a `map` scalar leaf (`n.Type = "map"`), returning early without parsing `raw["properties"]`. This drops all declared child properties whenever an OpenAPI v3 schema specifies `additionalProperties: false` (standard strict schema in Kubernetes CRDs to disallow unknown fields) or defines an extensible object with both explicit properties and an `additionalProperties` schema.

## Evidence

In `internal/schema/tree.go:214-223`:
```go
	switch n.Type {
	case "object":
		// additionalProperties means a map of scalars: a leaf, not a branch.
		if _, isMap := raw["additionalProperties"]; isMap {
			n.Type = "map"
			return n
		}
		if props, ok := raw["properties"].(map[string]any); ok {
			n.Children = BuildTree(props, stringSlice(raw["required"]))
		}
```

1. `_, isMap := raw["additionalProperties"]` evaluates to `true` whenever the key `"additionalProperties"` is present in the schema map, regardless of its value (`false`, `true`, or a schema map).
2. In Kubernetes CRDs and OpenAPI v3 schemas, `additionalProperties: false` is standard practice to disallow undeclared fields. But because the key is present, `buildNode` overrides `n.Type = "map"` and returns immediately.
3. Lines 221-223 are bypassed, dropping all children and leaving `n.Children = nil`.

## Acceptance test

```go
// internal/schema/tree_test.go
func TestBuildNodeAdditionalPropertiesDropsProperties(t *testing.T) {
	props := map[string]any{
		"strictConfig": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"host": map[string]any{"type": "string"},
				"port": map[string]any{"type": "integer"},
			},
		},
	}
	nodes := BuildTree(props, nil)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Type != "object" {
		t.Errorf("strictConfig.Type = %q, want object", n.Type)
	}
	if len(n.Children) != 2 {
		t.Fatalf("strictConfig.Children = %d, want 2", len(n.Children))
	}
	leaves := Leaves(nodes, "")
	if len(leaves) != 2 {
		t.Fatalf("Leaves() count = %d, want 2 (host, port)", len(leaves))
	}
}
```

**Fails today with:**
```
strictConfig.Type = "map", want object
strictConfig.Children = 0, want 2
```

## Contract

1. In `internal/schema/tree.go`:
   - When `n.Type == "object"`, prioritize declared `properties`: if `props, ok := raw["properties"].(map[string]any); ok && len(props) > 0`, populate `n.Children = BuildTree(props, stringSlice(raw["required"]))`.
   - Only treat as a map scalar leaf (`n.Type = "map"`) if there are no declared properties and `additionalProperties` is present and not `false` (e.g. `raw["additionalProperties"] != false`).
   - Also ensure untyped nodes with properties or objects with `additionalProperties: false` and no properties are handled cleanly.
2. Existing map-leaf behavior (e.g. `labels` or `tags` with `additionalProperties: {type: string}` and no properties) must continue to pass existing tests.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Downstream changes outside `internal/schema/`.

## Handover

Branch `CF-311-build-node-additional-properties`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
