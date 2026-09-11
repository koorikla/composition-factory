# CF-259 — adopt rejects valid YAML 1.2 parameter names like on/off/yes/no, breaking round-trip generation

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: round-trip fidelity & parameter validation) |
| **Closes** | `CF-259 — adopt rejects valid YAML 1.2 parameter names like on/off/yes/no, breaking round-trip generation` |
| **Worktree** | `.worktrees/CF-259` on branch `CF-259-adopt-yaml-param-names` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Symptom

When a blueprint defines a parameter named `on`, `off`, `yes`, `no`, `y`, or `n` (which are valid strings in YAML 1.2 and valid parameters according to `internal/blueprint/validate_params.go`), `cf gen` succeeds. However, re-adopting the generated XRD and Composition via `cf adopt` drops the parameter and any resource fields referencing it:
```
# adopt: dropped template.param.on (invalid parameter identifier)
# adopt: dropped resource.<res>.fields.<field> (invalid parameter reference)
```
This violates the AGENTS.md §1 Round-Trip Rule.

## Mechanism

In `internal/blueprint/validate_params.go:9-14`, `yamlParamKeywords` correctly restricts reserved YAML 1.2 literal booleans/nulls:
```go
var yamlParamKeywords = map[string]bool{
	"true": true, "false": true, "null": true,
}
```
However, `internal/adopt/adopt.go:1016-1019` maintains an outdated YAML 1.1 keyword list:
```go
yamlKeywords = map[string]bool{
	"true": true, "false": true, "yes": true, "no": true,
	"on": true, "off": true, "null": true, "y": true, "n": true,
}
```
And `isValidParamIdentifier` at `adopt.go:1045` uses this table:
```go
func isValidParamIdentifier(name string) bool {
	parts := strings.Split(name, ".")
	for _, p := range parts {
		if !paramNameRE.MatchString(p) || yamlKeywords[strings.ToLower(p)] {
			return false
		}
	}
	return true
}
```
Because of this divergence, `adopt` rejects parameters like `on` or `yes`, dropping them into the loss report.

## Contract

1. Align `yamlKeywords` in `internal/adopt/adopt.go` with `internal/blueprint/validate_params.go`, so only `"true"`, `"false"`, and `"null"` are reserved keywords. Words like `on`, `off`, `yes`, `no`, `y`, `n` are valid parameter identifiers.
2. Ensure `isValidParamIdentifier` permits these identifiers so parameters and field references using them are preserved during adopt without loss.
3. Unit test in `internal/adopt/adopt_test.go` verifies round-trip / adoption of parameters named `on`, `off`, `yes`, `no`, `y`, `n`.

## Acceptance Test

Unit test in `internal/adopt/adopt_test.go`:
Given an XRD and Composition defining a parameter `on` and a resource patch referencing `spec.parameters.on`, `adopt.Adopt` successfully imports the parameter as `on` in `Spec.XRD.Parameters` and preserves the field wire `from: params.on`, with 0 loss drops.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-259-adopt-yaml-param-names`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
