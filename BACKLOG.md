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

## Planned — the three untyped surfaces (designed 2026-09-03)

cf refuses any provider field it cannot check against a real CRD schema. Three
surfaces escape that, and they are the three an author hits the moment a
composition stops being simple: a pipeline step's `input:` (a free-text
textarea), the environment (no source at all), and Go-template expressions in
`raw:` (hand-written, unverified until apply).

Ordered by dependency — 1 gives 2 its editing surface, and 2 gives 3 a source to
offer. KCL trails deliberately (see the last item of 2).

### 1. Schema-aware function inputs

First because it is almost entirely reuse, and it closes the one gap Kubernetes
will never close for us. Two verified findings in
`docs/research/2026-08-27-crossplane-generator-research.md`:

- **:1609 [V]** Function packages ship their Input CRD in the package layer
  (`gotemplating.fn.crossplane.io/v1beta1/gotemplate.yaml`). `internal/xpkg`
  already pulls package layers — pointed at a Function ref it gets that schema
  with no new fetch machinery.
- **:497 [V]** That Input CRD is generated but *never installed*, so the API
  server never validates a pipeline step's input at all. A typo reaches
  reconcile time and fails there. The research's own conclusion: "the generator
  must validate its own output."

Today `web-proto/js/regions/inspector.js` offers a free-text `functionRef`, a
free-text `package`, and a three-row "Input YAML" textarea.

- [ ] Cache Function Input CRDs through the existing xpkg → schema → index path,
      keyed by their own API group so they cannot collide with provider MR
      kinds. Pin the ref in `.cf.lock` the way a provider is pinned.
- [ ] Validate `spec.pipeline[].input` against the resolved Input CRD at author
      time: unknown paths fail through the same `unknownPath` helper every other
      path check uses, with nearest-match suggestions. A step whose function is
      not cached is accepted with an explicit warning — never silently.
- [ ] Inspector renders the typed form the resource inspector already uses for
      `forProvider` (required badges, descriptions, field modes) in place of the
      textarea. Raw YAML stays as the escape hatch for uncached functions.
- [ ] The package field offers the versions the catalogue already knows, the way
      provider refs are pinned today.

### 2. EnvironmentConfig

In Crossplane v2 `spec.environment` is gone (verified,
`2026-08-27-crossplane-generator-research.md:917`). The environment reaches a
composition only via `index .context "apiextensions.crossplane.io/environment"`,
and only if `function-environment-configs` ran earlier in the pipeline. The
taxonomy verdict (`2026-08-27-composition-pattern-taxonomy.md:961`) is that this
is a blueprint section plus a mapping source, not a node type — 9–18% adoption
in every corpus except Upbound's zero.

**Decided:** keys are declared in the blueprint, not read from a cluster.
EnvironmentConfigs carry no schema, and reading a live cluster would make
generation depend on cluster access and stop being reproducible offline. The
declaration is the contract; cluster drift is a deploy-time concern.

- [ ] `spec.environment` declares the keys a blueprint relies on, and their
      types. The refs themselves stay in the `function-environment-configs`
      step, typed by item 1 — one fact, one home; do not model refs twice.
- [ ] `from: env.<key>` wherever `from:` is already legal — fields, annotations,
      envelope, `forEach` bounds, `when`. Unknown key → `unknownPath` with
      nearest match; type checked against the target node via the existing
      `isFieldTypeCompatible`. Later ref wins on key collision (research: merge
      order matters).
- [ ] Auto-inject the `function-environment-configs` step ahead of the templating
      step when `spec.environment` is non-empty, the way `function-auto-ready` is
      defaulted today; an explicit `spec.pipeline` still replaces in full. Pin
      the package in `functions.yaml`.
- [ ] Go-templating emitter: `{{ $env := index .context "…" | default dict }}`
      prelude, reads guarded with `hasKey` exactly like optional params.
- [ ] Python emitter: `MessageToDict(req.context)` — `req.context` is already on
      the request, so this is a prelude change.
- [ ] **Round-trip.** Emit the `keys:` declaration as an annotation on the
      Composition so `gen → apply → kubectl get -o yaml → import` recovers it.
      The refs round-trip natively as the step's Input; the declaration is
      cf-side and would otherwise be lost, which Engine Truth #5 forbids.
      Fallback for foreign compositions: reconstruct keys from `$env.<key>` reads
      in the template. *Assumption pending Kaur's call — the alternative is
      accepting the loss.*
- [ ] Extend the Lane C round-trip gate to an example that uses `env`, so the
      annotation path is proven by the diff rather than asserted.
- [ ] **KCL emitter — last.** Spike first: how `function-kcl` exposes `.context`
      is unverified. cf's emitter reads `option("params")?.oxr`; whether
      `params.ctx` carries the environment is not in the research. Find out
      against the real function before writing the emitter. Until it lands,
      `spec.environment` under KCL refuses with a "not yet" message matching the
      existing refusal style.

### 3. Expression authoring — preview and snippets

`raw:` is the escape hatch for everything the DSL does not model, and it is
unchecked until apply. `internal/blueprint/templates.go` already holds the parse
contract (text/template, `missingkey=error`, function-go-templating's function
set); what is missing is execution against a real context.

- [ ] `POST /api/preview-expression` executes one expression in-process against a
      synthetic context built from the blueprint's params, `$xr`, and observed
      fixtures, returning the rendered string or the template error. In-process
      means no Docker, unlike `/api/render`, so it can run while typing.
- [ ] A snippet catalogue in the inspector, filtered by what is actually in
      scope: this resource's schema node, the declared params, sibling resources
      exposing status leaves, whether the resource sits inside a `forEach`, and
      the env keys from item 2. Entries insert with the real names already filled
      in — range over an object param, index a sibling's status, `printf` with
      `$i`, `default`, read an env key.
- [ ] Live preview under the expression editor, updating as it is edited.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
