# CF-280 — SampleXR drops optional parameters with defaults, failing render check under missingkey=error

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-280 — SampleXR drops optional parameters with defaults, failing render check under missingkey=error` |
| **Worktree** | `.worktrees/CF-280` on branch `CF-280-sample-xr-drops-defaulted-parameters` |
| **May write** | `internal/emit/sample_xr.go`, `internal/emit/sample_xr_test.go`, `internal/api/render_test.go` |
| **Merges after** | nothing |

## Symptom

`SampleXR` (`internal/emit/sample_xr.go`) synthesizes sample Composite Resource (XR) manifests used by `RenderCheck`, `cf gen --validate`, and `POST /api/render`.

In `internal/emit/sample_xr.go`:
```go
for name, p := range b.Spec.XRD.Parameters {
    if !p.Required && !isForEachParam(b, name) {
        continue
    }
    spec[name] = placeholderValue(p)
}
```

When an XRD parameter is not required (`p.Required == false`), but carries a schema default (`p.Default != ""`), `SampleXR` completely omits it from `spec`. Even though `placeholderValue(p)` explicitly implements parsing for `p.Default != ""` and `case "object"` checks `member.Required || member.Default != ""`, top-level parameters with defaults are skipped by the loop filter.

This breaks render validation in two critical ways:
1. **`when` conditions hard-fail under `missingkey=error`**:
   `internal/blueprint/validate_resources.go` allows optional parameters in `when` conditions as long as they carry a default:
   ```go
   if !decl.Required && decl.Default == "" {
       return fmt.Errorf("resource %q: when parameter %q must be required or carry a default -- "+
           "the condition dereferences it unguarded, and under options: [\"missingkey=error\"] "+
           "an absent key hard-fails the whole render; only the XRD's required gate or its "+
           "schema default makes the key's presence unconditional", r.Name, name)
   }
   ```
   `whenCondition` in `internal/emit/composition.go` emits an unguarded `$spec.<name>` dereference (e.g. `{{- if eq $spec.tier "pro" }}` or `{{- if $spec.replicasEnabled }}`). Because `SampleXR` omits `tier` and `replicasEnabled`, `crossplane render` (which runs offline without Kubernetes API server schema defaulting) fails under `missingkey=error`:
   `template: manifests:12:14: map has no entry for key "tier"`
2. **Field wiring causes false-positive schema validation errors**:
   When `r.Fields` wires an optional parameter with a default into a resource field, `chainGuard` guards the field emission with `{{- if hasKey $spec "<name>" }}`. Because `SampleXR` omits `<name>`, the field is not rendered. If the provider CRD requires that field, `ValidateRenderedWithBlueprint` fails with a false-positive missing required field error, despite the user having provided a default in the XRD.

`TestSampleXRUsesTypeAppropriatePlaceholders` in `internal/api/render_test.go` codified this bug by asserting that `"maxDepth": {Type: "integer", Default: "4"}` was omitted from `xr.Spec`, under the mistaken assumption that compositions inject parameter defaults at runtime. As `composition.go` lines 233-237 explains, parameter defaults are injected by Kubernetes API server schema defaulting upon XR admission, not by the composition function.

## Contract

1. **Include Defaulted Parameters in `SampleXR`**:
   Update `SampleXR` in `internal/emit/sample_xr.go` so that any parameter with `p.Default != ""` is populated into `spec`, alongside `p.Required` parameters and `isForEachParam(b, name)`:
   ```go
   if !p.Required && p.Default == "" && !isForEachParam(b, name) {
       continue
   }
   ```
   For object parameters, if any declared property has a default or is required, and the parent object has properties populated, ensure `spec[name]` is properly set.
2. **Update API Render Test Expectations**:
   Update `TestSampleXRUsesTypeAppropriatePlaceholders` in `internal/api/render_test.go` so that defaulted parameters (such as `maxDepth: {Type: "integer", Default: "4"}`) are asserted to be present with their parsed default value (`float64(4)`), while genuinely optional parameters with no default (such as `comment: {Type: "string"}`) remain omitted.
3. **Verification Gates**:
   - `make lint` must pass.
   - `make lint-strict` must pass.
   - `make test-race` must pass.

## Acceptance Test

1. In `internal/emit/sample_xr_test.go`, add `TestSampleXRWithDefaultedParameters`:
   - Define a blueprint with:
     - a required parameter (`providerName`).
     - an optional parameter with a default (`tier` with `default: standard`).
     - an optional parameter without a default (`comment`).
     - a resource with `when: params.tier == "pro"`.
   - Call `SampleXR(b)`.
   - Verify `xr.Spec` contains `providerName` and `tier` with value `"standard"`.
   - Verify `xr.Spec` does not contain `comment`.
   - Render the emitted composition template with `xr.Spec` under `missingkey=error` and verify that execution succeeds without any missing key errors.
