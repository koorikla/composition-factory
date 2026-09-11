# CF-278 — Python and KCL emitters corrupt when string comparisons on numeric and boolean literals

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: emission correctness for `when` conditions in Python and KCL) |
| **Closes** | `CF-278 — Python and KCL emitters corrupt when string comparisons on numeric and boolean literals` |
| **Worktree** | `.worktrees/CF-278` on branch `CF-278-emitter-when-string-comparison` |
| **May write** | `internal/emit/python.go`, `internal/emit/python_test.go`, `internal/emit/kcl.go`, `internal/emit/kcl_test.go` |
| **Merges after** | `CF-277` |

## Symptom

When emitting Crossplane Compositions using `engine: python` or `engine: kcl`:
1. In Python (`translateWhenToPython`), string equality/inequality against numeric strings (e.g. `when: params.version == "1.0"` or `when: params.code == "123"`) is passed to `pythonFormatLiteral(literal, "")`, which strips string quotes and parses the literal as a number (`spec.get("version") == 1` or `spec.get("code") == 123`). In Python, a string is never equal to a number (`"1.0" == 1` is `False`, `"123" == 123` is `False`), so the conditional check permanently fails at runtime, silently dropping the resource from being composed.
2. In Python (`translateWhenToPython`), string equality/inequality against `"true"` or `"false"` (e.g. `when: params.flag == "false"`) is intercepted and transformed into `bool(spec.get("flag")) is False` or `is True`. In Python, any non-empty string is truthy (`bool("false") == True`), so `bool("false") is False` evaluates to `False`, while `bool("false") is True` evaluates to `True`. This causes a complete semantic inversion at runtime, deploying the resource when `flag: "true"` was expected, and vice versa.
3. In KCL (`translateWhenToKCL`), regex word boundary replacement `kclBoolTrueRE.ReplaceAllString(when, "True")` and `kclBoolFalseRE` modifies `"true"` to `"True"` and `"false"` to `"False"` inside string literals (e.g. `when: params.flag == "true"` produces `_spec?.flag == "True"`). In KCL, strings are case-sensitive, so runtime comparison against the XR spec value `"true"` fails (`"true" == "True"` is `False`).

## Mechanism

In `internal/emit/python.go:351-370`:
```go
		switch op {
		case "":
			return fmt.Sprintf("bool(%s.get(%q))", targetDict, name)
		case "==":
			if literal == "true" {
				return fmt.Sprintf("bool(%s.get(%q)) is True", targetDict, name)
			}
			if literal == "false" {
				return fmt.Sprintf("bool(%s.get(%q)) is False", targetDict, name)
			}
			return fmt.Sprintf("%s.get(%q) == %s", targetDict, name, pythonFormatLiteral(literal, ""))
		case "!=":
			if literal == "true" {
				return fmt.Sprintf("bool(%s.get(%q)) is not True", targetDict, name)
			}
			if literal == "false" {
				return fmt.Sprintf("bool(%s.get(%q)) is not False", targetDict, name)
			}
			return fmt.Sprintf("%s.get(%q) != %s", targetDict, name, pythonFormatLiteral(literal, ""))
		}
```
In `internal/emit/kcl.go:344-350`:
```go
func translateWhenToKCL(when string) string {
	when = strings.TrimSpace(when)
	when = strings.ReplaceAll(when, "params.", "_spec?.")
	when = kclBoolTrueRE.ReplaceAllString(when, "True")
	when = kclBoolFalseRE.ReplaceAllString(when, "False")
	return when
}
```

By the Blueprint specification and `validateResources` (`internal/blueprint/validate_resources.go:134-146`), `when` comparisons (`==` and `!=`) only apply to parameters of type `string` comparing against string literals. Go-template emission correctly treats all literals as quoted strings (`composition.go:738`: `eq $spec.%s %q`).

## Contract

1. In `internal/emit/python.go`:
   - For `==` and `!=` in `translateWhenToPython`, emit the literal as a JSON/quoted string literal:
     `fmt.Sprintf("%s.get(%q) == %s", targetDict, name, pythonFormatLiteral(literal, "string"))` or `fmt.Sprintf("%s.get(%q) == %q", targetDict, name, literal)`
     `fmt.Sprintf("%s.get(%q) != %s", targetDict, name, pythonFormatLiteral(literal, "string"))` or `fmt.Sprintf("%s.get(%q) != %q", targetDict, name, literal)`
   - Bare `when: params.<name>` (when `op == ""`) remains `bool(%s.get(%q))`.
2. In `internal/emit/kcl.go`:
   - `translateWhenToKCL` must not blindly replace `true` and `false` inside quoted string literals. Use `blueprint.ParseWhen` to differentiate bare booleans from string comparisons.
3. Update unit tests in `internal/emit/python_test.go` and `internal/emit/kcl_test.go` to guard that:
   - `params.version == "1.0"` emits `spec.get("version") == "1.0"` in Python and `_spec?.version == "1.0"` in KCL.
   - `params.code == "123"` emits `spec.get("code") == "123"` in Python and `_spec?.code == "123"` in KCL.
   - `params.flag == "false"` emits `spec.get("flag") == "false"` in Python and `_spec?.flag == "false"` in KCL.
   - `params.flag == "true"` emits `spec.get("flag") == "true"` in Python and `_spec?.flag == "true"` in KCL.
   - Bare `params.enabled` emits `bool(spec.get("enabled"))` in Python and `_spec?.enabled` in KCL.

## Acceptance Test

Go unit tests in `internal/emit/python_test.go` and `internal/emit/kcl_test.go`:
1. Test `translateWhenToPython`:
   - `params.version == "1.0"` -> `spec.get("version") == "1.0"`
   - `params.code == "123"` -> `spec.get("code") == "123"`
   - `params.flag == "false"` -> `spec.get("flag") == "false"`
   - `params.flag == "true"` -> `spec.get("flag") == "true"`
   - `params.enabled` -> `bool(spec.get("enabled"))`
2. Test `translateWhenToKCL`:
   - `params.version == "1.0"` -> `_spec?.version == "1.0"`
   - `params.code == "123"` -> `_spec?.code == "123"`
   - `params.flag == "false"` -> `_spec?.flag == "false"`
   - `params.flag == "true"` -> `_spec?.flag == "true"`
   - `params.enabled` -> `_spec?.enabled`
