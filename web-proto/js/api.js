/**
 * api.js — fetch wrappers for every live endpoint.
 *
 * All paths are RELATIVE (/api/...): serve.py proxies them to the engine at
 * http://127.0.0.1:8080, so every call is same-origin.
 *
 * Every wrapper returns the parsed JSON body on success. On any non-2xx
 * response (or network failure) it THROWS a plain object:
 *
 *   { status: number, message: string }
 *
 * where `message` is the server's error text VERBATIM (the `error` field of
 * the JSON body when present, otherwise the raw body). 400 validation
 * messages are worth showing to the user verbatim. `status` is 0 for
 * network-level failures.
 *
 * FROZEN CONTRACT — region agents code against this file without editing it.
 */

/**
 * @typedef {Object} ApiError
 * @property {number} status  HTTP status (0 = network failure)
 * @property {string} message Server error text, verbatim
 */

/**
 * Core request helper.
 * @param {string} method
 * @param {string} path   Relative path starting with /api
 * @param {*} [body]      JSON-serializable request body
 * @returns {Promise<*>}  Parsed JSON body (null for empty responses)
 * @throws {ApiError}
 */
async function request(method, path, body, opts) {
  const options = opts || {};
  const fetchOpts = { method, headers: {} };
  const contentType = options.contentType || (body !== undefined ? "application/json" : null);
  if (contentType) {
    fetchOpts.headers["Content-Type"] = contentType;
  }
  if (body !== undefined) {
    fetchOpts.body = (contentType === "application/json" && typeof body !== "string")
      ? JSON.stringify(body)
      : body;
  }
  let res;
  try {
    res = await fetch(path, fetchOpts);
  } catch (e) {
    throw { status: 0, message: "network error: " + (e && e.message || e) };
  }
  const text = await res.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch (_) { /* non-JSON body */ }
  }
  if (!res.ok) {
    const message = (data && typeof data.error === "string" && data.error)
      || text || (res.status + " " + res.statusText);
    console.warn("[API ERROR]", res.status, path, message);
    throw { status: res.status, message };
  }
  if (options.responseType === "text") {
    return text;
  }
  return data;
}

/**
 * GET /api/kinds
 * @param {string} [q] Substring search over kind names (server param: q).
 * @returns {Promise<{kinds: Array<{kind:string, group:string, version:string,
 *   apiVersion:string, plural:string, scope:string, provider:string,
 *   namespaced:boolean, required:number, fields:number}>}>}
 * @throws {ApiError}
 */
export function getKinds(q) {
  return request("GET", "/api/kinds" + (q ? "?q=" + encodeURIComponent(q) : ""));
}

/**
 * GET /api/kinds/{apiVersion}/{kind} — identity, envelope, and status fields.
 * @param {string} apiVersion
 * @param {string} kind
 * @returns {Promise<{kind: Object, envelope: Array<Object>, status: Array<Object>}>}
 * @throws {ApiError}
 */
export function getKind(apiVersion, kind) {
  return request("GET",
    "/api/kinds/" + encodeURIComponent(apiVersion) + "/" + encodeURIComponent(kind));
}

/**
 * GET /api/kinds/{apiVersion}/{kind}/fields
 * @param {string} apiVersion e.g. "sqs.aws.m.upbound.io/v1beta1" (encoded here — pass it raw)
 * @param {string} kind       e.g. "Queue"
 * @param {Object} [opts]
 * @param {string}  [opts.prefix]       Only fields under this path prefix
 * @param {number}  [opts.maxDepth]     Limit tree depth (server param: max_depth; 0 = unlimited)
 * @param {string}  [opts.search]       Substring search (server param: q)
 * @param {boolean} [opts.requiredOnly] Only required fields (server param: required_only)
 * @param {number}  [opts.limit]        Cap the returned field count (total still
 *                                      counts the full filtered set)
 * @returns {Promise<{fields: Array<{path:string, type:string,
 *   description:string, required:boolean, depth:number}>, total: number}>}
 * @throws {ApiError}
 */
export function getKindFields(apiVersion, kind, opts) {
  const o = opts || {};
  const q = new URLSearchParams();
  if (o.prefix !== undefined && o.prefix !== "") q.set("prefix", o.prefix);
  if (o.maxDepth !== undefined && o.maxDepth !== null) q.set("max_depth", String(o.maxDepth));
  if (o.search !== undefined && o.search !== "") q.set("q", o.search);
  if (o.requiredOnly) q.set("required_only", "true");
  if (o.limit !== undefined && o.limit !== null) q.set("limit", String(o.limit));
  const qs = q.toString();
  return request("GET",
    "/api/kinds/" + encodeURIComponent(apiVersion) + "/" + encodeURIComponent(kind) + "/fields"
    + (qs ? "?" + qs : ""));
}

/**
 * GET /api/blueprint — the full blueprint document.
 * @returns {Promise<Object>} The full doc. Field forms in
 *   spec.resources[].fields are exactly-one-of {value|from|raw}; wires live
 *   in the doc as fields with {from: "params.X"}.
 * @throws {ApiError}
 */
export function getBlueprint() {
  return request("GET", "/api/blueprint");
}

/**
 * PUT /api/blueprint — full-document replace.
 * @param {Object} doc The complete blueprint document.
 * @returns {Promise<Object>} The full persisted blueprint.
 * @throws {ApiError} 400 carries the server's validation message verbatim
 *   (e.g. unknown field paths are rejected with a message worth showing).
 */
export function putBlueprint(doc) {
  return request("PUT", "/api/blueprint", doc);
}

