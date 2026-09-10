# CF-103 — Docs state things the tree contradicts

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `#10` — `CF-103 — Docs state things the tree contradicts (ports 8081/8086, Go 1.25, the cf subcommand list, blueprints/xqueue.cf.yaml, the "frozen" store/api contracts, make lint scope, no [0.10.0] changelog entry, cf adopt accepting directories and the import alias)` |
| **Worktree** | `.worktrees/CF-103` on branch `CF-103-docs-drift` |
| **May write** | `CONTRIBUTING.md`, `README.md`, `docs/record-demos.md`, `web-proto/README.md`, `docs/mcp.md`, `CHANGELOG.md`, `docs/cli.md` |
| **Merges after** | `nothing` |

## Symptom

Documentation across several markdown files contains outdated information and contradicts actual implementation in the codebase regarding port allocation, Go versions, CLI subcommands, file paths, and frozen contracts.

## Evidence

Documented in `docs/code-audit.md` §3 (D1–D7, D9, D11):
- **D1. CONTRIBUTING.md §3 "Port Allocation Contract"**: States port 8081 for e2e and 8086 for demo recorder, whereas `AGENTS.md` §2, `playwright.config.js` and `scripts/record-demos/run.sh` use dynamic port ranges (18000–27999 and 28000–37999).
- **D2. docs/record-demos.md:10**: Claims port 8086 instead of dynamic range.
- **D3. Go version**: README.md:242 and CONTRIBUTING.md:11 claim Go 1.25+, whereas `go.mod`, `ci.yml`, and `Dockerfile` specify Go 1.27.
- **D4. CONTRIBUTING.md §2**: CLI subcommands list misses `init`, `function`, `kinds`, `fields`, `catalogue`, and `adopt` alias `import`.
- **D5. Non-existent path `blueprints/xqueue.cf.yaml`**: in `web-proto/README.md:11` and `docs/mcp.md:26, 36` — should be `testdata/xqueue.cf.yaml`.
- **D6. web-proto/README.md:20-21**: Obsolete "frozen contract, do not edit" warnings on `store.js` and `api.js`.
- **D7. `make lint` description**: Omits `npm run lint:js` in `AGENTS.md`, `README.md`, `CONTRIBUTING.md`.
- **D9. CHANGELOG.md**: Missing `## [0.10.0]` section for `v0.10.0` release.
- **D11. docs/cli.md**: `cf adopt` accepts directory and `import` alias; `--file` alias for `cf serve --blueprint`.

## Location

- `CONTRIBUTING.md`
- `README.md`
- `docs/record-demos.md`
- `web-proto/README.md`
- `docs/mcp.md`
- `CHANGELOG.md`
- `docs/cli.md`

## Acceptance test

Acceptance test: none — documentation.

## Contract

Update the specified documentation files to accurately reflect the current reality of the codebase as detailed in the evidence section. Do not alter executable logic or break any existing code.

## Verification

```sh
make lint && make test-race
```

## Out of scope

Modifying Go code or deleting tracked code files.

## Handover

Branch `CF-103-docs-drift`, committed, not pushed, not merged. In your final report: list each documentation correction made with before/after quotes, and every gate run.
