# CF-359 — cf catalogue --type accepts invalid package types and dumps entire unfiltered catalogue with exit 0

## 1. Context & Invariant

`cf catalogue --type <type>` allows users to filter catalogue entries by package type (`provider` or `function`).
When an unrecognized or invalid package type is passed (e.g. `--type bogus`, `--type providers`), `cf catalogue` should reject the invalid argument with an error naming the valid types (`provider`, `function`), rather than silently ignoring the filter and dumping the entire catalogue with exit code 0.

Similarly, `catalogue.Search(entries, query, typ)` must not treat unrecognized type filter strings as matching all packages; filtering by a non-matching or invalid type must return 0 results.

## 2. Requirements & Contract

1. In `catalogue/kinds.go`:
   - If `typ != "" && typ != "provider" && typ != "function"`, return `nil`.
2. In `cmd/cf/catalogue.go`:
   - Validate `c.Type`: if non-empty and not `"provider"` or `"function"`, return an error naming the invalid type and valid options.
3. Guard with automated unit tests:
   - `catalogue/kinds_test.go`: `TestSearchRejectsInvalidType`
   - `cmd/cf/catalogue_test.go`: `TestCatalogueCmd_RejectsInvalidType`, `TestCatalogueCmd_CLIInvocation_RejectsInvalidType`
4. Ensure `make lint && make lint-strict && make test-race` passes.
