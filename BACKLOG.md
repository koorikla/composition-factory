# Backlog

Open work only — concise, prioritized, and verified against the codebase.

Completed work is archived in [docs/backlog-archive.md](docs/backlog-archive.md); full history is in `git log -p BACKLOG.md`.

---

## Architectural Principle: DSL (`.cf.yaml`) as Canonical Intermediate Representation

The `factory.crossplane.io/v1alpha1` `Blueprint` document (`.cf.yaml`) is and remains the single source of truth and intermediate representation (IR) for `composition-factory`. 

All user interfaces (Canvas, CLI, API, MCP) operate on this model. Crossplane manifests (`composition.yaml`, `definition.yaml`, `functions.yaml`, `package.yaml`) are deterministic, generated artifacts. Manifest import and adoption act as high-fidelity converters *into* the canonical Blueprint format.

---

## Architectural Principle: The Round-Trip Rule

**Anything cf generates must survive Kubernetes and come back.** Apply it to a
real cluster, read it back with `kubectl get <kind> -o yaml`, and cf must be
able to import that — the server-round-tripped form, not just the file cf
wrote. The API server defaults fields, reorders maps, injects `managedFields`,
`creationTimestamp`, `uid`, `resourceVersion` and `status`, and prunes anything
the schema does not know; an importer that only reads cf's own output has not
been tested against the only version of the document that matters operationally.

Recorded in AGENTS.md §1 as an Engine Truth, so it binds every agent and not
just this backlog.

This is the acceptance bar for Track 1, and it is testable rather than
aspirational: `cf gen` → `kubectl apply` → `kubectl get -o yaml` → `cf import`
→ `cf gen` must reproduce the original bytes, with the server-added fields
scrubbed and named in a loss report. Lane C already stands up the kind cluster
this needs on every push, so the oracle exists — it just is not pointed at this
yet. Any generated artifact that cannot make the trip is a bug in the emitter,
not an exception for the importer to special-case.

---

---

## Found 2026-09-03 — four end-to-end journeys against `075646f`

Method: four agents built a composition end to end and reported only what they
personally triggered, each finding reproduced at least twice — CLI-only, canvas,
HTTP API, and a probe of the three features that shipped that morning. Plus a
static pass (gofmt/vet/staticcheck/deadcode/coverage). Items marked **[V]** were
re-verified by hand afterwards, independently of the agent that found them.

Static gates are all clean and stay clean: gofmt, vet, staticcheck, race, 859 Go
tests, 167 Playwright behaviours. Every defect below is in behaviour those gates
do not reach.

### P0 — the tool emits wrong output, or dies, and says nothing

