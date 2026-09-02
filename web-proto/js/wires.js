/**
 * wires.js — pure helpers over the blueprint document. No DOM, no state.
 *
 * Engine truth: wires live IN the doc.
 * 1. An XRD parameter wire: { from: "params.X" }
 * 2. A cross-resource status wire: { from: "resources.Y.status.Z" }
 * Field forms are exactly-one-of {value|from|raw}.
 */

/**
 * Parse a from: expression into its wire descriptor.
 * @param {string} from
 * @returns {{kind: "param", param: string}|{kind: "status", resource: string, statusPath: string}|null}
 */
export function parseFrom(from) {
  if (typeof from !== "string") return null;
  if (from.indexOf("params.") === 0) {
    return { kind: "param", param: from.slice("params.".length) };
  }
  if (from.indexOf("resources.") === 0) {
    const rest = from.slice("resources.".length);
    const idx = rest.indexOf(".status.");
    if (idx !== -1) {
      return {
        kind: "status",
        resource: rest.slice(0, idx),
        statusPath: rest.slice(idx + ".status.".length)
      };
    }
  }
  return null;
}

const docWiresCache = new WeakMap();
const docFanOutCache = new WeakMap();

/**
 * List every wire in the document.
 * @param {Object} doc The full blueprint document.
 * @returns {Array<{kind:string, param?:string, srcResource?:string, srcPath?:string, resource:string, path:string, from:string}>}
 */
export function listWires(doc) {
  if (!doc || typeof doc !== "object") return [];
  if (docWiresCache.has(doc)) {
    return docWiresCache.get(doc);
  }
  const out = [];
  const resources = doc && doc.spec && doc.spec.resources || [];
  resources.forEach(function (r) {
    const checkDict = function (dict, isEnv) {
      if (!dict) return;
      Object.keys(dict).sort().forEach(function (path) {
        const f = dict[path];
        if (!f || typeof f.from !== "string") return;
        const parsed = parseFrom(f.from);
        if (!parsed) return;
        if (parsed.kind === "param") {
          out.push({
            kind: "param",
            param: parsed.param,
            resource: r.name,
            path: isEnv ? ("envelope." + path) : path,
            from: f.from,
            isEnvelope: !!isEnv
          });
        } else if (parsed.kind === "status") {
          out.push({
            kind: "status",
            srcResource: parsed.resource,
            srcPath: parsed.statusPath,
            resource: r.name,
            path: isEnv ? ("envelope." + path) : path,
            from: f.from,
            isEnvelope: !!isEnv
          });
        }
      });
    };
    checkDict(r.fields, false);
    checkDict(r.envelope, true);
    // annotations carry the same {from} wires; their "path" is the
    // annotation key namespaced so port lookups can't collide with fields
    if (r.annotations) {
      Object.keys(r.annotations).sort().forEach(function (key) {
        const f = r.annotations[key];
        if (!f || typeof f.from !== "string") return;
        const parsed = parseFrom(f.from);
        if (!parsed) return;
        if (parsed.kind === "param") {
          out.push({ kind: "param", param: parsed.param, resource: r.name,
            path: "annotations." + key, from: f.from, isAnnotation: true });
        } else if (parsed.kind === "status") {
          out.push({ kind: "status", srcResource: parsed.resource, srcPath: parsed.statusPath,
            resource: r.name, path: "annotations." + key, from: f.from, isAnnotation: true });
        }
      });
    }
  });
  docWiresCache.set(doc, out);
  return out;
}

/**
 * Compute the fan-out count map for every parameter in the document in a single pass.
 * @param {Object} doc The full blueprint document.
 * @returns {Record<string, number>} Map from param name to count.
 */
export function fanOutMap(doc) {
  if (!doc || typeof doc !== "object") return {};
  if (docFanOutCache.has(doc)) {
    return docFanOutCache.get(doc);
  }
  const map = {};
  const wires = listWires(doc);
  for (let i = 0; i < wires.length; i++) {
    const w = wires[i];
    if (w.kind === "param" && w.param) {
      map[w.param] = (map[w.param] || 0) + 1;
    }
  }
  docFanOutCache.set(doc, map);
  return map;
}

/**
 * Fan-out of one parameter: how many fields it is wired into.
 * @param {Object} doc The full blueprint document.
 * @param {string} param Parameter name (without the "params." prefix).
 * @returns {number}
 */
export function fanOut(doc, param) {
  if (!doc) return 0;
  return fanOutMap(doc)[param] || 0;
}
