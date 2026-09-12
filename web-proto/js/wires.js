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
 * @returns {{kind: "param", param: string}|{kind: "env", key: string}|{kind: "status", resource: string, statusPath: string}|null}
 */
export function parseFrom(from) {
  if (typeof from !== "string") return null;
  if (from.indexOf("params.") === 0) {
    return { kind: "param", param: from.slice("params.".length) };
  }
  if (from.indexOf("env.") === 0) {
    return { kind: "env", key: from.slice("env.".length) };
  }
  if (from.indexOf("resources.") === 0) {
    const rest = from.slice("resources.".length);
    if (rest.endsWith(".metadata.name")) {
      return {
        kind: "status",
        resource: rest.slice(0, -".metadata.name".length),
        statusPath: "metadata.name"
      };
    }
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
 * @param {Blueprint|Object} doc The full blueprint document.
 * @returns {Array<{kind:string, param?:string, envKey?:string, srcResource?:string, srcPath?:string, resource:string, path:string, from:string, isEnvelope?:boolean, isAnnotation?:boolean}>}
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
        } else if (parsed.kind === "env") {
          out.push({
            kind: "env",
            envKey: parsed.key,
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
        } else if (parsed.kind === "env") {
          out.push({ kind: "env", envKey: parsed.key, resource: r.name,
            path: "annotations." + key, from: f.from, isAnnotation: true });
        } else if (parsed.kind === "status") {
          out.push({ kind: "status", srcResource: parsed.resource, srcPath: parsed.statusPath,
            resource: r.name, path: "annotations." + key, from: f.from, isAnnotation: true });
        }
      });
    }
    if (r.forEach) {
      const forEachStr = typeof r.forEach === "string" ? r.forEach
        : (r.forEach && typeof r.forEach === "object" && typeof r.forEach.over === "string") ? r.forEach.over : null;
      if (forEachStr) {
        const parsed = parseFrom(forEachStr.trim());
        if (parsed && parsed.kind === "status") {
          out.push({
            kind: "status",
            srcResource: parsed.resource,
            srcPath: parsed.statusPath,
            resource: r.name,
            path: "forEach",
            from: forEachStr
          });
        }
      }
    }
  });
  docWiresCache.set(doc, out);
  return out;
}

/**
 * Parse a when: condition expression.
 * @param {string} str
 * @returns {{source?: string, param?: string, op?: string, val?: string}}
 */
export function parseWhen(str) {
  if (!str || typeof str !== "string") return {};
  const s = str.trim();
  const m = /^(params|parameters|\$params|env|\$env)\.([A-Za-z0-9_.-]+?)(?:\s*(==|!=)\s*"([^"]*)")?$/.exec(s);
  if (!m) {
    const fallback = /^(params|parameters|\$params|env|\$env)\.([A-Za-z0-9_.-]+)/.exec(s);
    if (fallback) {
      const src = (fallback[1] === "env" || fallback[1] === "$env") ? "env" : "params";
      return { source: src, param: fallback[2].replace(/\.+$/, ""), op: "==", val: undefined };
    }
    return {};
  }
  const source = (m[1] === "env" || m[1] === "$env") ? "env" : "params";
  return { source, param: m[2].replace(/\.+$/, ""), op: m[3] || "==", val: m[4] };
}

/**
 * Check whether a reference string references parameter pn.
 * @param {string} ref
 * @param {string} pn
 * @returns {boolean}
 */
export function isParamRef(ref, pn) {
  if (typeof ref !== "string" || !ref || !pn) return false;
  if (ref === "params." + pn || ref.indexOf("params." + pn + ".") === 0) return true;
  if (ref === "parameters." + pn || ref.indexOf("parameters." + pn + ".") === 0) return true;
  if (ref === "$params." + pn || ref.indexOf("$params." + pn + ".") === 0) return true;
  return false;
}

/**
 * Check whether a when: condition references parameter pn.
 * @param {string} whenStr
 * @param {string} pn
 * @returns {boolean}
 */
export function isWhenReferencingParam(whenStr, pn) {
  if (!whenStr || typeof whenStr !== "string") return false;
  if (isParamRef(whenStr, pn)) return true;
  const parsed = parseWhen(whenStr);
  if (parsed && parsed.source === "params" && (parsed.param === pn || (parsed.param && parsed.param.indexOf(pn + ".") === 0))) return true;
  const m = /^(?:params|parameters|\$params)\.([A-Za-z0-9_.-]+)/.exec(whenStr.trim());
  if (m && (m[1] === pn || m[1].indexOf(pn + ".") === 0)) return true;
  return false;
}