- [ ] **One preview request kills `cf serve`. [V]** `POST /api/preview-expression`
      with a self-recursive template overflows the stack; Go's `fatal error:
      stack overflow` is a runtime fatal that `net/http` cannot recover, so the
      process dies and the author's canvas session with it. `internal/emit/preview.go`
      calls `tmpl.ExecuteTemplate` from its own `include` with no depth counter,
      so each recursion starts a fresh `Execute` and Go's 100000-depth guard
      never fires. Repro: `{"expression":"{{ define \"r\" }}{{ include \"r\" . }}{{ end }}{{ include \"r\" . }}"}`.
      Add a depth counter, a `context.WithTimeout`, and an output-size cap.
- [ ] **Preview has no execution bound.** `{{ range until 200000 }}{{ range until
      200000 }}{{ end }}{{ end }}` still burns 209% CPU long after the client
      disconnects — no deadline, no `r.Context()` cancellation, no output cap. This
      endpoint was specced to run *while typing*.
- [ ] **The auto-injected `function-environment-configs` step carries no `input:`. [V]**
      So it selects no EnvironmentConfigs, `.context["apiextensions.crossplane.io/environment"]`
      stays empty, every emitted `hasKey $env "…"` guard is false, and every
      env-derived field, annotation, envelope entry, `forEach` and `when` silently
      vanishes at reconcile. `cf gen` reports nothing. The default path of the
      environment feature cannot work.
- [ ] **KCL and Python flatten dotted field paths into literal keys. [V]**
      `bucketRef.name: {value: x}` emits `"bucketRef.name" = "x"` instead of a
      nested object; the API server prunes it. go-templating nests correctly — the
      v0.8.0 fix never propagated to the other two engines. Engine parity is a
      headline feature.
- [ ] **`raw:` is emitted verbatim, and the docs' own examples are bare
      expressions.** `docs/dsl.md` shows `{raw: 'printf "%s-subnet-%d" $xr $i'}`
      under a heading promising `$xr`/`$i` access. It lands in the composed
      resource as that literal string, `cf gen` exits 0 and `--validate` reports
      ok. Either auto-wrap bare expressions or reject a `raw:` containing `$var`
      with no `{{`.
- [ ] **`$observed` is documented but never bound.** `docs/dsl.md:121` lists it as
      a `raw:` runtime variable; the emitted preamble binds only `$spec`, `$xr`,
      `$xrMeta`, `$env`, `$i`. Every template following the doc fails to parse at
      render: `undefined variable "$observed"`. The working form, `$.observed.resources`,
      is what the generator itself emits and never documents.
- [ ] **Function-input validation is bypassed by the input's own `apiVersion`/`kind`.**
      `internal/emit/pipeline.go` resolves the Input CRD from the input document
      rather than from the step's `functionRef`, so `apiVersion: totally.made.up/v1`
      turns validation off entirely and emits verbatim. A `kind: GoTemplate` input
      under `functionRef: function-environment-configs` is likewise accepted.
- [ ] **The "uncached function ⇒ explicit warning" contract emits nothing.** Two
      defects: `internal/emit/composition.go` discards the `warnings` slice from
      its only call to `ValidatePipelineInputs`, and `isFunctionCached(pkgRef, crds)`
      never reads `pkgRef` — it returns true if *any* cached CRD is a function
      input, so once one function is cached no step is ever uncached.
- [ ] **`spec.environment` `default:` is inert.** Parsed, type-checked, and written
      into the annotation, but `internal/emit` reads only `.Type`, never `.Default`.
      The template guards on `hasKey` and emits nothing when the key is absent, so a
      declared default never applies — the author writes `default: 3` and gets zero
      resources instead of three. Either emit the default or reject the field.

### P1 — round-trip losses (Engine Truth #5)

The Lane C gate is real and green. It round-trips `internal/examples/k8s-workload.cf.yaml`,
which composes only native kinds wired by `metadata.name` — no managed resources,
no `atProvider`, no `spec.environment`. **[V]** That is precisely the one shape
that exercises none of the losses below. A real gate over a fixture that cannot
fail reads exactly like a passing gate.

- [ ] **Widen the Lane C round-trip fixture** to a blueprint with a managed
      resource, an `atProvider` status wire, an envelope, a `forEach`, and
      `spec.environment`. Do this first — it turns most of the items below into
      failing tests instead of prose. `docs/backlog-archive.md` already records
      "extend the gate to an example that uses env" as complete; it is not.
- [ ] `cf adopt` drops `atProvider.` from status wires — `resources.r.status.atProvider.arn`
      returns as `resources.r.status.arn`, which then fails regeneration.
- [ ] `cf adopt` emits `provider: ""` for every resource, so an adopted blueprint
      never regenerates; `--provider` stamps one ref onto all resources including
      native kinds. Infer it from the composed `apiVersion` — the cache already
      maps it.
- [ ] `cf adopt` silently flips `required: true` → `false` and drops `default:`,
      even with a sibling `definition.yaml` present. README promises lossy items
      are "named on screen"; nothing is named.
- [ ] Envelope is dropped entirely by adopt — the emitted guarded
      `writeConnectionSecretToRef` does not come back, so the connection secret
      silently stops being written. Reproduces for `{value:}` too, not just env.
- [ ] `forEach` resource names are destroyed — import invents `cf-expr-N`,
      changing every `crossplane.io/composition-resource-name`, so re-applying
      orphans and recreates the whole loop. The name is recoverable: it is in the
      template's own `printf`.
- [ ] Function package pins do not survive: adopt substitutes catalogue defaults
      (a silent version downgrade *and* registry change), and `.cf.lock` is never
      read at gen time — inputs are validated against the pinned schema and
      deployed against a hard-coded different version.
- [ ] Foreign env inference always types keys `string`, so any composition using
      env in a `forEach` or `when` without cf's annotation cannot be adopted at
      all — hard exit 1. Infer from the usage site (`until (int $env.x)` ⇒ integer).
- [ ] Flow-style `raw:` maps (`{app: worker}`) come back as expanded block maps —
      semantically equal, byte-different, so the widened gate above will fail on
      them until adopt preserves the style or the emitter normalises it.

### P2 — API contract

- [ ] **`raw:` is invisible to the edit routes.** Deleting a parameter used only
      inside a `raw:` expression returns 200 and orphans it (the XRD drops the
      property, the Composition still dereferences it under `missingkey=error`);
      renaming one rewrites `from:` but not `raw:`. Renaming a *resource* rewrites
      both — the scanning machinery exists and these two paths do not call it.
      The `from:` path correctly returns 409.
- [ ] **No route creates or updates a resource** — only rename and delete, while
      parameters get the full set. An agent must read-modify-write the whole
      blueprint for every field edit, and there is no ETag, so two agents on one
      workspace clobber each other. Add `POST /api/blueprint/resources` and
      `PUT /api/blueprint/resources/{name}`, and an `If-Match`.
- [ ] `/api/preview-expression` returns **200 for every failure** in a third error
      envelope (`{"rendered":"","error":…}`), and accepts a `resource` that does not
      exist with a confident `{"rendered":"sample"}`. Its `$env` is a hard-coded
      `{env, region, account}` fixture that ignores `spec.environment`, so it
      reports a false error for declared keys and renders undeclared ones.
- [ ] One upstream registry 404 yields 502 from `POST /api/providers` and 400 from
      `PUT /api/blueprint`. Also: `PUT /api/blueprint` makes synchronous network
      calls, so an offline agent editing an unrelated field gets a 400 on a
      blueprint that is valid.
- [ ] No route listing and no OpenAPI; `docs/mcp.md` inventories 13 of 31 routes.
      A previously archived backlog item that never shipped.
- [ ] MCP parity has drifted: still 13 tools, with no `add_function` and no
      `preview_expression`, while `docs/mcp.md:3` claims `cf mcp` serves "the full
      authoring surface".

### P2 — authoring UX

- [ ] Nearest-match suggestions never fire for names of 4 characters or fewer —
      `internal/blueprint/closest.go` requires `bestDist*2 < len(target)`, and a
      transposition in a 4-char name is distance 2. So `spce`→`spec` and
      `teir`→`tier` get no suggestion while 5-char cases all work. Inconsistent
      rather than absent, which is worse.
- [ ] Required CRD fields are never checked: omitting a required field, or feeding
      one from an optional parameter, emits happily and `--validate` passes. The
      analysis exists — the `when:`-on-optional-param refusal proves it.
- [ ] `cf gen --check` reports drift on orphaned files in its own output tree that
      `cf gen` will never remove, so the remedy it prints does not fix it.
- [ ] No CLI scaffold. `cf --help` has no `init`, and the `providerName` error tells
      a CLI user to start a web server. `cf init` emitting a minimal valid
      blueprint would remove the only rough step in an otherwise smooth CLI.
- [ ] `cf function add <provider>` succeeds with "0 function input schemas of 8
      CRDs" and files it under `functions` in `.cf.lock`; `cf provider add` on a
      function package does the mirror image. Neither says the package is the wrong
      sort.
- [ ] `blueprint.EnvRef` is dead while four production sites hand-roll
      `strings.CutPrefix(x, "env.")` (`envelope.go:74`, `load.go:527`, `refs.go:61`,
      plus three `"env." + key` concatenations in `adopt.go`). It sits beside
      `ParamRef`/`StatusRef`/`MetadataRef` as the canonical parser and has zero
      callers.

### P3 — documentation, all verified against the code

- [ ] The three features shipped in `075646f` are documented nowhere: `spec.environment`
      and `from: env.<key>` absent from `docs/dsl.md`, `cf function` absent from
      `docs/cli.md`, `POST /api/preview-expression` absent from `docs/mcp.md`, and
      `CHANGELOG.md` `[Unreleased]` empty. The only file mentioning them is
      `docs/backlog-archive.md` — the record that they were planned.
- [ ] `docs/dsl.md` documents a pipeline shape the API rejects outright
      (`spec.pipeline.steps[]` with `step:` and `functionRef: {name:}`); the real
      type is a bare list with a string `functionRef`. `input:`, `package:` and
      `position:` are undocumented.
- [ ] `providerName` is mandatory in every Namespaced XRD (since `scope: Cluster`
      is refused) and appears in no user-facing doc — the first-blueprint stumble.
- [ ] `cf kinds`, `cf fields`, `cf catalogue`, `gen --validate` and `gen
      --group-suffix` are documented nowhere. The three discovery verbs are the
      best UX in the tool.
- [ ] The FileSystem output tree in `docs/cli.md` does not match what is written
      (actual output nests under the XRD name — the real layout is better; fix the
      doc), and the `template:` refusal under kcl/python is undocumented.

### Notably solid — do not regress these

Reported independently by more than one journey: schema and type validation on
`from:`/`value:` is excellent and names the consequence, not just the rule
(`"the wire would render a YAML scalar of the wrong type, which the API server
rejects on apply"`). Environment-key validation is complete across fields,
annotations, envelope, `when` and `forEach`. The `factory.crossplane.io/environment-keys`
annotation survives a realistic server round-trip including `managedFields` and
`last-applied-configuration`. Byte-determinism holds — `cf package` produced an
identical `.xpkg` twice. KCL refuses `spec.environment` cleanly, as specced.
Malformed-body handling is uniform 400 across every route tested. The discovery
verbs, the provenance headers, and `cf function add` all worked first try.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
