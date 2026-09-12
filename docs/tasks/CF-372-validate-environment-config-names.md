# CF-372 — validateEnvironmentConfigs accepts non-DNS names, control chars, and YAML keywords in config name

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-372 — validateEnvironmentConfigs accepts non-DNS names, control chars, and YAML keywords in config name` (#264) |
| **Worktree** | `.worktrees/CF-372` on branch `CF-372-validate-environment-config-names`, branched from `main` |
| **May write** | `internal/blueprint/validate_environment.go`, `internal/blueprint/validate_environment_test.go` |
| **Merges after** | nothing |

## Defect Summary

EnvironmentConfig resources emitted by `internal/emit/environment_config.go:18-22` and `internal/emit/emit.go:112` are Kubernetes custom resources whose names become:
1. `metadata.name: <name>` in the emitted `EnvironmentConfig` YAML manifest.
2. `environmentconfigs/<name>.yaml` file path on disk.
3. `ref.name: <name>` in the Input document for `function-environment-configs` (`internal/blueprint/types.go:577`).

Under Kubernetes standards and engine conventions across `internal/blueprint` (`load.go:67-69`, `pipeline.go:84,107`, `validate_resources.go:35`, `validate_root.go:20`):
- All Kubernetes object names and reference names must conform to DNS label / subdomain requirements (`resourceNameRE`: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).
- User-supplied scalars must pass `checkScalar` to prevent control character injection (such as newlines breaking indentation in emitted YAML manifests).
- Unquoted YAML keywords (such as `yes`, `no`, `true`, `false`, `on`, `off`, `null`) must be rejected because YAML 1.1 parsers reinterpret bare keywords as boolean/null values.

However, `validateEnvironmentConfigs()` in `internal/blueprint/validate_environment.go:71-125` never validates `cfg.Name`.

Consequently:
- A blueprint can specify `name: "invalid/name"` or `name: "../../escape"`, leading to path traversal or malformed paths when emitting to disk.
- A blueprint can specify `name: "custom\ninjected: true"`, bypassing `checkScalar` and injecting arbitrary YAML lines.
- A blueprint can specify `name: yes` or `name: true`, emitting `metadata.name: yes` which YAML 1.1 interprets as a boolean.

## Acceptance Test

In `internal/blueprint/validate_environment_test.go`:

```go
func TestValidateEnvironmentConfigs_RejectsInvalidNames(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		wantErr    string
	}{
		{
			name:       "slash in name",
			configName: `"invalid/name"`,
			wantErr:    `spec.environmentConfigs[0].name: "invalid/name" is not a valid config name`,
		},
		{
			name:       "uppercase in name",
			configName: `"CustomConfig"`,
			wantErr:    `spec.environmentConfigs[0].name: "CustomConfig" is not a valid config name`,
		},
		{
			name:       "yaml keyword yes",
			configName: `yes`,
			wantErr:    `spec.environmentConfigs[0].name: "yes" is not a valid config name`,
		},
		{
			name:       "control character newline",
			configName: `"custom\ninjected: true"`,
			wantErr:    `control character`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  sources: []
  xrd:
    group: test.org
    version: v1alpha1
    kind: Test
    plural: tests
    scope: Namespaced
  environment:
    region:
      type: string
  environmentConfigs:
    - name: %s
      data:
        region: us-east-1`, tc.configName)

			_, err := Load(write(t, manifest))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}
```

## Contract

In `internal/blueprint/validate_environment.go:validateEnvironmentConfigs`:
When `cfg.Name != ""`:
1. Check `checkScalar(fmt.Sprintf("spec.environmentConfigs[%d].name", i), cfg.Name)` and return the error if non-nil.
2. Check `!resourceNameRE.MatchString(cfg.Name) || yamlKeywords[strings.ToLower(cfg.Name)]` and return an informative error (e.g. `fmt.Errorf("spec.environmentConfigs[%d].name: %q is not a valid config name (must be a DNS label, e.g. dev-env, and not a YAML keyword like yes/no/on/off)", i, cfg.Name)`).

## Verification

```sh
make lint
make lint-strict
make test-race
go test ./internal/blueprint -run TestValidateEnvironmentConfigs_RejectsInvalidNames -v
```
