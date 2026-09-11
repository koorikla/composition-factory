# CF-292 — cf gen emits empty string for omitted numeric and boolean keys in EnvironmentConfig manifests

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: EnvironmentConfig type fidelity for omitted keys) |
| **Closes** | `CF-292 — cf gen emits empty string for omitted numeric and boolean keys in EnvironmentConfig manifests` |
| **Worktree** | `.worktrees/CF-292` on branch `CF-292-env-config-omitted-keys-typing` |
| **May write** | `internal/emit/environment_config.go`, `internal/emit/environment_config_test.go` |
| **Merges after** | `CF-285` |

## Symptom

When a blueprint defines environment keys of non-string types (`integer`, `number`, `boolean`) under `spec.environment` without defaults, and an entry in `spec.environmentConfigs` does not define a value for that key, `internal/emit/environment_config.go` unconditionally emits `k: ""` in the generated `EnvironmentConfig`'s `data` block.

Because `""` is a YAML string literal, Crossplane and downstream pipeline functions (Go templating, Python, KCL) receive string `""` instead of an integer/boolean value (or omitting the key).
- In Python pipeline functions, attempting type conversion (`int(_env.get("count", 1))`) crashes at runtime with `ValueError: invalid literal for int() with base 10: ''`.
- In Go templating, passing `$env.replicaCount` into an integer-typed managed resource field emits `replicas: ""` which fails Kubernetes CRD validation with `Invalid value: "string": expected integer, got string`.

## Mechanism

In `internal/emit/environment_config.go:43-52`:
```go
		for _, k := range envKeys {
			envKey := b.Spec.Environment[k]
			if val, ok := data[k]; ok {
				d.Line(1, "%s: %s", k, formatEnvVal(envKey, val))
			} else if envKey.Default != "" {
				d.Line(1, "%s: %s", k, formatEnvDefault(envKey))
			} else {
				d.Line(1, "%s: \"\"", k)
			}
		}
```
Line 50 writes `d.Line(1, "%s: \"\"", k)` for any declared environment key missing from the config's `data` and lacking a default, regardless of `envKey.Type`.
Similarly, in `formatEnvVal` (lines 67-76):
```go
func formatEnvVal(k blueprint.EnvironmentKey, val string) string {
	if val == "" {
		return `""`
	}
	switch k.Type {
	case "integer", "number", "boolean":
		return val
...
```
If `val` is empty string in `data`, it emits `""` even for numeric/boolean keys.

## Contract

1. In `internal/emit/environment_config.go`:
   - When an environment key is omitted from an environment config's `data` and lacks a default:
     - If the key's type is `integer`, emit `0`.
     - If the key's type is `number`, emit `0.0`.
     - If the key's type is `boolean`, emit `false`.
     - Otherwise (string or undefined type), emit `""`.
   - When an environment key has an empty string value `""` in `data`:
     - If the key's type is `integer`, format as `0`.
     - If the key's type is `number`, format as `0.0`.
     - If the key's type is `boolean`, format as `false`.
     - Otherwise format as `""`.
2. Ensure generated `EnvironmentConfig` manifests validate against Kubernetes YAML parsing for integer and boolean types without emitting string quotes for non-string types.

## Acceptance Test

Go unit test in `internal/emit/environment_config_test.go`:
`TestEnvironmentConfigOmittedKeysTyping`:
Create a blueprint with environment keys:
- `replicaCount`: `integer`
- `scaleFactor`: `number`
- `enabled`: `boolean`
- `endpoint`: `string`
And an environment config with only `endpoint: "https://dev.example.org"`.
Verify the emitted YAML data block has:
- `replicaCount: 0` (no quotes)
- `scaleFactor: 0.0` (no quotes)
- `enabled: false` (no quotes)
- `endpoint: "https://dev.example.org"`

## Verification

```sh
make lint && make lint-strict && make test-race && make test-docker
```

## Handover

Branch `CF-292-env-config-omitted-keys-typing`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
