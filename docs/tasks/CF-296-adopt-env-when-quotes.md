# CF-296 — adopt formats environment when conditions without quotes, failing validation on round-trip

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: adopt crashes on valid Crossplane Composition with environment when condition) |
| **Closes** | `#184` — `CF-296 — adopt formats environment when conditions without quotes, failing validation on round-trip` |
| **Worktree** | `.worktrees/CF-296` on branch `CF-296-adopt-env-when-quotes` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | none |

## Symptom

When adopting a Crossplane Composition containing an environment `when` comparison condition (such as `{{- if eq $env.stage "prod" }}` or `{{- if eq (default "" (index $env "stage")) "prod" }}` emitted by `cf gen`), `cf adopt` extracts the environment key and value but formats the condition without quotes around the literal (e.g. `env.stage == prod`). Blueprint validation requires double quotes around comparison literals (`whenCmpRE`), causing `cf adopt` to abort with a validation error and preventing round-trip adoption.

## Evidence

Given a Composition manifest:

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- if eq $env.stage "prod" }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              env: "prod"
            {{- end }}
```

Running `cf adopt`:

```sh
$ ./bin/cf adopt /tmp/test-comp.yaml
```

Output:
```
cf: error: adopt composition: validate adopted blueprint: resource "test-cm": when must be params.<name>/env.<key> (a boolean), params.<name>/env.<key> == "<literal>" or params.<name>/env.<key> != "<literal>" — exactly one space around the operator, double quotes around the literal, no backslashes or embedded quotes (got "env.stage == prod")
```

Exit code is 1.

## Location

`internal/adopt/adopt.go:1736` and `1743`:
```go
	if m := reWhenIfEnvEq.FindStringSubmatch(text); len(m) >= 3 {
		key, lit := m[1], m[2]
		if key == "" && len(m) >= 5 {
			key, lit = m[3], m[4]
		}
		ensureEnvDeclared(bp, key, "string")
		return fmt.Sprintf("env.%s == %s", key, lit)
	} else if m := reWhenIfEnvNe.FindStringSubmatch(text); len(m) >= 3 {
		key, lit := m[1], m[2]
		if key == "" && len(m) >= 5 {
			key, lit = m[3], m[4]
		}
		ensureEnvDeclared(bp, key, "string")
		return fmt.Sprintf("env.%s != %s", key, lit)
```

In contrast, parameter conditions at line 1753 and 1756 use `%q`:
```go
		return fmt.Sprintf("params.%s == %q", m[1], m[2])
```

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestCF296_AdoptEnvironmentWhenComparisonQuotes(t *testing.T) {
	manifest := []byte(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-when-comp
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- if eq $env.stage "prod" }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              env: "prod"
            {{- end }}
`)

	bp, _, err := Adopt(manifest, Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	wantWhen := `env.stage == "prod"`
	if bp.Spec.Resources[0].When != wantWhen {
		t.Errorf("resource when = %q, want %q", bp.Spec.Resources[0].When, wantWhen)
	}
}
```

**Fails today with:**
```
adopt composition: validate adopted blueprint: resource "test-cm": when must be params.<name>/env.<key> (a boolean), params.<name>/env.<key> == "<literal>" or params.<name>/env.<key> != "<literal>" — exactly one space around the operator, double quotes around the literal, no backslashes or embedded quotes (got "env.stage == prod")
```

## Contract

1. In `internal/adopt/adopt.go`:
   - Quote the literal string in environment equality (`==`) and inequality (`!=`) when guards (`fmt.Sprintf("env.%s == %q", key, lit)` and `fmt.Sprintf("env.%s != %q", key, lit)`).
   - Ensure the output conforms to `blueprint.ParseWhen` format (`whenCmpRE`).
2. Adopting Compositions with `eq $env.<key> "val"`, `ne $env.<key> "val"`, and `cf gen` emitted `eq (default "" (index $env "<key>")) "val"` must succeed and produce valid blueprints.

## Verification

```sh
make test-race
make lint && make lint-strict
```

## Handover

Branch `CF-296-adopt-env-when-quotes`, committed, not pushed, not merged.
