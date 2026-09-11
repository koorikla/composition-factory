# CF-279 — untyped object param into map field drops loop or duplicates keys when nested fields present

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: emission correctness for object parameters wired into map fields) |
| **Closes** | `CF-279 — untyped object param into map field drops loop or duplicates keys when nested fields present` |
| **Worktree** | `.worktrees/CF-279` on branch `CF-279-untyped-object-param-map-emission` |
| **May write** | `internal/emit/composition.go`, `internal/emit/native.go`, `internal/emit/map_entries_test.go` |
| **Merges after** | `CF-278` |

## Symptom

When a blueprint wires an untyped object parameter into a map field (e.g. `tags: { from: "params.customTags" }`, where `customTags: { type: "object" }` has no declared properties):

1. **Silent drop and empty YAML on nested resources:** If the resource has *any* nested field (e.g. `nested.enabled: { value: "true" }` or `initProvider.kmsKey: { value: "key" }`), `writeForProviderTree` invokes `buildNativeTree`. Because `len(f.entries) == 0`, `insertNativePath` registers the map as a leaf with empty RHS (`f.rhs == ""`). `writeNativeLeaf` then emits:
   ```yaml
   tags:
   ```
   with no range loop, no guard, and no value. In YAML, this parses as `tags: null`, completely dropping the parameter wiring.

2. **Key duplication and emitter conflicts with explicit keys:** If the map field also defines explicit bracket keys (e.g. `tags[Environment]: { value: "prod" }`), `distinctBase` in `internal/emit/composition.go:966-968` appends `l.basePath` twice because `grouped[l.basePath]` is never populated for untyped objects on the first pass (`_, exists := grouped[l.basePath]` evaluates to `false`). This produces:
   - In Go template (flat mode): Duplicate `tags:` keys in the output YAML:
     ```yaml
     tags:
       Environment: 'prod'
     tags:
       Environment: 'prod'
     ```
   - In Python and KCL emitters (or Go template with nested fields): `insertNativePath` conflicts with itself and fails emission entirely:
     `resource "queue": field "tags[Environment]" conflicts with field "Environment", which already sets that whole value`

3. **Untyped parameter dropped when explicit keys exist:** When explicit keys exist alongside the untyped parameter wire, `len(fld.entries) > 0`, so `writeField`'s dynamic range branch (`fld.structured.targetType == "object" && len(fld.entries) == 0`) is skipped, completely omitting the `range $k, $v := $spec.<param>` loop and emitting only the explicit keys.

## Mechanism

In `internal/emit/composition.go:965-985`:
```go
	for _, l := range leaves {
		if _, exists := grouped[l.basePath]; !exists {
			distinctBase = append(distinctBase, l.basePath)
		}
		if l.isMap {
			isMapField[l.basePath] = true
			grouped[l.basePath] = append(grouped[l.basePath], ...)
		} else if l.structured.targetType == "object" && l.structured.kind == rhsParam {
			isMapField[l.basePath] = true
			mapBaseStructured[l.basePath] = l.structured
			_, chain, err := blueprint.ParamChain(b.Spec.XRD, "", l.structured.param)
			if err == nil && len(chain) > 0 {
				wireDecl := chain[len(chain)-1]
				if len(wireDecl.Properties) > 0 {
					// only populates grouped if wireDecl has Properties!
				}
			}
		}
```
When `len(wireDecl.Properties) == 0`:
1. `grouped[l.basePath]` is never added to the map. Thus `_, exists := grouped[l.basePath]` is `false` when subsequent leaves for the same `basePath` are inspected, leading to duplicate entries in `distinctBase`.
2. In `buildNativeTree` (`internal/emit/native.go:56-62`), an untyped map field with `len(f.entries) == 0` is added as a leaf with `leaf.rhs == ""`.
3. In `writeNativeLeaf` (`internal/emit/native.go:383-396`), native leaves are printed as `d.Line(indent, "%s: %s", formatKey(n.seg), n.leaf.rhs)`, emitting a bare key without checking if `n.leaf.isMap` or `n.leaf.structured.targetType == "object"`.

## Contract

1. In `internal/emit/composition.go`:
   - Track `seenBase := map[string]bool{}` explicitly rather than relying on `grouped[l.basePath]`, ensuring `distinctBase` never contains duplicates regardless of property count.
   - When an untyped object parameter is wired into a map field that also has explicit keys (`len(fld.entries) > 0`), ensure the range loop over `$spec.<param>` is still emitted alongside the explicit keys (with explicit keys taking precedence).
2. In `internal/emit/native.go`:
   - In `writeNativeLeaf`, check if `n.leaf.isMap` and `n.leaf.structured.targetType == "object"`. If `n.leaf.rhs == ""` and it represents an object parameter, emit the guarded `range $k, $v := $spec.<param>` block matching `writeField`.
3. Unit test coverage:
   - Add tests in `internal/emit/map_entries_test.go` verifying that untyped object parameter wiring into map fields renders correctly:
     - When the resource has nested fields (exercises native tree path).
     - When the resource has explicit keys alongside the object wire.
     - With no duplicate keys and valid YAML structure.

## Acceptance Test

Run Go unit tests in `internal/emit`:
`go test ./internal/emit -run TestUntypedObjectParam -v`
1. Resource with `tags: { from: "params.customTags" }` and `nested.enabled: { value: "true" }` must emit:
   ```yaml
   nested:
     enabled: true
   {{- if hasKey $spec "customTags" }}
   tags:
     {{- range $k, $v := $spec.customTags }}
     {{ $k }}: {{ $v }}
     {{- end }}
   {{- end }}
   ```
2. Resource with `tags: { from: "params.customTags" }` and `tags[Environment]: { value: "prod" }` must not duplicate `tags:` and must emit valid YAML with both explicit tag and dynamic range.
