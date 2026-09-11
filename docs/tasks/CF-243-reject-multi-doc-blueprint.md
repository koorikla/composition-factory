# CF-243 — cf gen silently uses only the first YAML document of a blueprint file; an unparseable second document passes with exit 0

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/blueprint/yaml.go`, `internal/blueprint/load_test.go` (or `load.go`)

## Background & Problem
`cf gen` (and `cf serve`, `cf kinds`, `cf fields`, `cf package`) on a blueprint file containing more than one YAML document decodes only the first document and silently ignores everything after the first document.
Even a syntactically invalid second document (e.g. `{{{ not yaml`) passes with exit 0.
Mechanism:
In `internal/blueprint/yaml.go:13-17`:
```go
func yamlToJSON(body []byte) ([]byte, error) {
	var raw any
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	...
}
```
`yaml.Unmarshal` unmarshals the first document and terminates. Trailing documents are never parsed.

## Expected Behavior & Contract
A blueprint YAML input must contain exactly one YAML document.
If multiple YAML documents are present in the input:
- If a subsequent document exists or has syntax errors, `blueprint.Parse` (via `yamlToJSON`) returns an error (e.g. `blueprint must contain exactly one YAML document` or trailing document syntax error).
- Commands such as `cf gen` exit non-zero (exit 1) reporting the error.

## Acceptance Test
- Tests in `internal/blueprint/load_test.go` (and `cmd/cf/` if applicable):
  - A blueprint with a valid second YAML document (separated by `---`) fails parsing.
  - A blueprint with an invalid trailing document (`---` followed by invalid YAML) fails parsing with a syntax error.
