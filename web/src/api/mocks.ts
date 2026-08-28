// MSW handlers over the frozen fixtures in src/api/fixtures/*.json. These
// fixtures are the shared contract with M2's Go HTTP tests — realistic
// shapes, not test scaffolding — so the filtering logic here mirrors the
// real server's documented semantics (internal/index.Fields' fixed filter
// order: Prefix, then MaxDepth, then RequiredOnly, then Search, then Limit)
// rather than faking just enough to pass one assertion.
import { http, HttpResponse } from "msw"
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

/** Exposed for tests that need a clean blueprint between cases; contract.test.ts
 * doesn't need it today because every mutation it exercises is rejected
 * (400/409) before anything changes, but a later task's success-path tests
 * will want it. */
export function resetBlueprintFixture(): void {
  blueprintState = structuredClone(blueprintFixture) as unknown as Blueprint
}

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/

function referencingResources(name: string): string[] {
  const refs: string[] = []
  for (const r of blueprintState.spec.resources) {
    for (const f of Object.values(r.fields)) {
      if (f.from === `params.${name}`) refs.push(r.name)
    }
  }
  return refs
}

// ---------------------------------------------------------------------------
// /fields query filtering
// ---------------------------------------------------------------------------

function filterFields(all: Field[], url: URL): Field[] | { error: string } {
  const requiredOnlyRaw = url.searchParams.get("required_only")
  const maxDepthRaw = url.searchParams.get("max_depth")
  const limitRaw = url.searchParams.get("limit")
  const prefix = url.searchParams.get("prefix") ?? ""
  const search = (url.searchParams.get("q") ?? "").toLowerCase()

  let requiredOnly = false
  if (requiredOnlyRaw !== null) {
    if (requiredOnlyRaw !== "true" && requiredOnlyRaw !== "false") {
      return { error: `required_only: invalid boolean "${requiredOnlyRaw}"` }
    }
    requiredOnly = requiredOnlyRaw === "true"
  }

  let maxDepth = 0
  if (maxDepthRaw !== null) {
    if (!/^\d+$/.test(maxDepthRaw)) {
      return { error: `max_depth: invalid integer "${maxDepthRaw}"` }
    }
    maxDepth = Number(maxDepthRaw)
  }

  let limit = 0
  if (limitRaw !== null) {
    if (!/^\d+$/.test(limitRaw)) {
      return { error: `limit: invalid integer "${limitRaw}"` }
    }
    limit = Number(limitRaw)
  }

  // Fixed order, matching internal/index.Fields: Prefix, MaxDepth,
  // RequiredOnly, Search, Limit.
  let out = all
  if (prefix) {
    out = out.filter(f => f.path === prefix || f.path.startsWith(`${prefix}.`))
  }
  if (maxDepth > 0) {
    out = out.filter(f => f.depth <= maxDepth)
  }
  if (requiredOnly) {
    out = out.filter(f => f.required)
  }
  if (search) {
    out = out.filter(
      f => f.path.toLowerCase().includes(search) || f.description.toLowerCase().includes(search),
    )
  }
  if (limit > 0) {
    out = out.slice(0, limit)
  }
  return out
}

export const handlers = [
  http.get("/api/kinds", ({ request }) => {
    const url = new URL(request.url)
    const q = (url.searchParams.get("q") ?? "").toLowerCase()
    const limitRaw = url.searchParams.get("limit")

    let kinds = KINDS
    if (q) {
      kinds = kinds.filter(k => k.kind.toLowerCase().includes(q) || k.group.toLowerCase().includes(q))
    }
    if (limitRaw !== null) {
      const limit = Number(limitRaw)
      if (Number.isFinite(limit) && limit > 0) {
        kinds = kinds.slice(0, limit)
      }
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
    if (!Array.isArray(result)) {
      return errorJSON(400, result.error)
    }
    return HttpResponse.json({ fields: result, total: result.length })
  }),

  http.get("/api/blueprint", () => HttpResponse.json(blueprintState)),

  http.post("/api/blueprint/parameters", async ({ request }) => {
    let body: { name?: string; parameter?: Parameter }
    try {
      body = (await request.json()) as typeof body
    } catch {
      return errorJSON(400, "malformed JSON body")
    }
    const name = body.name ?? ""
    const parameter = body.parameter
    if (!NAME_RE.test(name)) {
      return errorJSON(400, `invalid parameter name: "${name}"`)
    }
    if (!parameter || typeof parameter.type !== "string" || parameter.type === "") {
      return errorJSON(400, "parameter.type is required")
    }
    if (parameter.type === "array") {
      return errorJSON(400, `array parameters are unsupported: ${name}`)
    }
    if (blueprintState.spec.xrd.parameters[name]) {
      return errorJSON(400, `duplicate parameter: ${name}`)
    }
    blueprintState.spec.xrd.parameters[name] = parameter
    return HttpResponse.json({ parameter })
  }),

  http.put("/api/blueprint/parameters/:name", async ({ params, request }) => {
    const name = params.name as string
    let body: { parameter?: Parameter }
    try {
      body = (await request.json()) as typeof body
    } catch {
      return errorJSON(400, "malformed JSON body")
    }
    if (!body.parameter || typeof body.parameter.type !== "string" || body.parameter.type === "") {
      return errorJSON(400, "parameter.type is required")
    }
    if (!blueprintState.spec.xrd.parameters[name]) {
      return errorJSON(404, `unknown parameter: ${name}`)
    }
    blueprintState.spec.xrd.parameters[name] = body.parameter
    return HttpResponse.json({ parameter: body.parameter })
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
    if (!NAME_RE.test(to)) {
      return errorJSON(400, `invalid parameter name: "${to}"`)
    }
    const existing = blueprintState.spec.xrd.parameters[from]
    if (!existing) {
      return errorJSON(404, `unknown parameter: ${from}`)
    }
    if (blueprintState.spec.xrd.parameters[to]) {
      return errorJSON(400, `parameter already exists: ${to}`)
    }
    delete blueprintState.spec.xrd.parameters[from]
    blueprintState.spec.xrd.parameters[to] = existing
    for (const r of blueprintState.spec.resources) {
      for (const f of Object.values(r.fields)) {
        if (f.from === `params.${from}`) f.from = `params.${to}`
      }
    }
    return HttpResponse.json({})
  }),

  http.delete("/api/blueprint/parameters/:name", ({ params }) => {
    const name = params.name as string
    if (!blueprintState.spec.xrd.parameters[name]) {
      return errorJSON(404, `unknown parameter: ${name}`)
    }
    const refs = referencingResources(name)
    if (refs.length > 0) {
      return errorJSON(409, `parameter ${name} is still referenced by ${refs.join(", ")}`)
    }
    delete blueprintState.spec.xrd.parameters[name]
    return new HttpResponse(null, { status: 204 })
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
