# Skylos trial (2026-09-10, tree at 367928a)

`skylos 4.36.1` (Apache-2.0), run as `skylos . -a --json --no-upload --force --exclude node_modules
--exclude .worktrees …`. The Go engine is not in the pip package: it was built from the source
checkout (`skylos/engines/go`, `go build ./cmd/skylos-go`) and passed via `SKYLOS_GO_BIN`; without
it the run exits 2 and skips all Go checks. Whole run: 13 s JS/config, ~40 s with Go.

## What it reported, and what survived a look

| Family | Reported | Real | Notes |
|---|---|---|---|
| JS dead code | 1 function, 8 variables, 10 unused exports | most | `qsa` and the exports are already in CF-104; the 8 variables (`gestureActive`, `pendingRender`, `rafWires`, `inited`, `booted`, `clusterLoading`, …) are new but trivial |
| JS `innerHTML` "XSS" (SKY-D226) | 30 HIGH | 0 seen | sampled sinks assign strings built with `esc()`; provider CRD descriptions reach markup via `portRow` → `esc(opts.title)`. The tool cannot see the escaping; a real audit needs data-flow, not a sink grep |
| JS timing-unsafe compare (SKY-D253) | 3 | 0 | `if (t !== renderToken)` — a render-sequence guard, not a secret |
| "Hallucinated Go dependency" (SKY-D222) | 3 CRITICAL | 0 | `github.com/Masterminds/sprig/v3`, `goutils`, `semver/v3` — all real modules in the proxy |
| Go dependency CVE (SCA) | 1 UNKNOWN | not reachable | `govulncheck ./...` (authoritative): "0 vulnerabilities … 1 in modules you require, but your code doesn't call it" |
| Go dead code | 5 functions, 1 class, 1 variable | 0 | kong `Run` methods (reflection), `ImportCmd` alias, `rhsUnset` iota zero value |
| Go path traversal (SKY-D215) | 48 HIGH | 0 | CLI file arguments; that is what a CLI does |
| Go SSRF (SKY-D216) | 4 CRITICAL | 0 | in-process MCP bridge with a fake host, cluster client, a codegen script |
| Go hardcoded secrets (SKY-S101) | 18 HIGH | 0 | strings containing "Secret": kind names, an error message |
| Go unclosed resource (SKY-G260) | 1 | 0 | `internal/rendertest/lock.go` closes on both paths |
| Quality (length/complexity/nesting) | 628 | known | same hot spots as docs/code-audit.md §7 |
| GitHub Actions config | 33 | **yes** | unpinned action refs, workflow-level `contents: write`, `persist-credentials` default, `curl … \| sh` for the crossplane CLI, no `timeout-minutes` on release/catalogue jobs, image tags not digests. Filed as CF-135 |

## Verdict

- Not a gate for this repo: every CRITICAL/HIGH in Go and JS code was a false positive, and a
  gate that cries wolf gets `--force`d. Keep `make lint`/`lint-strict` (gofmt, vet, staticcheck,
  eslint) as the gates.
- Worth adding as gates instead, both cheap and precise: `govulncheck ./...` (clean today) and a
  workflow linter (`zizmor` or `actionlint`) once CF-135 lands, so the Actions findings cannot regress.
- Worth an occasional manual run (`make audit-skylos`, not in CI) for the JS dead-variable and
  unused-export families, which `deadcode` does not cover and eslint is not configured for; or
  configure `no-unused-vars` in `eslint.config.js` and drop the extra tool.
