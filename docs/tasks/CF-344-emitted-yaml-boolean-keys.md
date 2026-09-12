# Task Brief: CF-344 (Issue #232) — Emitted XRD and EnvironmentConfig write on/off/yes/no keys unquoted

## Problem Statement

`cf gen` writes parameter and environment keys named `on`, `off`, `yes`, `no`, `y`, `n`, `true`, `false`, `null`, `~` (and case variants) unquoted. The YAML decoder Kubernetes uses (`sigs.k8s.io/yaml`, which uses YAML 1.1 semantics) collapses them into booleans (`true`/`false`) or null. Four declared parameters (`on`, `off`, `yes`, `no`) reach the cluster as two properties named `true` and `false`, and `required: [on, providerName]` reaches Kubernetes as `[true, "providerName"]`, which violates CRD schema constraints. Similarly, `EnvironmentConfig` emits `data:` keys bare.

## Scope of Changes

- In `internal/emit/yaml.go` (or helper function):
  - Ensure any YAML keyword (`on`, `off`, `yes`, `no`, `y`, `n`, `true`, `false`, `null`, `~`, case-insensitively) or string containing special YAML characters is formatted/quoted appropriately when used as a mapping key or in a flow sequence.
  - Provide a helper such as `formatYAMLKey(k string) string` that returns quoted key `fmt.Sprintf("%q", k)` or `'k'` if it matches `yamlKeywords[strings.ToLower(k)] || strings.ToLower(k) == "~" || ...`.
- In `internal/emit/xrd.go`:
  - When writing parameter property names (both top-level in `x.Parameters` and in `writeObjectMembers`), quote keys that are YAML keywords.
  - When writing `required: [...]` lists (both top-level and in `writeObjectMembers`), quote entries that are YAML keywords (e.g. `'on'`).
- In `internal/emit/environment_config.go`:
  - In `EnvironmentConfig(b, cfg)`, format `data:` keys using key quoting if they are YAML keywords.
- In `internal/emit/`:
  - Add tests in `internal/emit/xrd_test.go` and `internal/emit/environment_config_test.go` that parse emitted XRD and EnvironmentConfig using `sigs.k8s.io/yaml` and assert that keys like `on`, `off`, `yes`, `no` survive as string keys.

## Acceptance Test

A test in `internal/emit/xrd_test.go` and/or `internal/emit/environment_config_test.go` decoding through `sigs.k8s.io/yaml`:
- Declares parameters `on`, `off`, `yes`, `no`, `providerName` with `on` marked required.
- Calls `XRD(bp.Spec.XRD)`.
- Decodes emitted XRD via `sigs.k8s.io/yaml.Unmarshal` or `YAMLToJSON` and validates that `spec.versions[0].schema.openAPIV3Schema.properties.spec.properties` has string keys `"on"`, `"off"`, `"yes"`, `"no"`, and `required` contains `"on"` (not boolean `true`).
- Similarly for `EnvironmentConfig`: keys `"on"`, `"off"`, etc. in `data:` decode as string keys under `sigs.k8s.io/yaml`.
