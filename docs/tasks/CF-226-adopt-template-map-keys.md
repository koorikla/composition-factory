# CF-226 — adopt leaks __CF_EXPR tokens into blueprint field names when template map keys have expressions

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`, `docs/tasks/CF-226-adopt-template-map-keys.md`

## Background & Problem
When adopting a Go-template composition whose template contains mustache interpolations in map keys (e.g. `{{ .spec.foo }}: bar` or `tags: { "{{ .spec.tagKey }}": "value" }`), the pre-adoption tokenizer masks mustache expressions as `__CF_EXPR_%d__`.

Because the blueprint IR only represents static field path keys, these masked tokens leaked directly into the blueprint's field paths (e.g. `spec.forProvider.tags.__CF_EXPR_0__`). Subsequent `cf gen` invocations output invalid manifests containing `__CF_EXPR_0__`.

## Expected Behavior & Contract
When map keys in Go templates contain expressions, `adopt` must detect dynamic expression tokens in keys, drop them from the extracted static blueprint fields, and record the dropped dynamic key in `LossReport`.

## Acceptance Test
- `internal/adopt/adopt_test.go:TestAdoptTreeDropsMustacheExprInMapKeys`
