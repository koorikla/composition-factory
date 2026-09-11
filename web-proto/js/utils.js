/**
 * utils.js — shared utility functions for web-proto regions.
 */

import { store as defaultStore } from "./store.js";

/** Node color families across the canvas and palette. */
export const COLORS = {
  aws: "var(--wire-ref)",
  k8s: "var(--wire-status)",
  xrd: "var(--wire-xrd)",
  azure: "#0078d4",
  gcp: "#ea4335",
  helm: "#0f1689",
  cluster: "#06b6d4",
};

/**
 * Provider family for color scheme classification.
 * Supports k8s, azure, gcp, helm, cluster, aws.
 * Accepts either a provider/group string or an object with provider/group properties.
 *
 * @param {string|Object|null} target
 * @returns {string} One of "k8s", "azure", "gcp", "helm", "cluster", "aws"
 */
export function famOf(target) {
  if (!target) return "k8s";
  let s;
  if (typeof target === "object") {
    if (target.provider === "cluster") return "cluster";
    const p = target.provider || "";
    const g = target.group || "";
    s = (g + " " + p).toLowerCase().trim();
    if (!s) return "k8s";
  } else {
    s = String(target).toLowerCase().trim();
    if (!s) return "k8s";
    if (s === "cluster") return "cluster";
  }
  if (s.indexOf("kubernetes") >= 0 || s === "k8s") return "k8s";
  if (s.indexOf("azure") >= 0) return "azure";
  if (s.indexOf("gcp") >= 0 || s.indexOf("google") >= 0) return "gcp";
  if (s.indexOf("helm") >= 0) return "helm";
  if (s.indexOf("aws") >= 0) return "aws";
  if (typeof target === "object" && !target.provider) return "k8s";
  return "aws";
}

/** Prototype slug: CamelCase -> camel-case. */
export function slug(k) {
  return String(k).replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}

/**
 * Generate a unique resource name in blueprint doc d based on kind.
 *
 * @param {Object} d Blueprint document
 * @param {string} kind Resource kind name (e.g. "Bucket")
 * @returns {string}
 */
export function uniqueResourceName(d, kind) {
  const base = slug(kind);
  const names = {};
  ((d && d.spec && d.spec.resources) || []).forEach(function (r) { names[r.name] = true; });
  if (!names[base]) return base;
  let i = 2;
  while (names[base + "-" + i]) i++;
  return base + "-" + i;
}

/**
 * Clean engine DSL jargon from user-facing error messages.
 * Translates internal mode names (from, value, raw, template) into user terms.
 *
 * @param {string} msg Raw error message
 * @returns {string}
 */
export function humanizeError(msg) {
  if (!msg || typeof msg !== "string") return msg;
  var s = msg;
  s = s.replace(/set exactly one of from, value, raw or template\s*\(got 0\)/gi, "requires a value or wire");
  s = s.replace(/set exactly one of from, value, raw or template\s*\(got (\d+)\)/gi, "set only one value or wire (got $1)");
  s = s.replace(/set exactly one of from, value, raw or template/gi, "requires a value or wire");
  s = s.replace(/mapping with one of value, from, raw, or template/gi, "mapping with a value or wire");
  return s;
}

/**
 * Map index-based spec.resources[N] coordinates in error messages to human-readable resource names.
 *
 * @param {string} msg Raw error message containing spec.resources[N]
 * @param {Object} [storeOrDoc] Optional store or blueprint document instance
 * @returns {string}
 */
export function mapResourceCoordinates(msg, storeOrDoc) {
  if (!msg || typeof msg !== "string") return msg;
  var cleaned = humanizeError(msg);
  return cleaned.replace(/spec\.resources\[(\d+)\]/g, function (match, indexStr) {
    var idx = parseInt(indexStr, 10);
    var doc = (storeOrDoc && storeOrDoc.spec)
      ? storeOrDoc
      : (storeOrDoc && storeOrDoc.state && storeOrDoc.state.doc)
      || (defaultStore && defaultStore.state && defaultStore.state.doc)
      || (typeof window !== "undefined" && window.store && window.store.state && window.store.state.doc);
    var resList = doc && doc.spec && doc.spec.resources;
    if (resList && resList[idx]) {
      var name = resList[idx].name || resList[idx].kind;
      if (name) {
        return "resource '" + name + "' (" + match + ")";
      }
    }
    return match;
  });
}

function isEnvRef(ref, keyName) {
  if (typeof ref !== "string" || !ref || !keyName) return false;
  if (ref === "env." + keyName || ref.indexOf("env." + keyName + ".") === 0) return true;
  if (ref === "$env." + keyName || ref.indexOf("$env." + keyName + ".") === 0) return true;
  return false;
}

