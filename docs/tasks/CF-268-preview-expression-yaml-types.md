# CF-268 — PreviewExpression unmarshals fromYaml into map[string]any, failing on YAML lists and scalars

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: Go template preview fidelity for fromYaml helper) |
| **Closes** | `CF-268 — PreviewExpression unmarshals fromYaml into map[string]any, failing on YAML lists and scalars` |
| **Worktree** | `.worktrees/CF-268` on branch `CF-268-preview-expression-yaml-types` |
| **May write** | `internal/emit/preview.go`, `internal/emit/preview_test.go` |
| **Merges after** | `CF-266` |

## Symptom

In Crossplane's `function-go-templating`, the template helper `fromYaml` has signature `fromYaml(val string) (any, error)`. It unmarshals into an empty interface (`any`), allowing YAML lists (e.g. `{{ range (fromYaml "[a, b, c]") }}{{ . }}{{ end }}`), scalars, and objects to be parsed.

In Composition Factory, `PreviewExpression` (used by `POST /api/preview-expression`, the canvas inspector live preview, and the MCP `preview_expression` tool) restricts `fromYaml` to `map[string]any`. When a user attempts to preview any valid expression that unmarshals a list (such as `{{ list "a" "b" | toYaml | fromYaml }}` or `{{ fromYaml "- item1\n- item2" }}`) or scalar, evaluation crashes with:
```
template: preview:1:10: executing "preview" at <fromYaml ...>: error calling fromYaml: error unmarshaling JSON: while decoding JSON: json: cannot unmarshal array into Go value of type map[string]interface {}
```

## Mechanism

In `internal/emit/preview.go:251-255`:
```go
funcs["fromYaml"] = func(s string) (map[string]any, error) {
    out := map[string]any{}
    err := yaml.Unmarshal([]byte(s), &out)
    return out, err
}
```
Because the destination type is hardcoded to `map[string]any`, `sigs.k8s.io/yaml.Unmarshal` returns an error when the root of the input YAML document is a list (`[]any`) or scalar.

## Contract

1. In `internal/emit/preview.go:251-255`, define `fromYaml` to unmarshal into `any`:
   ```go
   funcs["fromYaml"] = func(s string) (any, error) {
       var out any
       err := yaml.Unmarshal([]byte(s), &out)
       return out, err
   }
   ```
2. Verify that YAML objects continue to return `map[string]any`, YAML arrays return `[]any`, and YAML scalars return their primitive Go types (`int`, `float64`, `bool`, `string`).
3. Ensure template expressions evaluating list and scalar `fromYaml` invocations succeed in `PreviewExpression`.

## Acceptance Test

Go unit test in `internal/emit/preview_test.go`:
1. Call `PreviewExpression` with YAML list expression `{{ range (fromYaml "- a\n- b") }}{{ . }}{{ end }}` and verify result is `"ab"`.
2. Call `PreviewExpression` with roundtrip `{{ list "foo" "bar" | toYaml | fromYaml | len }}` and verify result is `"2"`.
3. Call `PreviewExpression` with scalar `{{ fromYaml "42" }}` and verify result is `"42"`.
4. Call `PreviewExpression` with standard YAML map `{{ (fromYaml "k: v").k }}` and verify result is `"v"`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-268-preview-expression-yaml-types`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
