# CF-362 — cf package builds and writes package without validating blueprint via b.Validate()

## 1. Context & Invariant

Every `cf` CLI command operating on a blueprint enforces `b.Validate()`.
`cmd/cf/package.go` must call `b.Validate()` immediately after parsing the blueprint, before loading sources or writing artifacts.

## 2. Requirements & Contract

1. In `cmd/cf/package.go`:
   - Call `if err := b.Validate(); err != nil { return err }` after `blueprint.Parse(source)`.
2. Guard with automated test in `cmd/cf/package_test.go`:
   - `TestPackageRejectsInvalidBlueprint` verifying that an invalid blueprint returns an error.
3. Pass `make lint && make lint-strict && make test-race`.
