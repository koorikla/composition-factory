# Task Brief: CF-342 (Issue #231) — adopt degrades single-quoted environment index references into raw strings

## Problem Statement

In `internal/adopt/adopt.go`, environment references in field values are extracted using `reEnvVar`:
```go
reEnvVar = regexp.MustCompile(`\{\{-?\s*(?:default\s+(?:"[^"]*"|\S+)\s+)?(?:\$env\.([a-zA-Z0-9_.-]+?)|\(index\s+\$env\s+"([a-zA-Z0-9_.-]+?)"\)|index\s+\$env\s+"([a-zA-Z0-9_.-]+?)")(?:\s*\|\s*quote)?\s*-?\}\}`)
```
In both index forms, the regex hardcodes double quotes (`"([a-zA-Z0-9_.-]+?)"`).
When a Go template composition indexes `$env` using single quotes:
- `region: {{ index $env 'region' }}`
- `region: {{ (index $env 'region') }}`
- `region: {{ default 'us-east-1' (index $env 'region') }}`

1. `reEnvVar` fails to match the expression.
2. In `normalizeFieldWires`, the field is degraded into a raw string (`Field{Raw: rawStr}`), leaving `From` empty.
3. The environment key is omitted from `bp.Spec.Environment`.
4. In the Blueprint IR, the environment dependency is missing, violating the Round-Trip Rule (`AGENTS.md` §1).

## Scope of Changes

- In `internal/adopt/adopt.go`:
  - Update `reEnvVar` (and any related regexes in `internal/adopt` scanning environment expressions) to admit single-quoted environment keys (`["']([a-zA-Z0-9_.-]+?)["']`).
- In `internal/adopt/adopt_test.go`:
  - Add test `TestAdoptGoTemplate_SingleQuotedEnvVarWire` verifying that single-quoted index expressions like `{{ index $env 'region' }}` extract `From: env.region` and populate `bp.Spec.Environment["region"]`.

## Acceptance Test

Verbatim test in `internal/adopt/adopt_test.go`:
- `TestAdoptGoTemplate_SingleQuotedEnvVarWire`
