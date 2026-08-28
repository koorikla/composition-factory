// MSW handlers over the frozen fixtures in src/api/fixtures/*.json. These
// fixtures are the shared contract with M2's Go HTTP tests — realistic
// shapes, not test scaffolding — so the filtering logic here mirrors the
// real server's documented semantics (internal/index.Fields' fixed filter
// order: Prefix, then MaxDepth, then RequiredOnly, then Search, then Limit)
// rather than faking just enough to pass one assertion.
import { http, HttpResponse } from "msw"
import type { HttpHandler } from "msw"
import type { Blueprint, Field, Kind, Parameter } from "./contract"

import kindsFixture from "./fixtures/kinds.json"
import queueFieldsFixture from "./fixtures/queue.fields.json"
import queueKindFixture from "./fixtures/queue.kind.json"
import blueprintFixture from "./fixtures/blueprint.json"
import generateFixture from "./fixtures/generate.json"

// queue.kind.json's "envelope" array is NOT a fixed list — it is
// Fields(crd.Envelope(), FieldQuery{}) for the v2 NAMESPACED Queue variant,
// where Envelope() (internal/schema/tree.go) computes
// spec.properties minus {forProvider, initProvider}. Namespaced (v2, ".m."
// API groups) and cluster-scoped (v1, legacy) managed resources have
// DIFFERENT envelopes: v2 requires providerConfigRef.kind (Provider vs.
// ClusterProvider config) where v1 does not, and v1's deletionPolicy /
// publishConnectionDetailsTo do not exist on v2 resources at all. Do not
// hand-edit this list from memory or from a v1 example — regenerate it from
// Envelope() for the specific CRD variant you are fixturing.
const KINDS: Kind[] = kindsFixture.kinds as Kind[]
const QUEUE_FIELDS: Field[] = queueFieldsFixture.fields as Field[]

function errorJSON(status: number, message: string) {
  return HttpResponse.json({ error: message }, { status })
}

// ---------------------------------------------------------------------------
// Blueprint state: an in-memory stand-in for "the file on disk is the source
// of truth". Cloned from the fixture so mutation handlers never leak state
// from one test file's run into another's module cache.
// ---------------------------------------------------------------------------

let blueprintState: Blueprint = structuredClone(blueprintFixture) as unknown as Blueprint

/** Exposed for tests that need a clean blueprint between cases —
 * contract.test.ts's success-path mutation tests (add/set/rename/delete)
 * persist into this module-level state and reset it in afterEach. */
export function resetBlueprintFixture(): void {
  blueprintState = structuredClone(blueprintFixture) as unknown as Blueprint
}

// ---------------------------------------------------------------------------
// Parameter validation — mirrors internal/blueprint/load.go's Validate()
// exactly for the parameter checks the parameter routes can trip. The server
// is the contract authority here: paramNameRE is `^[a-zA-Z][a-zA-Z0-9]*$`
// (camelCase — NO underscores, unlike a JS identifier), YAML keywords are
// rejected case-insensitively as names, the valid type set is exactly
// {string, integer, number, boolean, object}, and "array" gets its own
// refusal message. Message text is copied verbatim from load.go so the
// frontend surfaces what the real server would say.
// ---------------------------------------------------------------------------

const PARAM_NAME_RE = /^[a-zA-Z][a-zA-Z0-9]*$/
const YAML_KEYWORDS = new Set(["true", "false", "yes", "no", "on", "off", "null", "y", "n"])
const VALID_PARAM_TYPES = new Set(["string", "integer", "number", "boolean", "object"])

/** The first Validate() failure for one parameter declaration, in load.go's
 * own order (name, then type), phrased verbatim — or null when it passes the
 * checks this mock mirrors. */
