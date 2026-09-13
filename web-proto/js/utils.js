/**
 * utils.js — shared utility functions for web-proto regions.
 */

import { esc } from "./dom.js";
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

export function isRawEnvRef(raw, keyName) {
  if (typeof raw !== "string" || !raw || !keyName) return false;
  var escaped = keyName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  var re = new RegExp("(?:\\$env|\\.env|env)\\." + escaped + "(?:$|[^a-zA-Z0-9_])");
  if (re.test(raw)) return true;
  var reIndex = new RegExp("\\bindex\\s+(?:(?:\\$|\\$\\.|\\.)?env)\\s+(?:\"" + escaped + "\"|'" + escaped + "'|`" + escaped + "`)(?:$|[^a-zA-Z0-9_])");
  if (reIndex.test(raw)) return true;
  var reHasKey = new RegExp("\\bhasKey\\s+(?:(?:\\$|\\$\\.|\\.)?env)\\s+(?:\"" + escaped + "\"|'" + escaped + "'|`" + escaped + "`)(?:$|[^a-zA-Z0-9_])");
  return reHasKey.test(raw);
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

export function isWhenReferencingEnv(whenStr, keyName) {
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
  var deletedTemplates = [];
  if (draft.spec.templates && typeof draft.spec.templates === "object") {
    Object.keys(draft.spec.templates).forEach(function (tName) {
      if (typeof draft.spec.templates[tName] === "string" && isRawEnvRef(draft.spec.templates[tName], keyName)) {
        delete draft.spec.templates[tName];
        deletedTemplates.push(tName);
      }
    });
    if (Object.keys(draft.spec.templates).length === 0) {
      delete draft.spec.templates;
    }
    if (deletedTemplates.length > 0 && Array.isArray(draft.spec.conventions)) {
      draft.spec.conventions = draft.spec.conventions.filter(function (c) {
        return c && deletedTemplates.indexOf(c.template) === -1;
      });
      if (draft.spec.conventions.length === 0) {
        delete draft.spec.conventions;
      }
    }
  }
  var resources = draft.spec.resources || [];
  resources.forEach(function (r) {
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        var f = r.fields[k];
        if (f && (isEnvRef(f.from, keyName) || isRawEnvRef(f.raw, keyName) || (f.template && deletedTemplates.indexOf(f.template) !== -1))) {
          delete r.fields[k];
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        var f = r.envelope[k];
        if (f && (isEnvRef(f.from, keyName) || isRawEnvRef(f.raw, keyName) || (f.template && deletedTemplates.indexOf(f.template) !== -1))) {
          delete r.envelope[k];
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        var f = r.annotations[k];
        if (f && (isEnvRef(f.from, keyName) || isRawEnvRef(f.raw, keyName) || (f.template && deletedTemplates.indexOf(f.template) !== -1))) {
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
    if (Array.isArray(d.spec.pipeline)) {
      d.spec.pipeline = d.spec.pipeline.filter(function (s) {
        return !(s && (s.functionRef === "function-environment-configs" || s.name === "environment-configs"));
      });
      if (d.spec.pipeline.length === 0) {
        delete d.spec.pipeline;
      }
    }
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

/**
 * Rename environment key references inside Go templates and raw expressions.
 * Handles dotted references, index calls, and hasKey expressions.
 *
 * @param {string} raw
 * @param {string} oldKey
 * @param {string} newKey
 * @returns {string}
 */
export function replaceRawEnv(raw, oldKey, newKey) {
  if (typeof raw !== "string" || !raw || !oldKey || !newKey) return raw;
  var escaped = oldKey.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  var re = new RegExp("((?:\\$env|\\.env|env)\\.)" + escaped + "((?:$|[^a-zA-Z0-9_]))", "g");
  var res = raw.replace(re, "$1" + newKey + "$2");
  var reIndex = new RegExp("(\\bindex\\s+(?:(?:\\$|\\$\\.|\\.)?env)\\s+(?:\"|'|`))" + escaped + "((?:\"|'|`)(?:$|[^a-zA-Z0-9_]))", "g");
  res = res.replace(reIndex, "$1" + newKey + "$2");
  var reHasKey = new RegExp("(\\bhasKey\\s+(?:(?:\\$|\\$\\.|\\.)?env)\\s+(?:\"|'|`))" + escaped + "((?:\"|'|`)(?:$|[^a-zA-Z0-9_]))", "g");
  return res.replace(reHasKey, "$1" + newKey + "$2");
}

/**
 * Rename an environment key in a blueprint doc and rewrite all
 * references to it in resources and environmentConfigs.
 *
 * @param {Object} d Blueprint document
 * @param {string} oldKey Old environment key name
 * @param {string} newKey New environment key name
 */
export function renameEnvKeyInDoc(d, oldKey, newKey) {
  if (!d || !d.spec || !oldKey || !newKey || oldKey === newKey) return;

  // 1. Update spec.environment while preserving order
  if (d.spec.environment && d.spec.environment[oldKey] !== undefined) {
    var newEnv = {};
    Object.keys(d.spec.environment).forEach(function (k) {
      if (k === oldKey) {
        newEnv[newKey] = d.spec.environment[oldKey];
      } else {
        newEnv[k] = d.spec.environment[k];
      }
    });
    d.spec.environment = newEnv;
  }

  // 2. Update spec.environmentConfigs
  if (Array.isArray(d.spec.environmentConfigs)) {
    d.spec.environmentConfigs.forEach(function (cfg) {
      if (cfg && cfg.data && typeof cfg.data === "object" && cfg.data[oldKey] !== undefined) {
        cfg.data[newKey] = cfg.data[oldKey];
        delete cfg.data[oldKey];
      }
      if (cfg && cfg.values && typeof cfg.values === "object" && cfg.values[oldKey] !== undefined) {
        cfg.values[newKey] = cfg.values[oldKey];
        delete cfg.values[oldKey];
      }
    });
  }

  // 3. Helper to replace in strings/expressions
  function replaceEnvRef(ref) {
    if (typeof ref !== "string" || !ref) return ref;
    if (ref === "env." + oldKey) return "env." + newKey;
    if (ref === "$env." + oldKey) return "$env." + newKey;
    if (ref.indexOf("env." + oldKey + ".") === 0) {
      return "env." + newKey + ref.slice(("env." + oldKey).length);
    }
    if (ref.indexOf("$env." + oldKey + ".") === 0) {
      return "$env." + newKey + ref.slice(("$env." + oldKey).length);
    }
    return ref;
  }

  function replaceRaw(raw) {
    return replaceRawEnv(raw, oldKey, newKey);
  }

  function replaceInObject(obj) {
    if (!obj || typeof obj !== "object") return;
    Object.keys(obj).forEach(function (k) {
      var v = obj[k];
      if (typeof v === "string") {
        var r1 = replaceEnvRef(v);
        obj[k] = r1 !== v ? r1 : replaceRaw(v);
      } else if (typeof v === "object") {
        replaceInObject(v);
      }
    });
  }

  // 4. Update references in resources
  var resources = d.spec.resources || [];
  resources.forEach(function (r) {
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        var f = r.fields[k];
        if (f) {
          if (f.from) f.from = replaceEnvRef(f.from);
          if (f.raw) f.raw = replaceRaw(f.raw);
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        var f = r.envelope[k];
        if (f) {
          if (f.from) f.from = replaceEnvRef(f.from);
          if (f.raw) f.raw = replaceRaw(f.raw);
        }
      });
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        var f = r.annotations[k];
        if (f) {
          if (f.from) f.from = replaceEnvRef(f.from);
          if (f.raw) f.raw = replaceRaw(f.raw);
        }
      });
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        r.connectionSecret = replaceEnvRef(r.connectionSecret);
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys.forEach(function (item, idx) {
            if (typeof item === "string") {
              r.connectionSecret.keys[idx] = replaceEnvRef(item);
            } else if (item && typeof item === "object") {
              if (item.from) item.from = replaceEnvRef(item.from);
              if (item.raw) item.raw = replaceRaw(item.raw);
              replaceInObject(item);
            }
          });
        } else {
          replaceInObject(r.connectionSecret);
        }
      }
    }
    if (r.when) {
      if (typeof r.when === "string") {
        r.when = replaceRaw(r.when);
      } else if (typeof r.when === "object") {
        replaceInObject(r.when);
      }
    }
    if (r.forEach) {
      if (typeof r.forEach === "string") {
        r.forEach = replaceEnvRef(r.forEach);
      }
    }
  });

  // 5. Update references in templates
  if (d.spec && d.spec.templates && typeof d.spec.templates === "object") {
    Object.keys(d.spec.templates).forEach(function (tName) {
      if (typeof d.spec.templates[tName] === "string") {
        d.spec.templates[tName] = replaceRaw(d.spec.templates[tName]);
      }
    });
  }
}

/**
 * Resolves the active EnvironmentConfig selection (mode, name, labels) from a blueprint doc.
 * Prioritizes canonical spec.environmentConfigs; falls back to spec.pipeline step input,
 * and defaults to Reference mode with "default".
 *
 * @param {Object} doc Blueprint document
 * @returns {{ mode: "Reference"|"Selector", name: string, labels: string }}
 */
export function parseEnvSelection(doc) {
  if (doc && doc.spec && Array.isArray(doc.spec.environmentConfigs) && doc.spec.environmentConfigs.length > 0) {
    var cfg = doc.spec.environmentConfigs[0];
    if (cfg) {
      if (cfg.selector) {
        var labels = "";
        if (typeof cfg.selector === "string") {
          labels = cfg.selector.trim();
        } else if (cfg.selector.matchLabels && typeof cfg.selector.matchLabels === "object") {
          labels = Object.keys(cfg.selector.matchLabels).sort().map(function (k) {
            return k + "=" + cfg.selector.matchLabels[k];
          }).join(", ");
        } else if (typeof cfg.selector === "object") {
          labels = Object.keys(cfg.selector).sort().map(function (k) {
            return k + "=" + cfg.selector[k];
          }).join(", ");
        }
        return { mode: "Selector", name: cfg.name || "", labels: labels };
      }
      return { mode: "Reference", name: cfg.name || "default", labels: "" };
    }
  }
  var steps = (doc && doc.spec && doc.spec.pipeline) || [];
  for (var i = 0; i < steps.length; i++) {
    if (steps[i].functionRef === "function-environment-configs" || steps[i].name === "environment-configs") {
      var input = steps[i].input || "";
      if (input.indexOf("type: Selector") !== -1 || input.indexOf("selector:") !== -1) {
        var match = input.match(/matchLabels:\s*\n((?:\s+[\w./-]+:\s*.*(?:\n|$))*)/);
        var labelsArr = [];
        if (match && match[1]) {
          var lines = match[1].split("\n");
          lines.forEach(function (l) {
            var m = l.match(/^\s*([\w./-]+):\s*(.*)$/);
            if (m) labelsArr.push(m[1].trim() + "=" + m[2].trim());
          });
        }
        return { mode: "Selector", name: "", labels: labelsArr.join(", ") };
      }
      var nameMatch = input.match(/name:\s*([^\s\n]+)/);
      var name = nameMatch ? nameMatch[1].trim().replace(/^["']|["']$/g, "") : "default";
      return { mode: "Reference", name: name, labels: "" };
    }
  }
  return { mode: "Reference", name: "default", labels: "" };
}

/**
 * Returns the display name or label selector for the EnvironmentConfig card/header.
 *
 * @param {Object} doc Blueprint document
 * @returns {string}
 */
export function getEnvConfigName(doc) {
  var sel = parseEnvSelection(doc);
  return sel.mode === "Reference" ? (sel.name || "default") : (sel.labels || "selector");
}

/**
 * Count empty environment key values in blueprint doc.
 * @param {Object} doc
 * @returns {number}
 */
export function countEmptyValues(doc) {
  var env = (doc && doc.spec && doc.spec.environment) || {};
  var emptyCount = 0;
  Object.keys(env).forEach(function (k) {
    var item = env[k] || {};
    var val = item.value !== undefined && item.value !== "" ? item.value : (item.default !== undefined ? item.default : "");
    if (val === "" || val === null || val === undefined) {
      emptyCount++;
    }
  });
  return emptyCount;
}

/**
 * Generate EnvironmentConfig YAML for the environment keys.
 * @param {Object} doc
 * @param {Object} selInfo
 * @returns {string}
 */
export function generateEnvironmentConfigYAML(doc, selInfo) {
  var env = (doc && doc.spec && doc.spec.environment) || {};
  var keys = Object.keys(env).sort();
  var name = "default";
  if (selInfo && selInfo.mode === "Reference") {
    name = selInfo.name || "default";
  } else if (selInfo && selInfo.mode === "Selector") {
    if (selInfo.name) {
      name = selInfo.name;
    } else if (selInfo.labels) {
      var parts = [];
      selInfo.labels.split(",").forEach(function (pair) {
        var kv = pair.split("=");
        if (kv.length === 2 && kv[0].trim()) {
          parts.push(kv[0].trim() + "-" + kv[1].trim());
        }
      });
      parts.sort();
      name = parts.join("-") || "default";
    }
  }
  var lines = [
    "apiVersion: apiextensions.crossplane.io/v1beta1",
    "kind: EnvironmentConfig",
    "metadata:",
    "  name: " + name
  ];
  if (selInfo && selInfo.mode === "Selector" && selInfo.labels) {
    lines.push("  labels:");
    selInfo.labels.split(",").forEach(function (pair) {
      var parts = pair.split("=");
      if (parts.length === 2 && parts[0].trim()) {
        lines.push("    " + parts[0].trim() + ": " + JSON.stringify(parts[1].trim()));
      }
    });
  }
  lines.push("data:");
  if (keys.length === 0) {
    lines.push("  {}");
  } else {
    keys.forEach(function (k) {
      var item = env[k] || {};
      var val = item.value !== undefined && item.value !== "" ? item.value : (item.default !== undefined ? item.default : "");
      if (item.type === "integer" || item.type === "number" || item.type === "boolean") {
        if (val === "" || val === null || val === undefined) {
          lines.push('  ' + k + ': ""');
        } else {
          lines.push('  ' + k + ': ' + val);
        }
      } else {
        lines.push('  ' + k + ': ' + JSON.stringify(val || ""));
      }
    });
  }
  return lines.join("\n");
}


/** Prototype-style YAML highlighting over plain text. */
export function highlight(text) {
  return text.split("\n").map(function (line) {
    if (/^\s*#/.test(line)) return '<span class="cm">' + esc(line) + "</span>";
    var m = line.match(/^(\s*(?:-\s+)?)([\w.$/"'-]+):(\s*)(.*)$/);
    if (m) {
      var val = m[4];
      var h = esc(m[1]) + '<span class="kk">' + esc(m[2]) + "</span>:" + m[3];
      if (val) {
        var cls = /\{\{/.test(val) ? "tm" : "st";
        h += '<span class="' + cls + '">' + esc(val) + "</span>";
      }
      return h;
    }
    if (/\{\{/.test(line)) return '<span class="tm">' + esc(line) + "</span>";
    return esc(line);
  }).join("\n");
}
