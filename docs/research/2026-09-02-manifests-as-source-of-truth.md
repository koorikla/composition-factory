# Manifests as source of truth — analysis memo

**Status:** analysis for decision · **Date:** 2026-09-02 · **Asked by:** Kaur ("remove the DSL,
use plain Crossplane manifests with a specified structure; fields the engine does not know it
does not touch; go-templating only for now; the parser only has to read the go-templating
format cf itself emits")

## 1. What is being proposed, precisely

Today the on-disk source of truth is a `Blueprint` (`factory.crossplane.io/v1alpha1`); `cf gen`
compiles it into an XRD + a Composition whose go-templating body is generated text. The proposal
inverts that: the XRD and the Composition **are** the source of truth. The canvas, CLI and MCP
edit them in place. Two properties are required:

1. **Backwards compatibility with real manifests**, including `kubectl get -o yaml` exports of
   compositions cf never produced.
2. **Preservation**: an edit to one field (wire a parameter, set a literal) must not rewrite,
   reorder or reformat anything else in the file. Unknown constructs survive byte-for-byte.

This is a different problem from the one the spec ruled out. The spec's non-goal (§2) and the
blueprint-vs-Configuration memo's option (b) both reject **decompiling a template into a
Blueprint** — full understanding, lossless in both directions. The proposal only needs
**partial understanding with total preservation**: parse what you can, put a card on it; keep
everything else as opaque bytes and never re-serialise it. That is how editors treat source code
(CST + splice edits), not how compilers treat it. The infeasibility argument ("TEXT nodes are not
YAML, shape is data-dependent, indentation is semantic") still bounds *understanding*; it does not
bound *preservation*.

## 2. Scope: cf's own dialect, not arbitrary templates

Kaur's clarification narrows the problem decisively: the reader only has to parse **the
go-templating format cf emits**. Foreign hand-written templates are out of scope — they open
as a single opaque card or not at all, exactly as today. The corpus numbers about `range`/`if`/
`toYaml` in the wild therefore do not bound this design; what bounds it is whether cf's
emitted form is regular enough to be read back, and it is, by construction:

- a fixed prelude (`$spec`, `$xr`, `$xrMeta`), `define` blocks first, one `---` document per
  resource, `setResourceNameAnnotation "<name>"` naming each document;
- fields in three shapes only: literal (`key: 'v'`), wire (`key: {{ $spec.x }}`), and guarded
  optional (`{{- if hasKey $spec "x" }} … {{- end }}`), plus the status-wire guard chain,
  `range` for forEach and `if` for when, each emitted from one canonical writer;
- a provenance header and `options: ["missingkey=error"]`, so a file can be recognised as
  cf-shaped before parsing.

"Fields the engine does not know it does not touch" then means two concrete things: (1) YAML
outside the template body — labels, extra annotations, extra pipeline steps, `writeConnection…`,
anything a user or a controller added — is preserved through `yaml.v3` Node edits; (2) inside
the template body, any span the reader does not recognise as one of cf's forms (a hand-added
`range`, a `toYaml` pipe, a comment) is kept as an opaque span with its bytes untouched and
shown locked. Understanding of cf-shaped content is 100% by construction; preservation of
everything else is total by mechanism.

The one design obligation this creates: **the writer and the reader must share one definition
of the forms.** If the emitter is a set of `fmt.Sprintf` calls and the parser a set of regexes
written separately, they drift on the first change. Define each form once (a table of
bidirectional patterns: emit + match) and generate both sides from it, with a golden per form.

`cf adopt` already contains the masking technique (`internal/adopt/adopt.go:321`: strip
`define`s, mask `{{ … }}` as `"__CF_EXPR_n__"`, YAML-parse, unmask); it fails on block-level
actions because it treats them as values. Reading cf's own forms as *blocks with byte ranges*
is the missing half.

## 3. Mechanism that satisfies "does not touch what it does not know"

- **Parse to positions, never re-serialise foreign text.** `text/template/parse` with
  `SkipFuncCheck` gives every action its byte offset; masking scalar-position actions and parsing
  the masked text with `yaml.v3` gives every mapping key its line/column. Join the two into a
  model whose leaves carry `[start, end)` byte ranges into the original template string.
- **Edits are splices.** Set a literal, wire a parameter, add a guarded optional field, add a
  resource document, delete one: each is "replace bytes a..b with canonical text" where the
  canonical text comes from today's emitter, so cf-owned snippets stay byte-identical across
  saves. Saving without an edit must reproduce the input bytes exactly (golden: `patch(parse(x),
  nothing) == x`).
- **Opaque spans.** A top-level `if`/`range`/`with` block, a `define` body, a `toYaml` pipe, a
  key-position action, a `printf` name: kept as a span with a label; rendered as a locked region
  the inspector can show as text and the user can edit as text. Never rewritten by structured
  edits.
- **XRD and Composition metadata are plain YAML.** Edit via `yaml.v3` Node so comments and key
  order survive; accept that the first save of a `kubectl` export canonicalises indentation and
  quoting once (say so in the UI). Strip the known server-side fields deliberately on open:
  `metadata.{managedFields,resourceVersion,uid,creationTimestamp,generation,selfLink}`, `status`,
  `kubectl.kubernetes.io/last-applied-configuration`, `argocd.argoproj.io/tracking-id`.
- **Wires are recognised expressions.** `$spec.x`, `.observed.composite.resource.spec.x`,
  cf's guard chains, `(index $.observed.resources "r").resource.status.atProvider.p`,
  `getComposedResource`: normalised by a small expression grammar into param/status wires. Any
  other expression is a raw wire (shown, editable as text, not typed).
- **Schema validation stays the differentiator.** Once a document is parsed with placeholders,
  its field paths validate against the real CRD exactly as today, including the effective
  required view, nearest-match suggestions and the drag-to-wire picker.

## 4. The "specified structure"

Do not invent one. A Crossplane **Configuration source tree** already is the structure:

```
crossplane.yaml            # meta.pkg.crossplane.io/v1 Configuration: name + dependsOn providers/functions  → this IS `sources:`
apis/<xr>/definition.yaml  # XRD  → this IS `xrd:` + `parameters:`
apis/<xr>/composition.yaml # Composition, go-templating Inline or FileSystem → this IS `resources:`, wires, when, forEach, pipeline, envelope, annotations, templates(define)
.cf/layout.yaml            # card positions, sizes, hidden kinds (or an annotation on the Composition — decide)
.cf.lock                   # digests, unchanged
```

`cf package` and `crossplane xpkg build` consume exactly this tree today. Every DSL concept has a
native home: parameters → XRD schema (richer than the DSL: patterns, descriptions, nesting),
templates/conventions → `define` blocks, forEach/when → `range`/`if` in cf's canonical shape,
pipeline → `spec.pipeline`, envelope/annotations → literal YAML. Two DSL conveniences have no
native home and become **lint rules** instead of data: "optional parameter dereferenced without a
guard" and "XRD default and template `| default` disagree".

## 5. What is lost, and what it costs

- **Single-sourcing is gone.** Required-ness lives in the XRD; the guard lives in the template.
  cf derives the guard at edit time and lints drift. A hand edit to the XRD alone can leave a
  template unguarded; `cf validate` must catch it.
- **Readability of cf's own output becomes a product feature.** The status-wire guard chain cf
  emits today (`(and (hasKey $.observed "resources") (kindIs "map" …) …)`, ten clauses) is fine
  as compiler output and unacceptable as something a human edits in place. The pivot forces a
  simpler, still `missingkey=error`-safe form (`define "cf.observed"` helper or `dig`), proven by
  render.
- **KCL and Python engines have no parse story.** They become export-only (generate from the
  model, never read back) or are dropped. Kaur: go-templating only for now.
- **The blueprint file, `cf gen`, `adopt`, `import`, the embedded-blueprint annotation in
  `package.yaml`, and the examples format all go.** `cf gen` survives one release as the
  migration tool (blueprint → manifests).
- **Blast radius** (measured): `internal/blueprint` 1.9k lines replaced; 30 non-test Go files
  import it (api 10, emit 13, cf 3, adopt, cache, examples, xpkg); 10 HTTP routes and all 13 MCP
  tools speak blueprint JSON; the canvas reads `spec.resources`/`spec.xrd` in ~65 places across
  four regions; 33 of 62 Playwright specs assert on the blueprint shape; every acceptance golden
  changes role from "expected output" to "round-trip fixture". This is a v2, not a refactor.

## 6. Verdict

Feasible, and the better product, because it removes the one thing
users will always fight (a private DSL between them and their manifests) and turns `adopt` from a
lossy import into "open". The remaining risk is engineering, not research: a reader that mirrors the writer, and
a rewrite of everything that speaks blueprint JSON. **Run the spike against cf's own goldens,
then decide.** Do not
start the rewrite on a hunch; the last three weeks of consolidation work show what a half-done
pivot costs.

Go/no-go gate for the spike: (a) 100% of cf's own goldens round-trip byte-exact with no edits,
and survive one wire edit + one literal edit + one added resource with only the intended bytes
changed; (b) a cf-emitted composition exported with `kubectl get -o yaml` opens, scrubs its
server-side fields, takes one field edit, and `crossplane composition render` of the result
equals the original's render except for that field; (c) a hand-added label on the Composition,
an extra pipeline step, and a hand-added `{{ range }}` block inside the template all survive
three consecutive open-edit-save cycles byte-for-byte.
