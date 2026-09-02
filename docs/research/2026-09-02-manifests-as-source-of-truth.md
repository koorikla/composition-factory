# Manifests as source of truth — analysis memo

**Status:** analysis for decision · **Date:** 2026-09-02 · **Asked by:** Kaur ("remove the DSL,
use plain Crossplane manifests with a specified structure; fields the engine does not know it
does not touch; go-templating only for now")

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

## 2. What the corpus says about the understanding ceiling

From `raw/cs-gotemplating-corpus.md` (381 real Compositions, 347 inline, 21k template actions):

| Share of corpus | Construct | Consequence for an in-place model |
|---|---|---|
| 28% | no control flow at all | fully understood: every field a card row |
| 60% | no loop and no `define` | understood except whole-doc `if` blocks |
| 57% / 40% / 10% | `if` / `range` / `with` | recognised only in cf-shaped forms; otherwise an opaque block |
| 82% | multi-document (`---`) templates | fine: the model is a list of documents |
| 70% vs 22% | raw resource-name annotation string vs `setResourceNameAnnotation` | both recognisable line patterns |
| 32% | reads observed resources | status wires, recognisable when the expression is a plain path |
| 13% / 12% | `toYaml` / `nindent` | value-position pipes: field stays editable as raw, not as a typed value |
| 9% | `printf`-built resource names | name is a raw expression, not editable as text |
| 5% | true escapes (`define`/`set`/`mergeOverwrite`/`regexSplit`) | opaque |
| 73% of 10k value expressions | bare `$spec.x` path | typed, wireable |

The share a placeholder-token parser would accept **was never measured** — the research stopped
at "do not build Tier 2". `cf adopt` already contains the technique (`internal/adopt/adopt.go:321`:
strip `define`s, mask every `{{ … }}` with `"__CF_EXPR_n__"`, YAML-parse, unmask) and it fails
exactly where the research predicted: key-position actions, `setResourceNameAnnotation` (a whole
line, not a value), `{{- if }}` wrapping keys, `toYaml | nindent` injecting blocks. Each of those
is a *block*, not a value, and blocks can be preserved as opaque spans instead of parsed.

Honest expectation: for **cf-emitted** templates, 100% understanding is achievable because cf
controls the dialect. For **foreign** templates, something between the 28% (fully) and 60%
(mostly) rows, with the rest shown as locked "raw" regions on a card or a locked card. That is
strictly more than today, where a foreign template is `adopt`ed lossily or not opened at all.

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

Feasible, and it is the better product if the numbers hold, because it removes the one thing
users will always fight (a private DSL between them and their manifests) and turns `adopt` from a
lossy import into "open". It is not feasible to decide from the couch: the research never
measured what a placeholder+span parser accepts. **Run the spike, measure, then decide.** Do not
start the rewrite on a hunch; the last three weeks of consolidation work show what a half-done
pivot costs.

Go/no-go gate for the spike: (a) 100% of cf's own goldens round-trip byte-exact with no edits and
survive one wire edit + one literal edit with only the intended bytes changed; (b) on the corpus
sample, ≥60% of documents parse with every `forProvider` leaf addressable and ≥90% of documents
open with opaque spans and zero bytes lost; (c) a `kubectl get -o yaml` export opens, scrubs,
edits one field, and `crossplane composition render` of the result equals render of the original
except for that field.
