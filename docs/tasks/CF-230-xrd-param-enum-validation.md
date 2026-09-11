# CF-230 — blueprint validator accepts non-conforming enum values and defaults outside enum for XRD parameters

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/blueprint/load.go`, `internal/blueprint/load_test.go`, `docs/tasks/CF-230-xrd-param-enum-validation.md`

## Background & Problem
`blueprint.Validate` allows non-string XRD parameters (`integer`, `number`, `boolean`) to declare non-conforming `enum` entries, and allows parameters to declare a `default` that is not among the declared `enum` choices. `cf gen` generates an invalid OpenAPI v3 schema or an unusable default without error.

In `internal/blueprint/load.go:179-183` (`validateParameterScalars`):
```go
	for i, e := range p.Enum {
		if err := checkScalar(fmt.Sprintf("%s.enum[%d]", fieldPath, i), e); err != nil {
			return err
		}
	}
```
`checkScalar` only checks for newlines and unprintable characters. While `p.Default` is validated against `p.Type` via `strconv.ParseInt` / `strconv.ParseFloat` in lines 191-209, each `p.Enum` entry is never type-checked against `p.Type`. In `internal/emit/xrd.go:153-157` (`enumYAML`), non-string types are rendered bare (`- not_an_int` under `type: integer`).
Furthermore, `validateParameterScalars` does not verify that `p.Default` belongs to `p.Enum` when both are defined (`if len(p.Enum) > 0 && p.Default != "" && !slices.Contains(p.Enum, p.Default)`).

## Expected Behavior & Contract
1. In `validateParameterScalars`:
   - If `len(p.Enum) > 0`:
     - If `p.Type` is `"object"` or `"array"`: reject with error naming `fieldPath` and `p.Type`.
     - If `p.Type == "boolean"`: each entry `e` in `p.Enum` must be `"true"` or `"false"`. Otherwise return error: `%s: enum entry %q is not a valid boolean (must be "true" or "false")`.
     - If `p.Type == "integer"`: each entry `e` in `p.Enum` must parse via `strconv.ParseInt(e, 10, 64)`. Otherwise return error: `%s: enum entry %q is not a valid integer`.
     - If `p.Type == "number"`: each entry `e` in `p.Enum` must parse via `strconv.ParseFloat(e, 64)`. Otherwise return error: `%s: enum entry %q is not a valid number`.
   - If `len(p.Enum) > 0 && p.Default != "" && !slices.Contains(p.Enum, p.Default)`:
     - Return error: `%s: default %q is not in enum %v`.

## Acceptance Test
Add tests in `internal/blueprint/load_test.go`:
- `TestValidateParameterEnumConformance`:
  - Parameter of type integer with non-integer enum entries (`["not_an_int"]`) fails validation with error mentioning `enum entry "not_an_int" is not a valid integer`.
  - Parameter of type number with non-number enum entries (`["abc"]`) fails validation with error mentioning `enum entry "abc" is not a valid number`.
  - Parameter of type boolean with non-boolean enum entries (`["maybe"]`) fails validation with error mentioning `enum entry "maybe" is not a valid boolean`.
  - Parameter with default outside declared enum choices fails validation with error mentioning `default "staging" is not in enum [dev prod]`.
  - Valid parameters with conforming enum entries and valid defaults pass validation cleanly.
