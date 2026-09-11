# CF-241 — cf adopt -b silently ignores a missing or unparseable base blueprint, then fails XRD-only imports with a misleading error

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `cmd/cf/adopt.go`, `cmd/cf/adopt_test.go` (or `cmd/cf/main_test.go`)

## Background & Problem
`cf adopt -b <path>` silently discards the base blueprint when the specified path does not exist or does not parse: exit 0, no warning or error, and adoption proceeds as if `-b` had not been supplied.
The base blueprint feeds parameter type, default, and description recovery, and is mandatory for XRD-only imports.
Mechanism:
In `cmd/cf/adopt.go:48-54`:
```go
	if bpPath != "" {
		if data, err := os.ReadFile(bpPath); err == nil {
			if baseBP, err := blueprint.Parse(data); err == nil {
				opts.BaseBlueprint = baseBP
			}
		}
	}
```
Both `os.ReadFile` and `blueprint.Parse` errors are silently dropped.
When an explicit `-b` path is provided and fails to read or parse, `cf adopt` should fail loudly (exit 1 with a descriptive error naming the path and error), rather than silently proceeding without the base blueprint.

## Expected Behavior & Contract
When `-b / --blueprint` is explicitly specified by the user:
- If the file cannot be read (e.g. does not exist, permission denied), `cf adopt` returns exit 1 and an error: `read base blueprint: <err>`.
- If the file content cannot be parsed as a blueprint, `cf adopt` returns exit 1 and an error: `parse base blueprint: <err>`.

## Acceptance Test
- Tests in `cmd/cf/` verifying that:
  - `cf adopt -b does-not-exist.cf.yaml ...` exits with non-zero status and an error indicating the file does not exist.
  - `cf adopt -b broken.cf.yaml ...` exits with non-zero status and an error indicating parse failure.
