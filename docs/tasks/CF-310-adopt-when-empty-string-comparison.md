# CF-310 — adopt drops Go template when guards comparing empty strings, violating the round-trip rule

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Round-trip rule violation when adopting conditional resources comparing empty strings) |
| **Closes** | `#199` — `CF-310 — adopt drops Go template when guards comparing empty strings, violating the round-trip rule` |
| **Worktree** | `.worktrees/CF-310` on branch `CF-310-adopt-when-empty-string-comparison` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/adopt/adopt.go:1120-1124`, the regular expressions for extracting `when` guards from Go templates (`reWhenIfEq`, `reWhenIfNe`, `reWhenIfEnvEq`, `reWhenIfEnvNe`) match string comparison literals using `[^\"]+` or `[^\"]+?`, requiring at least one character inside the quotes.
However, `internal/blueprint/types.go:785` (`ParseWhen`) explicitly supports empty string literals `""` (`whenCmpRE = regexp.MustCompile(`^(params|env)\.([a-zA-Z][a-zA-Z0-9]*) (==|!=) "([^"\\]*)"$`)`), which is the canonical way to express optional resources conditioned on a parameter or environment key being populated (e.g. `when: params.customDomain != ""`).

When `cf gen` emits a composition with `when: params.customDomain != ""`, `internal/emit/composition.go:732` generates:
```yaml
{{- if ne $spec.customDomain "" }}
```
When `cf adopt` parses this template, `extractWhenGuard` (`internal/adopt/adopt.go:1866`) fails to match because `[^\"]+` does not match the zero-length string inside `""`. The function returns `""`, silently dropping the condition entirely without recording any drop in `LossReport`.

## Evidence

In `internal/adopt/adopt.go:1120-1124`:
```go
reWhenIfEq = regexp.MustCompile(`\{\{-?\s*if\s+eq\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+\"([^\"]+)\"\s*-?\}\}`)
reWhenIfNe = regexp.MustCompile(`\{\{-?\s*if\s+ne\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s+\"([^\"]+)\"\s*-?\}\}`)
```

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_ConditionalResources_EmptyStringComparison(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: render-resources
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          {{- if ne $spec.customDomain "" }}
          ---
          apiVersion: cert-manager.io/v1
          kind: Certificate
          metadata:
            annotations:
              crossplane.io/composition-resource-name: custom-cert
          spec:
            dnsNames:
              - example.com
          {{- end }}
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.When != `params.customDomain != ""` {
		t.Errorf("res.When = %q, want %q", res.When, `params.customDomain != ""`)
	}
}
```

**Fails today with:**
```
res.When = "", want "params.customDomain != \"\""
```

## Contract

1. In `internal/adopt/adopt.go`:
   - Update `reWhenIfEq`, `reWhenIfNe`, `reWhenIfEnvEq`, and `reWhenIfEnvNe` (and any related when guard regexes in `internal/adopt/adopt.go`) to allow empty string literals `\"([^\"]*)\"`, matching zero or more non-quote characters.
2. Ensure `extractWhenGuard` correctly parses empty string comparisons for both parameters and environment variables (`== ""` and `!= ""`).

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changing `ParseWhen` in `internal/blueprint/types.go` (already supports empty string literals).

## Handover

Branch `CF-310-adopt-when-empty-string-comparison`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
