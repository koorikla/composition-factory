# CF-040 — Three shipped features are described nowhere a user will look

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, what "done" means, and how you hand back. This brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `CF-040 The three features shipped in 075646f are documented nowhere: spec.environment and from: env.<key> absent from docs/dsl.md, cf function absent from docs/cli.md, and POST /api/preview-expression absent from docs/mcp.md.` |
| **Worktree** | `.worktrees/CF-040` on branch `CF-040-document-env-function-preview` |
| **May write** | `docs/dsl.md`, `docs/cli.md`, `docs/mcp.md`, `CHANGELOG.md` |
| **Merges after** | nothing — touches no file another open task writes |

## Symptom

`spec.environment`, `from: env.<key>`, `cf function`, and
`POST /api/preview-expression` all shipped in `075646f`. A user reading the docs
cannot discover that any of them exist. The only file in the repository describing
them is `docs/backlog-archive.md` — the record that they were *planned*.

## Evidence

```sh
$ grep -c "spec.environment\|from: env\." docs/dsl.md
0
$ grep -n "^### " docs/cli.md
9:### `cf version` — Print Version
19:### `cf gen` — Generate Manifests
75:### `cf serve` — Visual Canvas Server
97:### `cf adopt` — Ingest Existing Compositions
111:### `cf package` — Build Crossplane Configuration Packages
131:### `cf push` — Push Package to OCI Registry
146:### `cf provider add` — Cache Provider CRD Schemas
159:### `cf mcp` — Model Context Protocol Server
$ grep -c "preview-expression" docs/mcp.md
0
```

`cf function` exists (`cmd/cf/function.go`) and has no entry between `cf provider
add` and `cf mcp`. Verified twice on `3a3fef4`.

**One correction to the original finding:** it claimed `CHANGELOG.md [Unreleased]`
was empty. It is not — `3a3fef4` added an entry there describing the backlog audit.
It still does not mention the three features. Document them; do not delete what is
there.

## Location

- `docs/dsl.md` — `:121` lists `raw:` runtime variables, the natural neighbourhood
  for the `$env` binding. No `spec.environment` section exists to extend.
- `docs/cli.md:146-159` — where a `### cf function` entry belongs, in the same shape
  as its neighbours.
- `docs/mcp.md` — route inventory. Note it already documents only 13 of 31 routes
  (CF-036); this task adds one, it does not fix that.

## Acceptance test

**None — documentation.** There is no automated oracle for whether prose is true, and
inventing one (a grep asserting a heading exists) would guard the presence of a
string rather than the accuracy of a claim. That is exactly the weak-guard failure
mode this repository has been bitten by four times. Use the verification below
instead, and make every code claim in the prose one you ran.

## Contract

Each of the three features gets an entry where a user would look for it, written to
the standard of the surrounding text — behaviour first, then the consequence of
getting it wrong.

- **`spec.environment` and `from: env.<key>`** in `docs/dsl.md`: the declaration
  shape, the key types, and where env values may be consumed (fields, annotations,
  envelope, `when`, `forEach`). Two behaviours must be stated because both surprise:
  the emitted guard is `hasKey`, so an absent key silently emits nothing rather than
  failing; and `default:` on an environment key was **inert** (CF-009) until a fix
  landed on 2026-09-03. **Verify which behaviour is in the tree you are documenting**
  — run it, do not trust this brief — and document what you observe. Never document
  intended behaviour as though it worked.
- **`cf function`** in `docs/cli.md`: full entry in the neighbouring format —
  synopsis, flags, an example, and what lands in `.cf.lock`.
- **`POST /api/preview-expression`** in `docs/mcp.md`: request and response shapes,
  and its real error behaviour — it returns **200 with `{"rendered":"","error":…}`**
  on failure rather than a 4xx (CF-035). Document the shape callers actually get.
- A `CHANGELOG.md` `[Unreleased]` entry naming the three features. Add to the
  existing section; do not replace it.

Every example you write must be one you ran. If an example does not behave as the
prose says, that is a defect — report it in the handover and document the real
behaviour.

## Verification

Not a test; a review checklist for whoever merges. Run each and compare its output to
the prose you wrote:

```sh
./bin/cf function --help
./bin/cf gen <a blueprint using spec.environment> --out /tmp/envout && cat /tmp/envout/composition.yaml
curl -s -X POST localhost:$CF_E2E_PORT/api/preview-expression -d '{"expression":"{{ .bad "}' -i | head -1
make lint          # catches nothing here, but the gate is unconditional
```

Start any server you need on **your worktree's own port**, never 8080.

## Out of scope

Fixing CF-009 (inert `default:`), CF-035 (the 200-on-error envelope), or CF-036 (the
18 undocumented routes). Documenting the other verbs CF-043 covers (`cf kinds`,
`cf fields`, `cf catalogue`, `gen --validate`, `gen --group-suffix`) — that is a
separate item, and splitting them keeps the two diffs reviewable.

## Handover

Branch `CF-040-document-env-function-preview`, committed, not pushed, not merged.
List every command you ran to check an example, and every place the code did not
match what the docs were going to say.
