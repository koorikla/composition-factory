# CF-313 — adopt drops string prefix and suffix on interpolated Go template parameters into bare From wires

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: Round-trip rule violation when adopting interpolated Go template strings) |
| **Closes** | `#202` — `CF-313 — adopt drops string prefix and suffix on interpolated Go template parameters into bare From wires` |
| **Worktree** | `.worktrees/CF-313` on branch `CF-313-adopt-interpolated-go-template-strings` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `nothing` |

## Symptom

When a Go template in an adopted composition contains an interpolated string literal such as:
```yaml
data:
  arn: "arn:aws:s3:::{{ $spec.bucketName }}/*"
  prefixName: "prefix-{{ $spec.bucketName }}"
```
`adopt` strips the surrounding prefix (`"arn:aws:s3:::"`) and suffix (`"/*"`), reducing the field to a bare wire `From: "params.bucketName"`.
On subsequent generation (`cf gen`), the composition produces:
```yaml
data:
  arn: {{ $spec.bucketName | quote }}
  prefixName: {{ $spec.bucketName | quote }}
```
The ARN prefix and wildcard suffix are lost entirely, causing deployed infrastructure to fail.

## Evidence

In `internal/adopt/adopt.go` around line 2938:
```go
if m := reParamVar.FindStringSubmatch(rawStr); len(m) >= 2 {
    if isValidParamIdentifier(m[1]) {
        out[path] = blueprint.Field{From: "params." + m[1]}
    }
...
} else if strings.Contains(rawStr, "{{") {
    out[path] = blueprint.Field{Raw: rawStr}
}
```
Because `reParamVar.FindStringSubmatch(rawStr)` performs an unanchored substring search, any string containing `{{ $spec.<param> }}` anywhere within it matches line 2938. `adopt` assigns `Field{From: "params." + m[1]}` and bypasses the fallback at line 2985 (`else if strings.Contains(rawStr, "{{") { out[path] = blueprint.Field{Raw: rawStr} }`), silently deleting all prefix and suffix text surrounding the template expression. The same applies to `matchEnvVar(rawStr)` and `reObservedStatus`.

## Acceptance test

```go
// internal/adopt/adopt_test.go
func TestAdoptGoTemplate_InterpolatedStringField(t *testing.T) {
	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-interpolated-string
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
            data:
              arn: "arn:aws:s3:::{{ $spec.bucketName }}/*"
              prefixName: "prefix-{{ $spec.bucketName }}"
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]

	arnField, ok := r.Fields["data[arn]"]
	if !ok {
		arnField, ok = r.Fields["data.arn"]
	}
	if !ok {
		t.Fatalf("data.arn field missing from fields: %+v", r.Fields)
	}
	if arnField.Raw != `"arn:aws:s3:::{{ $spec.bucketName }}/*"` && arnField.Raw != `arn:aws:s3:::{{ $spec.bucketName }}/*` {
		t.Errorf("arnField.Raw = %q, want interpolated raw string; From was %q", arnField.Raw, arnField.From)
	}

	prefixField, ok := r.Fields["data[prefixName]"]
	if !ok {
		prefixField, ok = r.Fields["data.prefixName"]
	}
	if !ok {
		t.Fatalf("data.prefixName field missing from fields: %+v", r.Fields)
	}
	if prefixField.Raw != `"prefix-{{ $spec.bucketName }}"` && prefixField.Raw != `prefix-{{ $spec.bucketName }}` {
		t.Errorf("prefixField.Raw = %q, want interpolated raw string; From was %q", prefixField.Raw, prefixField.From)
	}

	// Downstream round-trip check: emitting this blueprint produces valid template output
	outputs, err := emit.Generate(bp, nativeCRDs, "")
	if err != nil {
		t.Fatalf("emit.Generate failed: %v", err)
	}
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			s := string(o.Body)
			if !strings.Contains(s, "arn:aws:s3:::") {
				t.Errorf("emitted composition lost 'arn:aws:s3:::' prefix! Content:\n%s", s)
			}
			if !strings.Contains(s, "prefix-") {
				t.Errorf("emitted composition lost 'prefix-' prefix! Content:\n%s", s)
			}
		}
	}
}
```

**Fails today with:**
```
arnField.Raw = "", want interpolated raw string; From was "params.bucketName"
prefixField.Raw = "", want interpolated raw string; From was "params.bucketName"
```

## Contract

1. In `internal/adopt/adopt.go`:
   - When checking whether a scalar string matches `reParamVar`, `matchEnvVar`, or `reObservedStatus`, ensure the match spans the entire trimmed string (i.e. `strings.TrimSpace(rawStr) == m[0]` or equivalent), so that there is no preceding or trailing text.
   - If `rawStr` contains `{{` and `}}` but has preceding/trailing text or multiple template expressions, fall through to `Field{Raw: rawStr}` (and handle equivalently in slices/nested collections).
2. Pure parameter references (`"{{ $spec.foo }}"`) must continue to be adopted as wires (`From: "params.foo"`).

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/adopt/`.

## Handover

Branch `CF-313-adopt-interpolated-go-template-strings`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