function parameterValidationError(name: string, p: Parameter | undefined): string | null {
  if (!PARAM_NAME_RE.test(name) || YAML_KEYWORDS.has(name.toLowerCase())) {
    return (
      `spec.xrd.parameters.${name}: invalid parameter name ` +
      `(must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)`
    )
  }
  const type = p?.type ?? ""
  if (type === "array") {
    return (
      `spec.xrd.parameters.${name}: type "array" is not supported in M1. ` +
      `The XRD emitter cannot write the required items: schema for it, and a from: ` +
      `mapping would render Go's fmt of the slice ("[a b c]") -- valid YAML, silently ` +
      `wrong. Use a scalar parameter, or a raw: field for a literal list`
    )
  }
  if (!VALID_PARAM_TYPES.has(type)) {
    return `spec.xrd.parameters.${name}: unknown type ${JSON.stringify(type)}`
  }
  return null
}

/** Mirrors internal/api/blueprint.go's parameterKeys: every parameter key in
 * declaration order, paired with "does the current declaration hold a value
 * here" — the anti-silent-destruction rule's notion of a value worth
 * refusing to drop (server commit d975531). */
const PARAMETER_KEYS: Array<{ name: string; set: (p: Parameter) => boolean }> = [
  { name: "type", set: p => p.type !== "" },
  { name: "required", set: p => p.required === true },
  { name: "enum", set: p => (p.enum?.length ?? 0) > 0 },
  { name: "default", set: p => (p.default ?? "") !== "" },
  { name: "description", set: p => (p.description ?? "") !== "" },
]

/** Mirrors silentlyDropped: the keys absent from a PUT body that currently
 * hold a value on the existing declaration, in PARAMETER_KEYS order. */
function silentlyDropped(existing: Parameter, present: Set<string>): string[] {
  return PARAMETER_KEYS.filter(k => !present.has(k.name) && k.set(existing)).map(k => k.name)
}

function referencingResources(name: string): string[] {
  const refs: string[] = []
  for (const r of blueprintState.spec.resources) {
    for (const f of Object.values(r.fields)) {
      if (f.from === `params.${name}`) {
        refs.push(r.name)
        break
      }
    }
  }
  return refs
}

// ---------------------------------------------------------------------------
// /fields query filtering
// ---------------------------------------------------------------------------

// Mirrors internal/api/kinds.go's parseIntParam: strconv.Atoi accepts an
// optional sign, so a NEGATIVE (or zero) integer parses fine and simply
// means "unlimited" downstream — only a value that is not an integer at all
// is a 400, with the server's own `invalid <name>: "<raw>"` wording. The
// mock used to 400 negative values too; the server does not.
const GO_ATOI_RE = /^[+-]?\d+$/

function parseIntParam(raw: string | null, name: string): { value: number } | { error: string } {
  if (raw === null || raw === "") return { value: 0 }
  if (!GO_ATOI_RE.test(raw)) {
    return { error: `invalid ${name}: "${raw}"` }
  }
  return { value: Number(raw) }
}

// Mirrors parseBoolParam / strconv.ParseBool: exactly these spellings.
const GO_PARSEBOOL: Record<string, boolean> = {
  "1": true, t: true, T: true, TRUE: true, true: true, True: true,
  "0": false, f: false, F: false, FALSE: false, false: false, False: false,
}

function parseBoolParam(raw: string | null, name: string): { value: boolean } | { error: string } {
  if (raw === null || raw === "") return { value: false }
  if (!(raw in GO_PARSEBOOL)) {
    return { error: `invalid ${name}: "${raw}"` }
  }
  return { value: GO_PARSEBOOL[raw] }
}

