# CF-356 — rawReferencesParam misses hasKey expressions, permitting deletion and corrupting parameter renames

## 1. Context & Invariant

In `internal/blueprint/edit.go`, `rawReferencesParam` and `rewriteRawParam` inspect and rewrite raw expressions and template bodies for parameter references.

In Crossplane `function-go-templating`, template authors frequently guard optional parameter access using `hasKey`, e.g.:
- `{{- if hasKey $spec "tier" }}`
- `{{- if hasKey .spec "tier" }}`
- `{{- if hasKey $.spec "tier" }}`
- `{{- if hasKey $params "tier" }}`
- `{{- if hasKey .params "tier" }}`
- `{{- if hasKey $.params "tier" }}`
- `{{- if hasKey params "tier" }}`

Because `rawReferencesParam` and `rewriteRawParam` do not recognize `hasKey` parameter queries:
1. `b.DeleteParameter("tier")` succeeds with nil error because `b.ReferencingResources("tier")` and `b.Spec.Templates` only check `rawReferencesParam`, leaving broken dangling references in dependent resources and templates.
2. `b.RenameParameter("tier", "ranking")` fails to rewrite `hasKey` expressions in `r.Fields`, `r.Envelope`, `r.Annotations`, and `cp.Spec.Templates`. The old parameter name (`"tier"`) remains in place. During composition rendering with the renamed parameter, `hasKey` evaluates to `false`, silently dropping whatever conditional resources or values were guarded by `hasKey`.

## 2. Requirements & Contract

1. In `internal/blueprint/edit.go`:
   - In `rawReferencesParam(raw, name string) bool`:
     - Match `hasKey` queries on parameter containers:
       `\bhasKey\s+(?:(?:\$|\$\.|\.)?observed\.composite\.resource\.spec|(?:\$|\$\.|\.)spec|(?:\$|\$\.|\.)?params)\s+(?:"` + q + `"|'` + q + `'|` + "`" + q + "`)" + `($|[^a-zA-Z0-9_])`
       where containers include `$spec`, `.spec`, `$.spec`, `$params`, `.params`, `$.params`, `params`, `.observed.composite.resource.spec`, `$.observed.composite.resource.spec`.
   - In `rewriteRawParam(raw, from, to string) string`:
     - For quote characters `"`, `'`, and `` ` ``:
       Rewrite `hasKey` expressions querying `from` to `to` across the same containers.
2. Guard with automated unit tests in `internal/blueprint/edit_test.go`:
   - `TestRawReferencesParam_HasKey`
   - `TestDeleteParameter_RefusesWhenHasKey`
   - `TestRenameParameter_RewritesHasKey`
3. Ensure `make lint && make lint-strict && make test-race` passes.

## 3. Verbatim Repro Test

```go
func TestRawReferencesParam_HasKey(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		param    string
		expected bool
	}{
		{"hasKey dollar spec", `{{- if hasKey $spec "tier" }}valid{{ end }}`, "tier", true},
		{"hasKey dot spec", `{{- if hasKey .spec "tier" }}valid{{ end }}`, "tier", true},
		{"hasKey dollar dot spec", `{{- if hasKey $.spec "tier" }}valid{{ end }}`, "tier", true},
		{"hasKey dollar params", `{{- if hasKey $params "tier" }}valid{{ end }}`, "tier", true},
		{"hasKey dot params", `{{- if hasKey .params "tier" }}valid{{ end }}`, "tier", true},
		{"hasKey single quote", `{{- if hasKey .spec 'tier' }}valid{{ end }}`, "tier", true},
		{"hasKey backtick", "{{- if hasKey .spec `tier` }}valid{{ end }}", "tier", true},
		{"hasKey unrelated prefix", `{{- if hasKey $spec "tierExtra" }}valid{{ end }}`, "tier", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rawReferencesParam(tt.raw, tt.param)
			if got != tt.expected {
				t.Errorf("rawReferencesParam(%q, %q) = %v, want %v", tt.raw, tt.param, got, tt.expected)
			}
		})
	}
}

func TestDeleteParameter_RefusesWhenHasKey(t *testing.T) {
	b := editable()
	b.Spec.XRD.Parameters["tier"] = Parameter{Type: "string"}
	b.Spec.Resources[0].Fields["rawField"] = Field{Raw: `{{- if hasKey $spec "tier" }}valid{{ end }}`}

	err := b.DeleteParameter("tier")
	if err == nil {
		t.Fatal("DeleteParameter = nil, want refusal when raw field references parameter via hasKey")
	}
}

func TestRenameParameter_RewritesHasKey(t *testing.T) {
	b := editable()
	b.Spec.XRD.Parameters["tier"] = Parameter{Type: "string"}
	b.Spec.Resources[0].Fields["rawField"] = Field{Raw: `{{- if hasKey $spec "tier" }}valid{{ end }}`}
	b.Spec.Templates = map[string]string{
		"helper": `{{- if hasKey .spec "tier" }}present{{ end }}`,
	}

	if err := b.RenameParameter("tier", "ranking"); err != nil {
		t.Fatalf("RenameParameter failed: %v", err)
	}

	if got := b.Spec.Resources[0].Fields["rawField"].Raw; got != `{{- if hasKey $spec "ranking" }}valid{{ end }}` {
		t.Errorf("raw field = %q, want hasKey $spec \"ranking\"", got)
	}
	if got := b.Spec.Templates["helper"]; got != `{{- if hasKey .spec "ranking" }}present{{ end }}` {
		t.Errorf("template = %q, want hasKey .spec \"ranking\"", got)
	}
}
```
