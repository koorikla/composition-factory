# CF-291 — KCL emitter outputs empty writeConnectionSecretToRef mapping when child parameters omitted

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: KCL envelope emission conditional suppression) |
| **Closes** | `CF-291 — KCL emitter outputs empty writeConnectionSecretToRef mapping when child parameters omitted` |
| **Worktree** | `.worktrees/CF-291` on branch `CF-291-kcl-empty-envelope-mapping` |
| **May write** | `internal/emit/kcl.go`, `internal/emit/kcl_test.go` |
| **Merges after** | `CF-285` |

## Symptom

In `internal/emit/kcl.go`, `writeKCLEnvelopeNodes` recursively generates KCL mappings for nested envelope fields like `writeConnectionSecretToRef` by unconditionally emitting `name = {\n ... \n}` without checking if all child fields are optional and omitted. When child fields (such as `name`) are wired to optional XR parameters and omitted at runtime, the generated KCL dict evaluates to `{}`. Consequently, `function-kcl` outputs `writeConnectionSecretToRef: {}` in the rendered resource manifest.

In Crossplane CRD schemas (such as Upbound AWS/GCP/Azure resources), `writeConnectionSecretToRef` defines required properties (`name` and `namespace`). Crossplane admission rejects manifests with empty `writeConnectionSecretToRef: {}` with `spec.writeConnectionSecretToRef.name: Required value`.

In contrast, Go-templating (`internal/emit/envelope.go:503-540`) explicitly inspects whether all entries under an envelope node are optional, and suppresses the key entirely when none of the parameters are present.

## Mechanism

- `internal/emit/kcl.go:467-472`:
```go
func writeKCLEnvelopeNodes(sb *strings.Builder, indent string, nodes []*envTreeNode) {
	for _, n := range nodes {
		if len(n.children) > 0 {
			sb.WriteString(fmt.Sprintf("%s%s = {\n", indent, quoteKCLKey(n.name)))
			writeKCLEnvelopeNodes(sb, indent+"    ", n.children)
			sb.WriteString(fmt.Sprintf("%s}\n", indent))
```
`writeKCLEnvelopeNodes` creates the dictionary block unconditionally whenever `len(n.children) > 0`. Because child leaves are guarded with `if <expr> != None:`, when all child parameters are omitted, no inner keys are written, leaving an empty dictionary `writeConnectionSecretToRef = {}`.

## Contract

1. In `internal/emit/kcl.go`:
   - When generating envelope mappings in KCL: if an envelope subtree (such as `writeConnectionSecretToRef`) contains only optional child leaves and no static or required fields, guard the envelope mapping or suppress it when all child parameter expressions evaluate to `None`.
   - In KCL syntax, this can be written as:
     If all child nodes are optional, wrap the mapping assignment with `if <any_child_present>:` or omit empty dictionaries so that Crossplane does not receive empty `{}` for `writeConnectionSecretToRef`.
   - Ensure consistency with how Go template and Python handle optional envelope trees.

## Acceptance Test

Go unit test in `internal/emit/kcl_test.go`:
`TestKCLWriteConnectionSecretToRefOptionalOmitted`:
Create a blueprint with KCL engine, an optional XR parameter `secretName` (required: false), and a resource with `writeConnectionSecretToRef: {name: "${parameters.secretName}"}`.
Verify that the emitted KCL code guards `writeConnectionSecretToRef` so that when `secretName` is None, `writeConnectionSecretToRef` is not set or not emitted as an empty dict `{}`.

## Verification

```sh
make lint && make lint-strict && make test-race && make test-docker
```

## Handover

Branch `CF-291-kcl-empty-envelope-mapping`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
