# CF-309 — validateParameterMembers rejects YAML 1.2 object member names like on/off/yes/no, breaking adopt

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: validation rejects valid YAML 1.2 object member names, breaking adopt) |
| **Closes** | `#198` — `CF-309 — validateParameterMembers rejects YAML 1.2 object member names like on/off/yes/no, breaking adopt` |
| **Worktree** | `.worktrees/CF-309` on branch `CF-309-validate-parameter-members-yaml-keywords` |
| **May write** | `internal/blueprint/load.go`, `internal/blueprint/validate_params_test.go` |
| **Merges after** | `nothing` |

## Symptom

In CF-259 (#147), top-level parameters and `cf adopt` parameter identifier resolution were updated to follow YAML 1.2 semantics, treating `"on"`, `"off"`, `"yes"`, `"no"`, `"y"`, and `"n"` as valid string identifiers (`yamlParamKeywords` in `internal/blueprint/validate_params.go:12`).
However, `validateParameterMembers` in `internal/blueprint/load.go:258` was left checking the legacy YAML 1.1 `yamlKeywords` map (`load.go:97`), causing `bp.Validate()` and `cf adopt` to fail with validation errors whenever an object parameter declares member properties named `on`, `off`, `yes`, `no`, `y`, or `n`.

## Evidence

In `internal/blueprint/load.go:258` (`validateParameterMembers`):
```go
if !paramNameRE.MatchString(m) || yamlKeywords[strings.ToLower(m)] {
    return fmt.Errorf("%s: invalid member name (must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)", mPath)
}
```
Because `load.go:97` includes `yes`, `no`, `on`, `off`, `y`, `n`, `validateParameterMembers` rejects any object parameter member bearing these names.

## Acceptance test

```go
// internal/blueprint/validate_params_test.go
func TestValidateObjectParameterMembersYAMLKeywordResilience(t *testing.T) {
	for _, name := range []string{"n", "y", "yes", "no", "on", "off"} {
		t.Run(name, func(t *testing.T) {
			bp := validParamBlueprint("settings")
			bp.Spec.XRD.Parameters["settings"] = Parameter{
				Type: "object",
				Properties: map[string]Parameter{
					name: {Type: "string"},
				},
			}
			if err := bp.Validate(); err != nil {
				t.Fatalf("Validate rejected valid object member name %q: %v", name, err)
			}
		})
	}
}
```

**Fails today with:**
```
Validate rejected valid object member name "n": spec.xrd.parameters.settings.properties.n: invalid member name (must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)
```

## Contract

1. In `internal/blueprint/load.go`:
   - In `validateParameterMembers`, do not forbid YAML 1.1 boolean keywords (`yes`, `no`, `on`, `off`, `y`, `n`). Forbid only YAML 1.2 reserved literals (`true`, `false`, `null`), matching top-level parameter validation in `internal/blueprint/validate_params.go:yamlParamKeywords`.
2. `bp.Validate()` must accept object member names `on`, `off`, `yes`, `no`, `y`, `n`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Renaming existing parameters.

## Handover

Branch `CF-309-validate-parameter-members-yaml-keywords`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