function filterFields(all: Field[], url: URL): { fields: Field[]; total: number } | { error: string } {
  const prefix = url.searchParams.get("prefix") ?? ""
  const search = (url.searchParams.get("q") ?? "").toLowerCase()

  const requiredOnly = parseBoolParam(url.searchParams.get("required_only"), "required_only")
  if ("error" in requiredOnly) return requiredOnly
  const maxDepth = parseIntParam(url.searchParams.get("max_depth"), "max_depth")
  if ("error" in maxDepth) return maxDepth
  const limit = parseIntParam(url.searchParams.get("limit"), "limit")
  if ("error" in limit) return limit

  // Fixed order, matching internal/index.Fields: Prefix, MaxDepth,
  // RequiredOnly, Search, Limit.
  let out = all
  if (prefix) {
    out = out.filter(f => f.path === prefix || f.path.startsWith(`${prefix}.`))
  }
  if (maxDepth.value > 0) {
    out = out.filter(f => f.depth <= maxDepth.value)
  }
  if (requiredOnly.value) {
    out = out.filter(f => f.required)
  }
  if (search) {
    out = out.filter(
      f => f.path.toLowerCase().includes(search) || f.description.toLowerCase().includes(search),
    )
  }
  // `total` counts the filtered set BEFORE limit truncation — the server
  // (internal/api/kinds.go's handleKindFields) runs the query with Limit
  // zeroed and slices afterwards, precisely so a caller can tell the limit
  // cut the response short (total > len(fields)). total == len(fields) here
  // would make that signal tautologically useless.
  const total = out.length
  if (limit.value > 0 && out.length > limit.value) {
    out = out.slice(0, limit.value)
  }
  return { fields: out, total }
}

