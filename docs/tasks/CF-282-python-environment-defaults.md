# CF-282 — Python emitter drops defaulted environment keys when omitted from environment context

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: emission correctness for environment defaults in Python) |
| **Closes** | `CF-282 — Python emitter drops defaulted environment keys when omitted from environment context` |
| **Worktree** | `.worktrees/CF-282` on branch `CF-282-python-environment-defaults` |
| **May write** | `internal/emit/python.go`, `internal/emit/structured.go`, `internal/emit/envelope.go`, `internal/emit/annotations.go`, `internal/emit/environment_test.go`, `internal/emit/python_test.go` |
| **Merges after** | `CF-281` |

## Symptom

When emitting Crossplane Compositions using `engine: python` on a Blueprint declaring `spec.environment` keys with `default:` values:
1. Resource fields and envelope fields wired to `from: env.<key>` (`case rhsEnv` in `pythonStructuredRHS` in `internal/emit/python.go:265-272`) emit `env.get(<key>)` without passing the declared default value. When function-python executes against a context that omits `<key>`, `env.get(<key>)` returns `None`. Because line 29 of the emitted Python function defines `_present = lambda d: {k: v for k, v in d.items() if v is not None}`, all `None` values are stripped from the resource specification. Consequently, the declared default value is lost and the field is missing from the composed resource. In contrast, Go-templating (`internal/emit/structured.go:256-270` and `internal/emit/envelope.go:209-227`) emits `{{ default <defVal> (index $env <key>) }}`, correctly falling back to the default. If the field is required by the provider schema (such as AWS `region`), the resource reconciliation fails.
2. Resource loop bounds (`forEach: env.<key>`) in Python (`translateForEachToPython` in `internal/emit/python.go:411-413`) hardcode `range(int(env.get(<key>, 0)))`. If `<key>` declares a default (e.g. `default: "3"`), Python ignores the blueprint default and evaluates to `0`, skipping the loop entirely and emitting zero resources when the environment key is omitted. In contrast, Go-templating (`internal/emit/composition.go:275-277`) emits `{{- range $i := until (int (default <defVal> (index $env <key>))) }}` which executes 3 iterations.
3. Resource conditions (`when: env.<key>`) in Python (`translateWhenToPython` in `internal/emit/python.go:343-405`) evaluate `bool(env.get(<key>))` or `env.get(<key>) == ...` without defaulting. If `<key>` declares a default of `"true"` or a matching string value, Python evaluates against `None` (`bool(None)` is `False`), skipping the resource when omitted from the context. In contrast, Go-templating (`internal/emit/composition.go:712-722`) emits `default <defVal> (index $env <key>)`.

## Mechanism

In `internal/emit/structured.go:256-270`, `internal/emit/envelope.go:209-227`, and `internal/emit/annotations.go:140-155`:
When `envDecl.Default != ""`, Go templating generates `default <defVal> (index $env <key>)`, but `structuredRHS` only stores `kind: rhsEnv`, `param: ref.Env`, `rawExpr: expr`, etc. It does not record the default value (`envDecl.Default`).

In `internal/emit/python.go:265-272`:
```go
	case rhsEnv:
		expr := fmt.Sprintf("env.get(%q)", s.param)
		if s.targetType == "string" && s.sourceType != "" && s.sourceType != "string" {
			return fmt.Sprintf("_str(%s)", expr)
		}
		return expr
```
`pythonStructuredRHS` has no access to the default value, and emits `env.get(%q)` without a fallback argument.

In `internal/emit/python.go:411-413`:
```go
	if key, ok := blueprint.EnvRef(forEach); ok {
		return fmt.Sprintf("range(int(env.get(%q, 0)))", key)
	}
```
`translateForEachToPython` hardcodes `0` instead of reading `b.Spec.Environment[key].Default`.

In `internal/emit/python.go:343-405`:
`translateWhenToPython` translates `env.<key>` expressions to `bool(env.get(%q))` without consulting `b.Spec.Environment[key].Default`.

## Contract

1. In `internal/emit/structured.go`, `internal/emit/envelope.go`, and `internal/emit/annotations.go`:
   - Extend `structuredRHS` (or pass default information) so that `rhsEnv` carries the declared environment key default value.
2. In `internal/emit/python.go`:
   - In `pythonStructuredRHS`, when `s.kind == rhsEnv` and a default value is defined, emit `env.get(<key>, <formattedDefault>)`:
     - string default: `env.get("region", "us-east-1")`
     - integer/number default: `env.get("retention", 345600)`
     - boolean default: `env.get("enabled", True)` (or `False`)
   - In `translateForEachToPython`, pass or look up the blueprint's environment defaults so that `forEach: env.<key>` emits `range(int(env.get(<key>, <default>)))` using `envDecl.Default` when present (falling back to `0` only when no default is declared).
   - In `translateWhenToPython`, pass or look up the blueprint's environment defaults so that `when: env.<key>` conditions fall back to the declared default value when missing from context.
3. Update `TestEnvironmentEmission_Python` in `internal/emit/environment_test.go` and add tests in `internal/emit/python_test.go` guarding that:
   - Field `from: env.region` with default `"us-east-1"` emits `"region": env.get("region", "us-east-1")`.
   - Field `from: env.retention` with default `"345600"` emits `"messageRetentionSeconds": env.get("retention", 345600)`.
   - Envelope `providerConfigRef.name` with default `"us-east-1"` emits `"name": env.get("region", "us-east-1")`.
   - `forEach: env.count` with default `"3"` emits `range(int(env.get("count", 3)))`.
   - `when: env.enabled` with default `"true"` emits `bool(env.get("enabled", True))`.

## Acceptance Test

```go
func TestPythonEnvironmentDefaults(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"region":    {Type: "string", Default: "us-east-1"},
				"retention": {Type: "integer", Default: "345600"},
				"count":     {Type: "integer", Default: "3"},
				"enabled":   {Type: "boolean", Default: "true"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					When:     "env.enabled",
					ForEach:  "env.count",
					Fields: map[string]blueprint.Field{
						"region":                  {From: "env.region"},
						"messageRetentionSeconds": {From: "env.retention"},
					},
					Envelope: map[string]blueprint.Field{
						"providerConfigRef.name": {From: "env.region"},
					},
				},
			},
		},
	}

	body, err := pythonTemplateBody(bp, testCRDs(t))
	if err != nil {
		t.Fatalf("pythonTemplateBody failed: %v", err)
	}

	if !strings.Contains(body, `"region": env.get("region", "us-east-1")`) {
		t.Errorf("expected env.get(\"region\", \"us-east-1\") in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `"messageRetentionSeconds": env.get("retention", 345600)`) {
		t.Errorf("expected env.get(\"retention\", 345600) in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `"name": env.get("region", "us-east-1")`) {
		t.Errorf("expected envelope providerConfigRef name with default in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `range(int(env.get("count", 3)))`) {
		t.Errorf("expected range(int(env.get(\"count\", 3))) in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `bool(env.get("enabled", True))`) {
		t.Errorf("expected bool(env.get(\"enabled\", True)) in python body, got:\n%s", body)
	}
}
```