/**
 * POST /api/blueprint/parameters — add a parameter.
 * @param {string} name Parameter name (camelCase).
 * @param {{type:string, required?:boolean, enum?:string[]|null,
 *   default?:string, description?:string}} parameter
 * @returns {Promise<Object>} The full persisted blueprint.
 * @throws {ApiError}
 */
export function addParameter(name, parameter) {
  return request("POST", "/api/blueprint/parameters", { name, parameter });
}

/**
 * PUT /api/blueprint/parameters/{name} — replace a parameter's definition.
 * @param {string} name
 * @param {{type:string, required?:boolean, enum?:string[]|null,
 *   default?:string, description?:string}} parameter
 * @returns {Promise<Object>} The full persisted blueprint.
 * @throws {ApiError}
 */
export function updateParameter(name, parameter) {
  return request("PUT", "/api/blueprint/parameters/" + encodeURIComponent(name), { parameter });
}

/**
 * DELETE /api/blueprint/parameters/{name}
 * @param {string} name
 * @returns {Promise<Object>} The full persisted blueprint.
 * @throws {ApiError} 404 if the parameter is not declared.
 */
export function deleteParameter(name) {
  return request("DELETE", "/api/blueprint/parameters/" + encodeURIComponent(name));
}

/**
 * POST /api/blueprint/parameters/{name}/rename
 * Renames the parameter and rewrites every wire that referenced it.
 * @param {string} name Current name.
 * @param {string} to   New name.
 * @returns {Promise<Object>} The full persisted blueprint.
 * @throws {ApiError}
 */
export function renameParameter(name, to) {
  return request("POST", "/api/blueprint/parameters/" + encodeURIComponent(name) + "/rename", { to });
}

/**
 * POST /api/generate
 * @param {boolean} [write=false] Whether the engine writes files to disk.
 * @returns {Promise<{outputs: Array<{path:string, bytes:number, body:string}>,
 *   written: boolean}>}
 * @throws {ApiError}
 */
export function generate(write) {
  return request("POST", "/api/generate", { write: !!write });
}

/**
 * List the server's cached providers.
 * @returns {Promise<{providers: Array<{ref:string,digest:string,kinds:number}>}>}
 */
export function getProviders() {
  return request("GET", "/api/providers");
}

/**
 * Add a provider by OCI ref; the server pulls, caches and reindexes.
 * @param {string} ref
 */
export function addProvider(ref) {
  return request("POST", "/api/providers", { ref: ref });
}

/**
 * Real render check: the server runs `crossplane composition render` on the
 * current blueprint against a synthesized sample XR.
 * @returns {Promise<{ok:boolean,resources:number,error:string,unavailable:string}>}
 */
export function renderCheck() {
  return request("POST", "/api/render");
}

/**
 * Remove a cached provider. The server refuses with 409 (naming referencers)
 * while the blueprint still uses it.
 * @param {string} ref
 */
export function removeProvider(ref) {
  return request("DELETE", "/api/providers/" + encodeURIComponent(ref));
}

/**
 * Search the static provider/function catalogue (CI-built index of OSS packages).
 * @param {string} q substring filter over name/description
 * @param {string} [type] optional filter: "function" | "provider"
 */
export function getCatalogue(q, type) {
  var params = [];
  if (q) params.push("q=" + encodeURIComponent(q));
  if (type) params.push("type=" + encodeURIComponent(type));
  var qs = params.length ? ("?" + params.join("&")) : "";
  return request("GET", "/api/catalogue" + qs);
}

/**
 * Rename a composed resource server-side: wires, status refs, when/forEach
 * referencers all re-point atomically; returns the full persisted blueprint.
 */
export function renameResource(name, to) {
  return request("POST", "/api/blueprint/resources/" + encodeURIComponent(name) + "/rename", { to: to });
}

/** GET /api/rbac — the RBAC rules the composed kinds need (broad-by-default). */
export function getRBAC() {
  return request("GET", "/api/rbac");
}

/** GET /api/version — the server build version for the wordmark. */
export function getVersion() {
  return request("GET", "/api/version");
}

/** GET /api/cluster — live cluster connection status. */
export function getCluster() {
  return request("GET", "/api/cluster");
}

/** POST /api/cluster/sync — discover and cache CRDs from the connected cluster. */
export function syncCluster() {
  return request("POST", "/api/cluster/sync");
}


/**
 * POST /api/blueprint/import — raw blueprint YAML through the file gate;
 * returns the persisted doc. 400s carry the parse/validation error verbatim.
 */
export function importBlueprint(yamlText) {
  return request("POST", "/api/blueprint/import", yamlText, { contentType: "application/yaml" });
}

/** GET /api/package?format=yaml — the package.yaml document stream as text. */
export function getPackageYAML() {
  return request("GET", "/api/package?format=yaml", undefined, { responseType: "text" });
}

/** POST /api/sources/crds — add a scanned CRD manifest as a schema source. */
export function addCRDSource(name, yamlText) {
  return request("POST", "/api/sources/crds", { name: name, yaml: yamlText });
}

/** GET /api/examples — list curated starter blueprints. */
export function getExamples() {
  return request("GET", "/api/examples");
}

/** GET /api/examples/{id} — get a specific starter blueprint by id. */
export function getExample(id) {
  return request("GET", "/api/examples/" + encodeURIComponent(id));
}

/** POST /api/examples/{id}/load — load starter blueprint and import/cache required providers. */
export function loadExample(id) {
  return request("POST", "/api/examples/" + encodeURIComponent(id) + "/load");
}

