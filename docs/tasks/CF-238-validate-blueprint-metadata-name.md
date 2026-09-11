# CF-238 — PUT /api/blueprint accepts an empty or non-DNS metadata.name that GET /api/package 500s on

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/blueprint/validate_root.go`, `internal/blueprint/validate_root_test.go`, `internal/api/blueprint_test.go`, `internal/api/package_test.go`, `docs/tasks/CF-238-validate-blueprint-metadata-name.md`

## Background & Problem
`Blueprint.Validate` only runs `checkScalar` on `metadata.name` (`internal/blueprint/validate_root.go:17`), so `PUT /api/blueprint` persists `""`, `"My Blueprint"` or `"UPPER"` with 200.
The same server's `GET /api/package` then hands the name to `xpkg.WriteTarballTo` -> `name.NewTag` (`internal/xpkg/build.go:112-116`) and answers **500** with go-containerregistry's error message; `GET /api/package?format=yaml` answers 200 and writes the name verbatim into the Configuration's `metadata.name` (`internal/emit/configuration.go:86`) — a package manifest the Kubernetes API server will reject. `cf package` on the same file exits 1 with a "bad package tag" error.

## Expected Behavior & Contract
1. `validateRoot` validates `metadata.name`:
   - Must not be empty: `metadata.name is required`.
   - Must be a valid DNS subdomain name: lowercase alphanumeric characters, `-` or `.`, starting and ending with an alphanumeric character (matching `groupRE`), and not a YAML keyword (e.g. `true`, `false`, `yes`, `no`), with a max length of 253 characters.
   - If invalid, returns a clear error naming the field, e.g.:
     `metadata.name: %q is not a valid DNS subdomain name (must be lowercase alphanumeric characters, '-' or '.', and start and end with an alphanumeric character)` or `metadata.name: %q exceeds maximum length of 253 characters`.
2. `PUT /api/blueprint` rejects empty or non-DNS names with 400 Bad Request and the validation error message.
3. No request-data problem reaches `GET /api/package` as a 500 Internal Server Error.

## Acceptance Test
- Unit tests in `internal/blueprint/validate_root_test.go` checking acceptance of valid DNS subdomain names (including hyphens and dots, e.g. `sqs-queue`, `xworkloadfulls.platform.sparky.ee`) and rejection of empty names, spaces, uppercase letters, YAML keywords, and invalid boundary characters.
- API tests in `internal/api/blueprint_test.go` and/or `internal/api/package_test.go` asserting `PUT /api/blueprint` returns 400 Bad Request when `metadata.name` is empty or non-DNS.