export const handlers = [
  http.get("/api/kinds", ({ request }) => {
    const url = new URL(request.url)
    const q = (url.searchParams.get("q") ?? "").toLowerCase()

    // The server 400s a limit that is not an integer (`invalid limit:
    // "abc"`, verbatim from parseIntParam) instead of silently ignoring it
    // and returning every kind; a negative or zero integer is accepted and
    // means unlimited (index.Search documents limit <= 0 exactly that way).
    const limit = parseIntParam(url.searchParams.get("limit"), "limit")
    if ("error" in limit) {
      return errorJSON(400, limit.error)
    }

    let kinds = KINDS
    if (q) {
      kinds = kinds.filter(k => k.kind.toLowerCase().includes(q) || k.group.toLowerCase().includes(q))
    }
    if (limit.value > 0) {
      kinds = kinds.slice(0, limit.value)
    }
    return HttpResponse.json({ kinds })
  }),

  http.get("/api/kinds/:apiVersion/:kind", ({ params }) => {
    const apiVersion = params.apiVersion as string
    const kind = params.kind as string
    const match = KINDS.find(k => k.apiVersion === apiVersion && k.kind === kind)
    if (!match) {
      return errorJSON(404, `kind not found: ${apiVersion}/${kind}`)
    }
    // queue.kind.json's envelope is the v2 NAMESPACED Queue's envelope only
    // (see the note above KINDS): v1 (cluster-scoped) and v2 envelopes
    // genuinely differ (providerConfigRef.kind is v2-only;
    // deletionPolicy/publishConnectionDetailsTo are v1-only). Reusing it here
    // for both variants is a known simplification — correct for `match`
    // when it's the namespaced Queue, a placeholder otherwise — until a
    // cluster-scoped envelope fixture exists. forProvider is identical
    // across both (scope is the only difference there), which is why
    // QUEUE_FIELDS is shared safely but this envelope is not.
    return HttpResponse.json({ kind: match, envelope: queueKindFixture.envelope })
  }),

  http.get("/api/kinds/:apiVersion/:kind/fields", ({ params, request }) => {
    const apiVersion = params.apiVersion as string
    const kind = params.kind as string
    const match = KINDS.find(k => k.apiVersion === apiVersion && k.kind === kind)
    if (!match) {
      return errorJSON(404, `kind not found: ${apiVersion}/${kind}`)
    }
    const result = filterFields(QUEUE_FIELDS, new URL(request.url))
    if ("error" in result) {
      return errorJSON(400, result.error)
    }
    return HttpResponse.json({ fields: result.fields, total: result.total })
  }),

  http.get("/api/blueprint", () => HttpResponse.json(blueprintState)),

  // Full-document replace: the client-authoritative canvas document
  // overwrites the mock's "disk" state outright, so a following GET or
  // /api/generate reflects it — the same contract the real PUT /api/blueprint
  // route serves (see api/contract.ts's putBlueprint doc comment). A doc
  // whose XRD scope is "Cluster" is rejected with a 400 to emulate a real
  // engine validation failure (Blueprint's XRD schema only supports the
  // scopes the real Go engine's blueprint.Validate() accepts today —
  // "Cluster" is a stand-in for "the engine rejected this document," not a
  // claim that scope itself is invalid).
  http.put("/api/blueprint", async ({ request }) => {
    let body: Blueprint
    try {
      body = (await request.json()) as Blueprint
    } catch {
      return errorJSON(400, "malformed JSON body")
    }
    if (body?.spec?.xrd?.scope === "Cluster") {
      return errorJSON(400, `invalid blueprint: xrd scope "Cluster" is unsupported`)
    }
    blueprintState = body
    return HttpResponse.json(blueprintState)
  }),

  // Every parameter mutation below mirrors internal/api/blueprint.go's
  // handlers: check order, status classification, error text (verbatim from
  // internal/blueprint/edit.go and load.go), and — on success — a 200
  // carrying the FULL persisted blueprint, the same shape GET returns
  // (never a bare {parameter}, {} or an empty 204; the server responds
  // writeJSON(w, http.StatusOK, b) on every one of these routes).

  http.post("/api/blueprint/parameters", async ({ request }) => {
    let body: { name?: string; parameter?: Parameter }
    try {
      body = (await request.json()) as typeof body
    } catch {
      return errorJSON(400, "malformed JSON body")
    }
    const name = body.name ?? ""
    const parameter: Parameter = body.parameter ?? { type: "" }

    // AddParameter's own FIRST action is the duplicate check, before any
    // validation — and a duplicate is a conflict with current state: 409,
    // not 400 (the HTTP layer classifies on "existed going in").
    if (blueprintState.spec.xrd.parameters[name]) {
      return errorJSON(409, `add parameter: ${JSON.stringify(name)} is already declared`)
    }
    const invalid = parameterValidationError(name, parameter)
    if (invalid !== null) {
      return errorJSON(400, `add parameter ${JSON.stringify(name)}: ${invalid}`)
    }
    blueprintState.spec.xrd.parameters[name] = parameter
    return HttpResponse.json(blueprintState)
  }),

  http.put("/api/blueprint/parameters/:name", async ({ params, request }) => {
    const name = params.name as string
    let body: { parameter?: Record<string, unknown> }
    try {
      body = (await request.json()) as typeof body
    } catch {
      return errorJSON(400, "malformed JSON body")
    }
    const present = new Set(Object.keys(body.parameter ?? {}))
    const parameter = (body.parameter ?? { type: "" }) as unknown as Parameter

    // The anti-silent-destruction rule (server commit d975531): PUT is a
    // whole-parameter replace, so a body that OMITS a key which currently
    // holds a value is refused — clearing a value must be said out loud
    // ("required": false, "enum": null), never implied by omission. Runs
    // before the existence 404 the way the server's handler does (only
    // meaningful when the parameter exists).
    const existing = blueprintState.spec.xrd.parameters[name]
    if (existing) {
      const dropped = silentlyDropped(existing, present)
      if (dropped.length > 0) {
        return errorJSON(
          400,
          `refusing a partial update of parameter ${JSON.stringify(name)}: PUT replaces the whole parameter, so omitting ` +
            `${dropped.join(", ")} would silently discard the value each of them currently holds. Send those keys ` +
            `explicitly — their zero values (false, null, "") are how you clear one.`,
        )
      }
    } else {
      return errorJSON(404, `set parameter: ${JSON.stringify(name)} is not declared`)
    }
    const invalid = parameterValidationError(name, parameter)
    if (invalid !== null) {
      return errorJSON(400, `set parameter ${JSON.stringify(name)}: ${invalid}`)
    }
    blueprintState.spec.xrd.parameters[name] = parameter
    return HttpResponse.json(blueprintState)
  }),

  http.post("/api/blueprint/parameters/:name/rename", async ({ params, request }) => {
    const from = params.name as string
    let body: { to?: string }
    try {
      body = (await request.json()) as typeof body
    } catch {
      return errorJSON(400, "malformed JSON body")
    }
    const to = body.to ?? ""

    // RenameParameter's fixed check order: from declared, then to == from
    // (a no-op SUCCESS — a blur-submit UI resubmits an unchanged name),
    // then to collides (409, a conflict with current state), then the
    // validation of the new name (400).
    const existing = blueprintState.spec.xrd.parameters[from]
    if (!existing) {
      return errorJSON(404, `rename parameter: ${JSON.stringify(from)} is not declared`)
    }
    if (to === from) {
      return HttpResponse.json(blueprintState)
    }
    if (blueprintState.spec.xrd.parameters[to]) {
      return errorJSON(409, `rename parameter: ${JSON.stringify(to)} is already declared`)
    }
    const invalid = parameterValidationError(to, existing)
    if (invalid !== null) {
      return errorJSON(400, `rename parameter ${JSON.stringify(from)} to ${JSON.stringify(to)}: ${invalid}`)
    }
    delete blueprintState.spec.xrd.parameters[from]
    blueprintState.spec.xrd.parameters[to] = existing
    for (const r of blueprintState.spec.resources) {
      for (const f of Object.values(r.fields)) {
        if (f.from === `params.${from}`) f.from = `params.${to}`
      }
    }
    return HttpResponse.json(blueprintState)
  }),

  http.delete("/api/blueprint/parameters/:name", ({ params }) => {
    const name = params.name as string
    if (!blueprintState.spec.xrd.parameters[name]) {
      return errorJSON(404, `delete parameter: ${JSON.stringify(name)} is not declared`)
    }
    const refs = referencingResources(name)
    if (refs.length > 0) {
      const quoted = refs.map(r => JSON.stringify(r)).join(", ")
      return errorJSON(409, `delete parameter ${JSON.stringify(name)}: still referenced by resources ${quoted}`)
    }
    // Existed, unreferenced — but the delete can STILL fail validation:
    // blueprint.Validate() requires providerName on a Namespaced XRD (the
    // Composition dereferences {{ $spec.providerName }} unguarded for every
    // composed resource), so deleting it is a 400, never a 204. Verbatim
    // from DeleteParameter's wrapped Validate() error.
    if (name === "providerName" && blueprintState.spec.xrd.scope === "Namespaced") {
      return errorJSON(
        400,
        `delete parameter "providerName": spec.xrd.parameters.providerName is required for a Namespaced XRD: ` +
          `the Composition emits providerConfigRef.name as {{ $spec.providerName }} for every ` +
          `composed resource, so a blueprint without this parameter generates a Composition ` +
          `that can never render. Add: providerName: {type: string, required: true}`,
      )
    }
    delete blueprintState.spec.xrd.parameters[name]
    // 200 with the full persisted blueprint — the server never answers this
    // route with an empty 204.
    return HttpResponse.json(blueprintState)
  }),

  http.post("/api/generate", async ({ request }) => {
    let body: { write?: boolean } = {}
    try {
      body = (await request.json()) as typeof body
    } catch {
      // an absent/empty body defaults to write:false
    }
    return HttpResponse.json({ ...generateFixture, written: Boolean(body.write) })
  }),
]

/** A one-off MSW handler override that makes /api/generate fail with a 400,
 * for tests that need to exercise "a generation failure surfaces instead of
 * stale output" (Output.test.tsx) without hand-rolling the same
 * http.post("/api/generate", ...) override in every file that needs it.
 * Install with `server.use(failGenerate())`; MSW's own resetHandlers()
 * (afterEach in every test file that calls it) undoes it automatically. */
export function failGenerate(message = "generation failed: engine returned a non-zero exit"): HttpHandler {
  return http.post("/api/generate", () => errorJSON(400, message))
}