/**
 * Extract parameter reference from a when: guard expression.
 * @param {string} when
 * @returns {string|null} Parameter name without "params." prefix, or null.
 */
export function extractWhenParam(when) {
  if (typeof when !== "string") return null;
  const parsed = parseWhen(when);
  if (parsed && parsed.source === "params") return parsed.param || null;
  return null;
}

/**
 * Extract parameter reference from a forEach: expression.
 * @param {string|Object} forEach
 * @returns {string|null} Parameter name without "params." prefix, or null.
 */
export function extractForEachParam(forEach) {
  let str;
  if (typeof forEach === "string") {
    str = forEach;
  } else if (forEach && typeof forEach === "object" && typeof forEach.over === "string") {
    str = forEach.over;
  } else {
    return null;
  }
  const s = str.trim();
  const m = /^(?:params|parameters|\$params)\.([A-Za-z0-9_.-]+)/.exec(s);
  if (!m) return null;
  const p = m[1].replace(/\.+$/, "");
  if (!p) return null;
  return isParamRef(s, p) ? p : null;
}

function extractWhenEnv(when) {
  if (typeof when !== "string") return null;
  const parsed = parseWhen(when);
  if (parsed && parsed.source === "env") return parsed.param || null;
  const s = when.trim();
  const m = /^(?:\$env|env)\.([A-Za-z0-9_.-]+)/.exec(s);
  if (!m) return null;
  const p = m[1].replace(/\.+$/, "");
  return p || null;
}

function extractForEachEnv(forEach) {
  let str;
  if (typeof forEach === "string") {
    str = forEach;
  } else if (forEach && typeof forEach === "object" && typeof forEach.over === "string") {
    str = forEach.over;
  } else {
    return null;
  }
  const s = str.trim();
  const m = /^(?:\$env|env)\.([A-Za-z0-9_.-]+)/.exec(s);
  if (!m) return null;
  const p = m[1].replace(/\.+$/, "");
  return p || null;
}

/**
 * Check whether a raw template/expression string references parameter pn.
 * Matches .spec.<pn>, $spec.<pn>, params.<pn>, parameters.<pn>, etc.
 * Also matches index and hasKey expressions referencing pn.
 * @param {string} raw
 * @param {string} pn
 * @returns {boolean}
 */
