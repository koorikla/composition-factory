# CF-289 — Python emitter outputs empty writeConnectionSecretToRef mapping when child parameters omitted

## Summary
In `internal/emit/python.go`, helper `_present(d)` strips entries whose values are `None`:
```python
def _present(d):
    """Filter out None values from a dict."""
    return {k: v for k, v in d.items() if v is not None}
```
When emitting envelope structures like `writeConnectionSecretToRef`, `pythonEnvelopeEntries` generates:
```python
"writeConnectionSecretToRef": _present({
    "name": oxr["spec"].get("secretName"),
    "namespace": oxr["spec"].get("secretNamespace"),
})
```
When the XR omits both optional fields, the inner `_present` returns `{}` (an empty dictionary).
Because `{}` is not `None`, the outer `_present` on `spec` retains `"writeConnectionSecretToRef": {}`.

In Crossplane and Kubernetes CRDs, `writeConnectionSecretToRef` requires `name` to be non-empty. Crossplane rejects the generated desired resource at apply-time with:
`spec.writeConnectionSecretToRef.name: Required value`.

In contrast, Go-templating (`internal/emit/envelope.go:530-537`) checks `allOptional` and wraps the entire key in `{{- if or ... }}`, omitting `writeConnectionSecretToRef` entirely when all child fields are absent.

## Affected Locations
- `internal/emit/python.go:123-125`
- `internal/emit/python.go:482-493`

## Steps to Reproduce
1. Create a blueprint with a resource having `writeConnectionSecretToRef` wired to optional parameters.
2. Run `cf gen --engine python`.
3. In the generated `function.py`, observe:
   `"writeConnectionSecretToRef": _present({...})`
4. When executed with an XR where both parameters are omitted, the function returns a desired resource containing `"writeConnectionSecretToRef": {}`.
5. Apply to Crossplane or inspect schema: fails schema validation because `name` is required if `writeConnectionSecretToRef` is present.

## Expected Behavior
`_present` or the python emitter should ensure empty dictionaries (`{}`) for optional envelope structures are stripped, or `_present` should recursively omit empty dictionaries (or check `if v not in (None, {})`).

## Severity
`severity:P2` — Engine parity and runtime schema validation error in Crossplane.