function isRawEnvRef(raw, keyName) {
  if (typeof raw !== "string" || !raw || !keyName) return false;
  var escaped = keyName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  var re = new RegExp("(?:\\$env|\\.env|env)\\." + escaped + "(?:$|[^a-zA-Z0-9_])");
  return re.test(raw);
}

function isObjectReferencingEnv(obj, keyName) {
  if (!obj || typeof obj !== "object") return false;
  for (var k of Object.keys(obj)) {
    var v = obj[k];
    if (typeof v === "string" && (isEnvRef(v, keyName) || isRawEnvRef(v, keyName))) return true;
    if (typeof v === "object" && isObjectReferencingEnv(v, keyName)) return true;
  }
  return false;
}

function isWhenReferencingEnv(whenStr, keyName) {
  if (!whenStr || typeof whenStr !== "string") return false;
  if (isEnvRef(whenStr, keyName) || isRawEnvRef(whenStr, keyName)) return true;
  var m = /^(?:env|\$env)\.([A-Za-z0-9_-]+)/.exec(whenStr);
  if (m && m[1] === keyName) return true;
  return false;
}

/**
 * Remove all references to an environment key from resources in a blueprint doc
 * (fields, envelope, annotations, connectionSecret, when, forEach).
 *
 * @param {Object} draft Blueprint document
 * @param {string} keyName Environment key name
 */
export function cleanEnvRefs(draft, keyName) {
  if (!draft || !draft.spec) return;
  var resources = draft.spec.resources || [];
  resources.forEach(function (r) {
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        var f = r.fields[k];
        if (f && (isEnvRef(f.from, keyName) || isRawEnvRef(f.raw, keyName))) {
          delete r.fields[k];
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        var f = r.envelope[k];
        if (f && (isEnvRef(f.from, keyName) || isRawEnvRef(f.raw, keyName))) {
          delete r.envelope[k];
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        var f = r.annotations[k];
        if (f && (isEnvRef(f.from, keyName) || isRawEnvRef(f.raw, keyName))) {
          delete r.annotations[k];
        }
      });
      if (Object.keys(r.annotations).length === 0) delete r.annotations;
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        if (isEnvRef(r.connectionSecret, keyName) || isRawEnvRef(r.connectionSecret, keyName)) {
          delete r.connectionSecret;
        }
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys = r.connectionSecret.keys.filter(function (item) {
            if (typeof item === "string") return !isEnvRef(item, keyName) && !isRawEnvRef(item, keyName);
            if (item && typeof item === "object") {
              if (item.from && (isEnvRef(item.from, keyName) || isRawEnvRef(item.from, keyName))) return false;
              if (item.raw && isRawEnvRef(item.raw, keyName)) return false;
              if (isObjectReferencingEnv(item, keyName)) return false;
            }
            return true;
          });
          if (r.connectionSecret.keys.length === 0) delete r.connectionSecret;
        } else if (isObjectReferencingEnv(r.connectionSecret, keyName)) {
          delete r.connectionSecret;
        }
      }
    }
    if (r.when && isWhenReferencingEnv(r.when, keyName)) {
      delete r.when;
    }
    if (r.forEach && (isEnvRef(r.forEach, keyName) || isRawEnvRef(r.forEach, keyName))) {
      delete r.forEach;
    }
  });
}

/**
 * Delete an environment key from a blueprint doc and remove it from all
 * environmentConfigs data maps and resource references. If no environment keys remain in spec.environment,
 * cleans up both spec.environment and spec.environmentConfigs so the document
 * remains valid under backend validation rules.
 *
 * @param {Object} d Blueprint document
 * @param {string} keyName Environment key name
 */
export function deleteEnvKeyFromDoc(d, keyName) {
  if (!d || !d.spec) return;
  cleanEnvRefs(d, keyName);
  if (d.spec.environment) {
    delete d.spec.environment[keyName];
    if (Object.keys(d.spec.environment).length === 0) {
      delete d.spec.environment;
    }
  }
  if (!d.spec.environment || Object.keys(d.spec.environment).length === 0) {
    delete d.spec.environment;
    delete d.spec.environmentConfigs;
  } else if (Array.isArray(d.spec.environmentConfigs)) {
    d.spec.environmentConfigs.forEach(function (cfg) {
      if (cfg && cfg.data && typeof cfg.data === "object") {
        delete cfg.data[keyName];
      }
      if (cfg && cfg.values && typeof cfg.values === "object") {
        delete cfg.values[keyName];
      }
    });
    if (d.spec.environmentConfigs.length === 0) {
      delete d.spec.environmentConfigs;
    }
  }
}


