// The frozen API contract. This file is the ONLY place that builds a URL or
// calls fetch — every other module in this app goes through `api`. The wire
// types below mirror internal/index.Kind and internal/index.Field, and
// internal/blueprint.{Blueprint,Parameter} exactly (see the M2 plan, Tasks
// 1-2 and internal/blueprint/types.go). If the Go side and this file ever
// disagree, the fixtures the two share (src/api/fixtures/*.json, also read
// by M2's Go HTTP tests) are the shared contract that should break the build.

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

/** Mirrors internal/index.Kind. */
export interface Kind {
  kind: string
  group: string
  version: string
  /** group/version */
  apiVersion: string
  plural: string
  /** "Namespaced" | "Cluster" */
  scope: string
  /** the xpkg ref it came from */
  provider: string
  namespaced: boolean
  /** count of required forProvider leaves */
  required: number
  /** count of forProvider leaves */
  fields: number
}

/** Mirrors internal/index.Field. */
export interface Field {
  /** dotted, arrays of objects indexed: containers[0].image */
  path: string
  /** string number integer boolean object array map */
  type: string
  description: string
  required: boolean
  /** 0 for a top-level field */
  depth: number
}

/** Mirrors internal/index.FieldQuery, in camelCase for the JS side. */
export interface FieldQuery {
  requiredOnly?: boolean
  /** 0 means unlimited */
  maxDepth?: number
  /** "" for the whole tree; e.g. "template.spec" to expand one subtree */
  prefix?: string
  search?: string
  limit?: number
}

/** Mirrors internal/blueprint.Parameter. */
export interface Parameter {
  type: string
  required?: boolean
  enum?: string[]
  default?: string
  description?: string
}

/** Mirrors internal/blueprint.Field (a resource's field assignment). Exactly
 * one of from, value, raw or template is set. */
export interface FieldAssignment {
  from?: string
  value?: string
  raw?: string
  /** Names a spec.templates entry whose output becomes the value. */
  template?: string
}

/** Mirrors internal/blueprint.Resource. */
export interface Resource {
  name: string
  kind: string
  provider?: string
  /** "params.<name>" — repeats the resource N times over an integer parameter. */
  forEach?: string
  fields: Record<string, FieldAssignment>
  /** metadata.annotations authoring: free-form annotation keys (dots and
   * slashes legal, e.g. "eks.amazonaws.com/role-arn") to the same
   * exactly-one-of assignment forms as fields. Values are always strings on
   * the composed object; a wired entry whose source is absent omits the key
   * cleanly. Omitted entirely (never null/{}) when a resource has none —
   * the Go side marshals it omitempty. */
  annotations?: Record<string, FieldAssignment>
}

/** Mirrors internal/blueprint.Source. */
export interface Source {
  provider: string
}

/** Mirrors internal/blueprint.XRD. */
export interface XRD {
  group: string
  kind: string
  plural: string
  version: string
  scope: string
  parameters: Record<string, Parameter>
}

/** Mirrors internal/blueprint.Metadata. */
export interface Metadata {
  name: string
}

/** Mirrors internal/blueprint.Spec. */
export interface Spec {
  sources: Source[]
  xrd: XRD
  resources: Resource[]
}

/** Mirrors internal/blueprint.Blueprint, the root document. */
export interface Blueprint {
  apiVersion: string
  kind: string
  metadata: Metadata
  spec: Spec
}

export interface GenerateOutput {
  path: string
  bytes: number
  /** The full generated YAML file content, byte-for-byte what the engine
   * writes. Always present (not optional — the server always sends it). */
  body: string
}

/** Mirrors the /api/generate response body. */
export interface GenerateResult {
  outputs: GenerateOutput[]
  written: boolean
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/** Thrown for every non-2xx response. Carries the HTTP status and the
 * server's own "error" message so callers can distinguish, say, a 409
 * (conflict with current state) from a 400 (malformed request). */
export class ApiError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

const BASE = "/api"

function query(params: Record<string, string | number | boolean | undefined>): string {
  const sp = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) continue
    sp.set(key, String(value))
  }
  const s = sp.toString()
  return s ? `?${s}` : ""
}

async function errorMessage(res: Response): Promise<string> {
  try {
    const body = await res.json()
    if (body && typeof body === "object" && typeof (body as { error?: unknown }).error === "string") {
      const msg = (body as { error: string }).error
      if (msg.length > 0) return msg
    }
  } catch {
    // body wasn't JSON (or was empty) — fall through to the status text
  }
  return res.statusText || `request failed with status ${res.status}`
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init)
  if (!res.ok) {
    throw new ApiError(await errorMessage(res), res.status)
  }
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

function jsonInit(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }
}

// ---------------------------------------------------------------------------
// The client
// ---------------------------------------------------------------------------

export const api = {
  kinds(q?: string, limit?: number): Promise<Kind[]> {
    return request<{ kinds: Kind[] }>(`/kinds${query({ q, limit })}`).then(r => r.kinds)
  },

  kind(apiVersion: string, kind: string): Promise<{ kind: Kind; envelope: Field[] }> {
    return request(`/kinds/${encodeURIComponent(apiVersion)}/${encodeURIComponent(kind)}`)
  },

  fields(apiVersion: string, kind: string, q: FieldQuery = {}): Promise<{ fields: Field[]; total: number }> {
    const qs = query({
      required_only: q.requiredOnly,
      max_depth: q.maxDepth,
      prefix: q.prefix,
      q: q.search,
      limit: q.limit,
    })
    return request(`/kinds/${encodeURIComponent(apiVersion)}/${encodeURIComponent(kind)}/fields${qs}`)
  },

  blueprint(): Promise<Blueprint> {
    return request("/blueprint")
  },

  /** Full-document replace. The canvas document lives client-side (in the
   * store), but /api/generate reads the document from DISK — without this,
   * the preview would ignore everything the user has done on the canvas.
   * Returns the persisted doc (same shape GET returns); a 400 carries the
   * engine's own validation error verbatim, exactly like every other
   * mutation route in this file. */
  putBlueprint(doc: Blueprint): Promise<Blueprint> {
    return request("/blueprint", jsonInit("PUT", doc))
  },

  /** Every parameter mutation resolves with the FULL persisted blueprint —
   * the same shape GET /api/blueprint returns. That is the server's actual
   * success contract for all four routes (internal/api/blueprint.go answers
   * each with writeJSON(w, http.StatusOK, b)); DELETE in particular is a
   * 200 with the document, never an empty 204. */
  addParameter(name: string, p: Parameter): Promise<Blueprint> {
    return request("/blueprint/parameters", jsonInit("POST", { name, parameter: p }))
  },

  renameParameter(from: string, to: string): Promise<Blueprint> {
    return request(`/blueprint/parameters/${encodeURIComponent(from)}/rename`, jsonInit("POST", { to }))
  },

  setParameter(name: string, p: Parameter): Promise<Blueprint> {
    return request(`/blueprint/parameters/${encodeURIComponent(name)}`, jsonInit("PUT", { parameter: p }))
  },

  deleteParameter(name: string): Promise<Blueprint> {
    return request(`/blueprint/parameters/${encodeURIComponent(name)}`, { method: "DELETE" })
  },

  generate(write: boolean): Promise<GenerateResult> {
    return request("/generate", jsonInit("POST", { write }))
  },
}