export function isRawParamRef(raw, pn) {
  if (typeof raw !== "string" || !raw || !pn) return false;
  const escaped = pn.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const reDotted = new RegExp("(?:\\$spec|\\.spec|\\$params|\\.params|params|parameters)\\." + escaped + "(?:$|[^a-zA-Z0-9_])");
  if (reDotted.test(raw)) return true;
  const reIndex = new RegExp("\\bindex\\s+(?:(?:\\$|\\$\\.|\\.)?observed\\.composite\\.resource\\.spec|(?:\\$|\\$\\.|\\.)spec|(?:\\$|\\$\\.|\\.)?params)\\s+(?:\"" + escaped + "\"|'" + escaped + "'|`" + escaped + "`)(?:$|[^a-zA-Z0-9_])");
  if (reIndex.test(raw)) return true;
  const reHasKey = new RegExp("\\bhasKey\\s+(?:(?:\\$|\\$\\.|\\.)?observed\\.composite\\.resource\\.spec|(?:\\$|\\$\\.|\\.)spec|(?:\\$|\\$\\.|\\.)?params)\\s+(?:\"" + escaped + "\"|'" + escaped + "'|`" + escaped + "`)(?:$|[^a-zA-Z0-9_])");
  return reHasKey.test(raw);
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
  const addParam = function (param) {
    if (!param) return;
    map[param] = (map[param] || 0) + 1;
    const parts = param.split(".");
    for (let p = 1; p < parts.length; p++) {
      const prefix = parts.slice(0, p).join(".");
      map[prefix] = (map[prefix] || 0) + 1;
    }
  };
  const addEnv = function (key) {
    if (!key) return;
    map["env." + key] = (map["env." + key] || 0) + 1;
  };

  const wires = listWires(doc);
  for (let i = 0; i < wires.length; i++) {
    const w = wires[i];
    if (w.kind === "param" && w.param) {
      addParam(w.param);
    } else if (w.kind === "env" && w.envKey) {
      addEnv(w.envKey);
    }
  }

  const resources = (doc && doc.spec && doc.spec.resources) || [];
  for (let i = 0; i < resources.length; i++) {
    const r = resources[i];
    if (!r || typeof r !== "object") continue;
    if (r.when) {
      addParam(extractWhenParam(r.when));
      addEnv(extractWhenEnv(r.when));
    }
    if (r.forEach) {
      addParam(extractForEachParam(r.forEach));
      addEnv(extractForEachEnv(r.forEach));
    }
  }

  const templates = (doc && doc.spec && doc.spec.templates) || {};
  if (templates && typeof templates === "object") {
    const declaredParams = Object.keys((doc.spec && doc.spec.xrd && doc.spec.xrd.parameters) || {});
    const tmplKeys = Object.keys(templates);
    for (let i = 0; i < tmplKeys.length; i++) {
      const body = templates[tmplKeys[i]];
      if (typeof body !== "string") continue;
      const seen = new Set();
      for (let p = 0; p < declaredParams.length; p++) {
        const pn = declaredParams[p];
        if (isRawParamRef(body, pn)) {
          seen.add(pn);
        }
      }
      const rawParamRegex = /(?:\$spec|\.spec|\$params|\.params|params|parameters)\.([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)*)/g;
      let m;
      while ((m = rawParamRegex.exec(body)) !== null) {
        seen.add(m[1]);
      }
      for (const p of seen) {
        const hasMoreSpecific = Array.from(seen).some(function (other) {
          return other !== p && other.startsWith(p + ".");
        });
        if (!hasMoreSpecific) {
          addParam(p);
        }
      }
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

export function envFanOut(doc, key) {
  if (!doc) return 0;
  return findEnvWires(doc, key).length;
}

/**
 * Find all wires referencing an environment key.
 * @param {Object} doc The full blueprint document.
 * @param {string} key Environment key name (without the "env." prefix).
 * @returns {string[]} List of wire target paths (e.g. "my-res.field", "my-res.when").
 */
export function findEnvWires(doc, key) {
  const wires = [];
  const ref = "env." + key;
  const refDollar = "$env." + key;
  const resources = (doc && doc.spec && doc.spec.resources) || [];
  resources.forEach(function (r) {
    const checkDict = function (dict, prefix) {
      if (!dict) return;
      Object.keys(dict).forEach(function (p) {
        const f = dict[p];
        if (f && (f.from === ref || f.from === refDollar)) {
          wires.push(r.name + "." + (prefix ? prefix + "." : "") + p);
        }
      });
    };
    checkDict(r.fields, "");
    checkDict(r.envelope, "envelope");
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f && (f.from === ref || f.from === refDollar)) {
          wires.push(r.name + ".annotations." + k);
        }
      });
    }
    if (r.connectionSecret) {
      let csMatched = false;
      if (typeof r.connectionSecret === "string") {
        if (r.connectionSecret === ref || r.connectionSecret === refDollar) {
          csMatched = true;
        }
      } else if (typeof r.connectionSecret === "object") {
        if (r.connectionSecret.name === ref || r.connectionSecret.name === refDollar ||
            r.connectionSecret.namespace === ref || r.connectionSecret.namespace === refDollar) {
          csMatched = true;
        } else if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys.forEach(function (item) {
            if (typeof item === "string" && (item === ref || item === refDollar)) {
              csMatched = true;
            } else if (item && typeof item === "object" && (item.from === ref || item.from === refDollar)) {
              csMatched = true;
            }
          });
        }
      }
      if (csMatched) {
        wires.push(r.name + ".connectionSecret");
      }
    }
    if (r.when) {
      const escaped = key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      const re = new RegExp("(?:\\$env|\\.env|env)\\." + escaped + "(?:$|[^a-zA-Z0-9_])");
      if (re.test(r.when)) {
        wires.push(r.name + ".when");
      }
    }
    if (r.forEach) {
      if (r.forEach === ref || r.forEach === refDollar) {
        wires.push(r.name + ".forEach");
      } else if (typeof r.forEach === "object" && (r.forEach.over === ref || r.forEach.over === refDollar)) {
        wires.push(r.name + ".forEach");
      }
    }
  });
  return wires;
}

