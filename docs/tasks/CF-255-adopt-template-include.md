# CF-255 — Import turns a template: field into a raw {{ include }} expression, leaking emitter boilerplate

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: round-trip fidelity) |
| **Closes** | `CF-255 — Import turns a template: field into a raw {{ include }} expression, leaking emitter boilerplate` (#134) |
| **Worktree** | `.worktrees/CF-255` on branch `CF-255-adopt-template-include` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Symptom

Re-importing a Composition generated with template fields (such as the Full-Stack starter) turns fields like `role.assumeRolePolicy` that originally had `template: trust-policy` into raw include expressions:
```yaml
raw: '{{ include "trust-policy" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "role" "field" "assumeRolePolicy") | trim | nindent 6 }}'
```
The define block itself is recovered into `spec.templates.trust-policy`, but the field reference is not recognized as a template reference.

## Mechanism

`internal/emit/composition.go` writes a `template:` field as:
`{{ include "<name>" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "<resource>" "field" "<field>") | trim | nindent <indent> }}`

`internal/adopt/adopt.go` extracts define blocks into `bp.Spec.Templates`, but `extractFields` and `extractEnvelopeFields` only recognize `$spec.<param>`, `env.<key>`, and observed status expressions. Any other template expression is classified as `Field{Raw: rawStr}`.

## Contract

1. In `internal/adopt/adopt.go`, recognize `{{ include "<name>" ... }}` expressions in both `extractFields` and `extractEnvelopeFields`.
2. When the include expression references a template `<name>`, set `Field{Template: "<name>"}` instead of `Field{Raw: ...}`.
3. Verify that round-tripping a blueprint containing `template:` fields (such as `role.assumeRolePolicy` with `template: trust-policy`) restores `template: trust-policy` losslessly.

## Acceptance Test

An automated unit test in `internal/adopt/adopt_test.go`:
- Adopts a Composition containing an emitted template include expression `{{ include "trust-policy" (dict ...) | trim | nindent 6 }}` alongside `{{- define "trust-policy" }}...{{- end }}`.
- Asserts that the corresponding resource field is adopted with `Field.Template == "trust-policy"` and `Field.Raw == ""`.
- Asserts that the adopted blueprint passes `bp.Validate()`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-255-adopt-template-include`, committed, not pushed, not merged.
