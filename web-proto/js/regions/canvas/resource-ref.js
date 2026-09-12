/**
 * canvas/resource-ref.js — Pure reference detection and cleanup for composed resources.
 *
 * Checks whether a raw template expression, wire path, or nested config references
 * a given resource name, mirroring Go's internal/blueprint/edit.go:rawReferencesResource.
 *
 * Detects:
 *  - Dotted references: resources.<name>, observed.resources.<name>, $.observed.resources.<name>
 *  - Go template index: {{ (index $observed.resources "<name>") }}
 *  - Go template hasKey: {{ hasKey $observed.resources "<name>" }}
 *  - Sprig dig: {{ hasKey (dig "resources" "<name>" ...) }}
 *  - Crossplane getComposedResource: {{ getComposedResource $observed "<name>" }}
 *
 * Supports double quotes ("), single quotes ('), and backticks (`).
 * Pure: zero DOM dependencies; runnable in Node and browser.
 */

export function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Checks whether val (wire path or raw expression) references the target resource.
 * @param {*} val
 * @param {string} name
 * @returns {boolean}
 */
export function isResourceRef(val, name) {
  if (typeof val !== "string" || !val || !name) return false;
  if (val === "resources." + name || val.startsWith("resources." + name + ".")) return true;

  const q = escapeRegex(name);

  // 1. Dotted references: resources.<name>, observed.resources.<name>, $.observed.resources.<name>
  const reDotted = new RegExp("(?:^|[^a-zA-Z0-9_$-])(?:(?:\\$|\\$\\.|\\.)?observed\\.resources|resources)\\." + q + "(?:$|[^a-zA-Z0-9_-])");
  if (reDotted.test(val)) return true;

  // 2. index expressions: e.g. {{ (index $observed.resources "<name>").resource.status.url }}
  const reIndex = new RegExp("\\bindex\\s+(?:(?:\\$|\\$\\.|\\.)?(?:observed\\.)?resources)\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reIndex.test(val)) return true;

  // 3. hasKey expressions: e.g. {{ hasKey $observed.resources "<name>" }}
  const reHasKey = new RegExp("\\bhasKey\\s+(?:(?:\\$|\\$\\.|\\.)?(?:observed\\.)?resources)\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reHasKey.test(val)) return true;

  // 4. Sprig dig expressions: e.g. {{ hasKey (dig "resources" "<name>" "resource" "status" dict $.observed) "url" }}
  const reDig = new RegExp("\\bdig\\s+(?:(?:\"observed\"|'observed'|`observed`)\\s+)?(?:\"resources\"|'resources'|`resources`)\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reDig.test(val)) return true;

  // 5. getComposedResource expressions: e.g. {{ getComposedResource $observed "<name>" }}
  const reGetComposed = new RegExp("\\bgetComposedResource\\s+[^\\s\"'`]+\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reGetComposed.test(val)) return true;

  return false;
}

/**
 * Recursively checks whether an object (e.g. connectionSecret) references the given resource.
 * @param {*} obj
 * @param {string} name
 * @returns {boolean}
 */
export function isObjectReferencingResource(obj, name) {
  if (!obj || typeof obj !== "object") return false;
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    if (typeof v === "string" && isResourceRef(v, name)) return true;
    if (typeof v === "object" && isObjectReferencingResource(v, name)) return true;
  }
  return false;
}

/**
 * Finds all downstream references to resource `name` in other resources' fields, envelope,
 * annotations, connectionSecret, when, or forEach.
 * @param {any[]} resources
 * @param {string} name
 * @returns {Array<{name: string, fields: Array<string>}>}
 */
export function findDownstreamRefs(resources, name) {
  const downstream = [];
  (resources || []).forEach(function (r) {
    if (r.name === name) return;
    const depFields = [];
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        const f = r.fields[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          depFields.push(k);
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        const f = r.envelope[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          depFields.push("envelope." + k);
        }
      });
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          depFields.push("annotations." + k);
        }
      });
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string" && isResourceRef(r.connectionSecret, name)) {
        depFields.push("connectionSecret");
      } else if (typeof r.connectionSecret === "object" && isObjectReferencingResource(r.connectionSecret, name)) {
        depFields.push("connectionSecret");
      }
    }
    if (r.when && isResourceRef(r.when, name)) {
      depFields.push("when");
    }
    if (r.forEach && isResourceRef(r.forEach, name)) {
      depFields.push("forEach");
    }
    if (depFields.length > 0) {
      downstream.push({ name: r.name, fields: depFields });
    }
  });
  return downstream;
}

/**
 * Cleans all downstream references to resource `name` in other resources' fields, envelope,
 * annotations, connectionSecret, when, or forEach.
 * @param {any[]} resources
 * @param {string} name
 */
export function cleanDownstreamRefs(resources, name) {
  (resources || []).forEach(function (r) {
    if (r.name === name) return;
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        const f = r.fields[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          delete r.fields[k];
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        const f = r.envelope[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          delete r.envelope[k];
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          delete r.annotations[k];
        }
      });
      if (Object.keys(r.annotations).length === 0) delete r.annotations;
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        if (isResourceRef(r.connectionSecret, name)) delete r.connectionSecret;
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys = r.connectionSecret.keys.filter(function (item) {
            if (typeof item === "string") return !isResourceRef(item, name);
            if (item && typeof item === "object") {
              if (item.from && isResourceRef(item.from, name)) return false;
              if (item.fromNode && item.fromNode === name) return false;
            }
            return true;
          });
          if (r.connectionSecret.keys.length === 0) delete r.connectionSecret;
        } else if (isObjectReferencingResource(r.connectionSecret, name)) {
          delete r.connectionSecret;
        }
      }
    }
    if (r.when && isResourceRef(r.when, name)) {
      delete r.when;
    }
    if (r.forEach && isResourceRef(r.forEach, name)) {
      delete r.forEach;
    }
  });
}
