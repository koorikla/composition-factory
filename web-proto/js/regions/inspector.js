/**
 * Region: INSPECTOR. Root element: #region-inspector (body: #insp, filter: #fseg).
 *
 * Resource selected: header (kind, name, apiVersion, leaf/required counts) and
 * the field list from /api/kinds/{apiVersion}/{kind}/fields with the
 * prototype's Required|Set|All filter and per-field V/W/R modes:
 *   V — literal value  -> replaceDoc sets {value}
 *   W — wire           -> dropdown of type-compatible params + "new parameter…"
 *                         (POST parameters route, then bind {from:"params.X"})
 *   R — raw template   -> textarea -> replaceDoc sets {raw}
 * Editing a wired field in V mode asks before overwriting the wire.
 * Server 400s render verbatim in the prototype's .warnbar styling; the doc
 * stays unchanged (store contract).
 *
 * XRD selected ("xrd"): parameter list with add/rename/delete/update via the
 * parameter routes (each returns the full persisted blueprint -> store adopts).
 */

import { store as defaultStore } from "../store.js";
import * as defaultApi from "../api.js";
import { esc } from "../dom.js";
import { fanOut, parseFrom, listWires, findEnvWires } from "../wires.js";
import { mapResourceCoordinates, deleteEnvKeyFromDoc, renameEnvKeyInDoc } from "../utils.js";

function isParamRequired(params, pName) {
  if (!params || !pName) return false;
  const parts = pName.split(".");
  let cur = params[parts[0]];
  if (!cur) return false;
  if (parts.length === 1) {
    return !!(cur.required || cur.requiredChain);
  }
  for (let i = 1; i < parts.length; i++) {
    if (!cur.properties || !cur.properties[parts[i]]) return false;
    cur = cur.properties[parts[i]];
  }
  return !!(cur.required || cur.requiredChain);
}

function isOptParamWire(fromVal, params) {
  const parsed = parseFrom(fromVal);
  if (!parsed || parsed.kind !== "param") return false;
  return !isParamRequired(params, parsed.param);
}

var PARAM_TYPES = ["string", "integer", "number", "boolean", "object"];

var store = defaultStore;
var api = defaultApi;
var root = null;
var box = null;   // #insp
var fseg = null;  // #fseg

var filter = "req";              // "req" | "set" | "all"
var warnMsg = null;              // verbatim server error to show, or null
var uiMode = {};                 // path -> "v"|"w"|"r" local mode override (selected resource only)
var pendingNewParam = null;      // field path currently showing the inline new-parameter form
var pendingNewMapEntry = null;   // map field path currently showing the inline add-key form
var pendingFocusParam = null;    // parameter name to focus and select in XRD inspector after render
var paramOrder = null;           // stable order of XRD parameter names while inspector is open
var pendingRenamedParam = null;  // { from: string, to: string } to follow focus across async rename
var pendingFocusEnvKey = null;   // environment key name to focus and select after render
var pendingRenamedEnvKey = null; // { from: string, to: string } to follow focus across async rename
var lastDocName = null;
var annDraftKey = "";            // draft annotation key being entered (CF-136)
var annDraftRes = "";            // resource name annDraftKey belongs to (CF-136)
var renderToken = 0;

var kindsPromise = null;         // cached GET /api/kinds
var fieldsCache = {};            // "apiVersion|kind" -> {fields,total}
var kindDetailCache = {};        // "apiVersion|kind" -> {kind, envelope, status}

/* ---------------- helpers ---------------- */

function selectedResource() {
  var doc = store.state.doc, sel = store.state.selectedResource;
  if (!doc || !sel || sel === "xrd") return null;
  var list = doc.spec && doc.spec.resources || [];
  for (var i = 0; i < list.length; i++) if (list[i].name === sel) return list[i];
  return null;
}

function entryOf(res, path, isEnvelope) {
  var map = isEnvelope ? (res && res.envelope) : (res && res.fields);
  var f = map && map[path];
  if (!f || typeof f !== "object") return null;
  var from = typeof f.from === "string" ? f.from : "";
  var value = typeof f.value === "string" ? f.value : "";
  var raw = typeof f.raw === "string" ? f.raw : "";
  if (!from && !value && !raw) return null;
  return { from: from, value: value, raw: raw };
}

function envelopeEntryOf(res, path) {
  return entryOf(res, path, true);
}

function docMode(entry, isEnvelope) {
  if (!entry) return "v";
  if (entry.from) return "w";
  if (isEnvelope && entry.raw === "{{ $xr }}") return "w";
  if (entry.raw) return "r";
  return "v";
}

function compatible(paramType, fieldType) {
  if (!fieldType || !paramType) return true;
  if (fieldType === paramType) return true;
  if (fieldType === "number" && paramType === "integer") return true;
  if (fieldType === "map" && paramType === "object") return true;
  if (fieldType === "object" && paramType === "map") return true;
  return false;
}

function suggestedParamType(fieldType) {
  if (fieldType === "integer") return "integer";
  if (fieldType === "number") return "number";
  if (fieldType === "boolean") return "boolean";
  return "string";
}

/** Parse the engine's when grammar: params.x | params.x == "lit" | != */
function parseWhen(str) {
  if (!str) return {};
  var m = /^params\.([A-Za-z][A-Za-z0-9]*)(?:\s(==|!=)\s"([^"]*)")?$/.exec(str);
  if (!m) return {};
  return { param: m[1], op: m[2] || "==", val: m[3] };
}

function whenFromControls(root, rn) {
  var pSel = root.querySelector('[data-when-param="' + CSS.escape(rn) + '"]');
  var p = pSel && pSel.value;
  if (!p) return null;
  var params = paramsOf(store.state.doc);
  var decl = params[p] || {};
  if (decl.type === "boolean") return "params." + p; // bare: engine's boolean form
  // string param: compose the full comparison — when the op/value controls
  // haven't rendered yet (param just chosen), default to == first enum value
  var opEl = root.querySelector('[data-when-op="' + CSS.escape(rn) + '"]');
  var valEl = root.querySelector('[data-when-val="' + CSS.escape(rn) + '"]');
  var op = opEl ? opEl.value : "==";
  var val = valEl ? valEl.value : ((decl.enum && decl.enum[0]) || "");
  return "params." + p + " " + op + ' "' + val + '"';
}

function paramsOf(doc) {
  return doc && doc.spec && doc.spec.xrd && doc.spec.xrd.parameters || {};
}

function isParamLocked(doc, n) {
  if (!doc || !doc.spec || n !== "providerName") return false;
  var xrd = doc.spec.xrd || {};
  var scope = xrd.scope || "Namespaced";
  if (scope !== "Namespaced") return false;
  var resources = doc.spec.resources || [];
  return resources.length === 0 || resources.some(function (r) {
    return r && r.provider !== "k8s";
  });
}

function isParamRef(ref, pn) {
  if (typeof ref !== "string" || !ref || !pn) return false;
  if (ref === "params." + pn || ref.indexOf("params." + pn + ".") === 0) return true;
  if (ref === "parameters." + pn || ref.indexOf("parameters." + pn + ".") === 0) return true;
  return false;
}

function isRawParamRef(raw, pn) {
  if (typeof raw !== "string" || !raw || !pn) return false;
  var escaped = pn.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  var re = new RegExp("(?:\\$spec|\\.spec|\\$params|\\.params|params|parameters)\\." + escaped + "(?:$|[^a-zA-Z0-9_])");
  return re.test(raw);
}

function isObjectReferencingParam(obj, pn) {
  if (!obj || typeof obj !== "object") return false;
  for (var k of Object.keys(obj)) {
    var v = obj[k];
    if (typeof v === "string" && (isParamRef(v, pn) || isRawParamRef(v, pn))) return true;
    if (typeof v === "object" && isObjectReferencingParam(v, pn)) return true;
  }
  return false;
}

function isWhenReferencingParam(whenStr, pn) {
  if (!whenStr || typeof whenStr !== "string") return false;
  if (isParamRef(whenStr, pn)) return true;
  var parsed = parseWhen(whenStr);
  if (parsed && parsed.param === pn) return true;
  var m = /^(?:params|parameters)\.([A-Za-z0-9_-]+)/.exec(whenStr);
  if (m && m[1] === pn) return true;
  return false;
}

function cleanParamRefs(draft, pn) {
  if (!draft || !draft.spec) return;
  if (draft.spec.xrd && draft.spec.xrd.parameters) {
    delete draft.spec.xrd.parameters[pn];
  }
  var resources = draft.spec.resources || [];
  resources.forEach(function (r) {
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        var f = r.fields[k];
        if (f && (isParamRef(f.from, pn) || isRawParamRef(f.raw, pn))) {
          delete r.fields[k];
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        var f = r.envelope[k];
        if (f && (isParamRef(f.from, pn) || isRawParamRef(f.raw, pn))) {
          delete r.envelope[k];
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        var f = r.annotations[k];
        if (f && (isParamRef(f.from, pn) || isRawParamRef(f.raw, pn))) {
          delete r.annotations[k];
        }
      });
      if (Object.keys(r.annotations).length === 0) delete r.annotations;
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        if (isParamRef(r.connectionSecret, pn) || isRawParamRef(r.connectionSecret, pn)) {
          delete r.connectionSecret;
        }
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys = r.connectionSecret.keys.filter(function (item) {
            if (typeof item === "string") return !isParamRef(item, pn) && !isRawParamRef(item, pn);
            if (item && typeof item === "object") {
              if (item.from && (isParamRef(item.from, pn) || isRawParamRef(item.from, pn))) return false;
              if (item.raw && isRawParamRef(item.raw, pn)) return false;
              if (isObjectReferencingParam(item, pn)) return false;
            }
            return true;
          });
          if (r.connectionSecret.keys.length === 0) delete r.connectionSecret;
        } else if (isObjectReferencingParam(r.connectionSecret, pn)) {
          delete r.connectionSecret;
        }
      }
    }
    if (r.when && isWhenReferencingParam(r.when, pn)) {
      delete r.when;
    }
    if (r.forEach && (isParamRef(r.forEach, pn) || isRawParamRef(r.forEach, pn))) {
      delete r.forEach;
    }
  });
}

function getKindsCached() {
  if (!kindsPromise) {
    kindsPromise = api.getKinds().catch(function (e) {
      kindsPromise = null;
      throw e;
    });
  }
  return kindsPromise;
}

/** Resolve a resource's apiVersion via /api/kinds (match kind+provider, then kind). */
async function kindMeta(res) {
  var data = await getKindsCached();
  var kinds = data && data.kinds || [];
  var m = kinds.filter(function (k) { return k.kind === res.kind && k.provider === res.provider; })[0]
       || kinds.filter(function (k) { return k.kind === res.kind; })[0]
       || null;
  if (!m) {
    // Invalidate cached kinds and retry once in case a new provider was declared in doc
    kindsPromise = null;
    data = await getKindsCached().catch(function () { return null; });
    kinds = data && data.kinds || [];
    m = kinds.filter(function (k) { return k.kind === res.kind && k.provider === res.provider; })[0]
     || kinds.filter(function (k) { return k.kind === res.kind; })[0]
     || null;
  }
  return m;
}

async function fieldsFor(apiVersion, kind) {
  var key = apiVersion + "|" + kind;
  if (!fieldsCache[key]) {
    fieldsCache[key] = await api.getKindFields(apiVersion, kind);
  }
  return fieldsCache[key];
}

async function kindDetail(apiVersion, kind) {
  var key = apiVersion + "|" + kind;
  if (!kindDetailCache[key]) {
    kindDetailCache[key] = await api.getKind(apiVersion, kind).catch(function () { return null; });
  }
  return kindDetailCache[key];
}

var catalogueFnsCache = null;
async function getCatalogueFunctions() {
  if (catalogueFnsCache) return catalogueFnsCache;
  try {
    var res = await api.getCatalogue("", "function");
    catalogueFnsCache = (res && res.providers) || [];
  } catch (_) {
    catalogueFnsCache = [];
  }
  return catalogueFnsCache;
}

var pipeInputMode = {};

function inferFnMeta(step) {
  if (!step) return null;
  var pkg = step.package || "";
  var fn = step.functionRef || step.name || "";
  if (pkg.indexOf("function-environment-configs") !== -1 || fn.indexOf("environment-configs") !== -1) {
    return { apiVersion: "environmentconfigs.fn.crossplane.io/v1beta1", kind: "Input" };
  }
  if (pkg.indexOf("function-go-templating") !== -1 || fn.indexOf("go-templating") !== -1) {
    return { apiVersion: "gotemplating.fn.crossplane.io/v1beta1", kind: "GoTemplate" };
  }
  if (pkg.indexOf("function-cel-filter") !== -1 || fn.indexOf("cel-filter") !== -1) {
    return { apiVersion: "cel.fn.crossplane.io/v1alpha1", kind: "Filter" };
  }
  if (pkg.indexOf("function-extra-resources") !== -1 || fn.indexOf("extra-resources") !== -1) {
    return { apiVersion: "extraresources.fn.crossplane.io/v1alpha1", kind: "ExtraResources" };
  }
  return null;
}

function parseInputYAML(str) {
  if (!str) return {};
  var out = {};
  var lines = str.split("\n");
  var stack = [{ obj: out, indent: -1 }];
  for (var i = 0; i < lines.length; i++) {
    var line = lines[i];
    var trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    var indent = line.search(/\S/);
    var colonIdx = line.indexOf(":");
    if (colonIdx === -1) continue;
    var key = line.slice(indent, colonIdx).trim();
    var val = line.slice(colonIdx + 1).trim();
    while (stack.length > 1 && stack[stack.length - 1].indent >= indent) {
      stack.pop();
    }
    var parent = stack[stack.length - 1].obj;
    if (val === "" || val === "|") {
      var newObj = {};
      parent[key] = newObj;
      stack.push({ obj: newObj, indent: indent });
    } else {
      var unquoted = val.replace(/^["']|["']$/g, "");
      parent[key] = unquoted;
    }
  }
  return out;
}

function serializeInputYAML(obj, indent) {
  indent = indent || 0;
  var pad = "  ".repeat(indent);
  var lines = [];
  var keys = Object.keys(obj);
  if (indent === 0) {
    keys.sort(function (a, b) {
      if (a === "apiVersion") return -1;
      if (b === "apiVersion") return 1;
      if (a === "kind") return -1;
      if (b === "kind") return 1;
      return a.localeCompare(b);
    });
  } else {
    keys.sort();
  }
  for (var i = 0; i < keys.length; i++) {
    var k = keys[i];
    var v = obj[k];
    if (v === undefined || v === null || v === "") continue;
    if (typeof v === "object" && !Array.isArray(v)) {
      var nested = serializeInputYAML(v, indent + 1);
      if (nested) {
        lines.push(pad + k + ":\n" + nested);
      }
    } else if (typeof v === "string" && v.indexOf("\n") !== -1) {
      lines.push(pad + k + ": |\n" + v.split("\n").map(function (l) { return pad + "  " + l; }).join("\n"));
    } else {
      lines.push(pad + k + ": " + v);
    }
  }
  return lines.join("\n");
}

function getPathVal(obj, path) {
  var parts = path.split(".");
  var cur = obj;
  for (var i = 0; i < parts.length; i++) {
    if (!cur || typeof cur !== "object") return "";
    cur = cur[parts[i]];
  }
  return cur === undefined || cur === null ? "" : String(cur);
}

function setPathVal(obj, path, val) {
  var parts = path.split(".");
  var cur = obj;
  for (var i = 0; i < parts.length - 1; i++) {
    var p = parts[i];
    if (!cur[p] || typeof cur[p] !== "object") cur[p] = {};
    cur = cur[p];
  }
  if (val === "" || val === undefined) {
    delete cur[parts[parts.length - 1]];
  } else {
    cur[parts[parts.length - 1]] = val;
  }
}

export { mapResourceCoordinates };

/**
 * Run a store operation, capturing the "error" the store emits for it so the
 * inspector can show the server's message in its warnbar.
 * warnMsg is cleared up-front so the success-path "doc" re-render is clean.
 */
export async function op(fn, actionContext) {
  warnMsg = null;
  var err = null;
  var un = store.subscribe("error", function (e) { err = e; });
  var res;
  try {
    res = await fn();
  } catch (e) {
    if (!err) err = e;
    res = null;
  } finally {
    un();
  }
  if (res === null) {
    var detail = err ? (err.message || "") : "";
    if (detail) {
      warnMsg = mapResourceCoordinates(detail);
    } else {
      var act = actionContext || (store.state && store.state.selectedResource ? "unable to update resource '" + store.state.selectedResource + "'" : "unable to update field");
      warnMsg = act.indexOf("Operation failed") === 0 || act.indexOf("Failed") === 0
        ? act
        : "Operation failed: " + act;
    }
    render();
  }
  return res;
}

/* ---------------- rendering: resource ---------------- */

function warnHtml() {
  return warnMsg
    ? '<div class="warnbar" style="border-bottom:1px solid var(--rule)">' + esc(warnMsg) + "</div>"
    : "";
}

function formatDescHtml(desc, path) {
  if (!desc) return "";
  var isLong = desc.length > 90 || desc.indexOf("\n") !== -1;
  if (!isLong) return '<div class="fld-d">' + esc(desc) + "</div>";
  return '<div class="fld-d trunc" data-desc-path="' + esc(path) + '">' +
    '<span class="desc-text">' + esc(desc) + "</span>" +
    '<button type="button" class="desc-more-btn" data-toggle-desc="' + esc(path) + '">more</button>' +
    "</div>";
}

function modeButtons(path, pressed, isEnv) {
  var titles = { v: "Literal value", w: "Wire from a parameter or resource status", r: "Raw go-template" };
  var labels = { v: "Val", w: "Wire", r: "Raw" };
  var envAttr = isEnv ? ' data-env="1"' : "";
  return '<span class="modes">' + ["v", "w", "r"].map(function (x) {
    return '<button' + envAttr + ' data-m="' + x + '" data-path="' + esc(path) + '" aria-pressed="' +
      (pressed === x) + '" title="' + titles[x] + '">' + labels[x] + "</button>";
  }).join("") + "</span>";
}

function wireSelectHtml(path, fieldType, params, otherResources, otherStatusMap, isEnv, isRequired, currentFrom, env) {
  if (!env && store.state && store.state.doc && store.state.doc.spec) {
    env = store.state.doc.spec.environment;
  }
  env = env || {};
  var names = [];
  function collectMemberRefs(prefix, props) {
    Object.keys(props || {}).sort().forEach(function (mn) {
      var mp = props[mn];
      if (mp.type === "object" && mp.properties) {
        collectMemberRefs(prefix + "." + mn, mp.properties); // arbitrary depth
        return;
      }
      if (compatible(mp.type, fieldType)) names.push(prefix + "." + mn);
    });
  }
  Object.keys(params).forEach(function (n) {
    var p = params[n];
    if (p.type === "object" && p.properties) {
      collectMemberRefs(n, p.properties);
      return; // a typed object itself is not a scalar wire target
    }
    if (compatible(p.type, fieldType)) names.push(n);
  });
  var wireAttr = isEnv ? 'data-env-wire="' : 'data-wire="';
  var reqAttr = isRequired ? ' data-fld-req="true"' : "";
  var npKey = isEnv ? ("env:" + path) : path;
  var h = '<div class="bound"><span style="color:var(--faint)">&#8592;</span>' +
    '<select class="tsel" ' + wireAttr + esc(path) + '"' + reqAttr + ' style="flex:1">' +
    '<option value="">wire to&#8230;</option>';

  if (isEnv) {
    h += '<optgroup label="Context">';
    h += '<option value="$xr"' + (currentFrom === "$xr" ? ' selected' : '') + '>XR name ($xr)</option>';
    h += '</optgroup>';
  }

  if (names.length > 0) {
    h += '<optgroup label="XRD Parameters">';
    names.forEach(function (n) {
      var isOpt = !isParamRequired(params, n);
      var optNote = isOpt && isRequired ? " (optional \u2192 req)" : "";
      var isSel = currentFrom === ("params." + n);
      h += '<option value="params.' + esc(n) + '"' + (isSel ? " selected" : "") + '>params.' + esc(n) + esc(optNote) + "</option>";
    });
    h += '</optgroup>';
  }

  var envKeys = [];
  Object.keys(env).sort().forEach(function (k) {
    var kDef = env[k] || {};
    var kType = (typeof kDef === "object" && kDef.type) ? kDef.type : "string";
    if (compatible(kType, fieldType)) {
      envKeys.push(k);
    }
  });
  if (envKeys.length > 0) {
    h += '<optgroup label="Environment">';
    envKeys.forEach(function (k) {
      var kDef = env[k] || {};
      var kType = (typeof kDef === "object" && kDef.type) ? kDef.type : "string";
      var wireVal = "env." + k;
      var isSel = currentFrom === wireVal;
      h += '<option value="' + esc(wireVal) + '"' + (isSel ? ' selected' : '') + '>' + esc(wireVal) + ' (' + esc(kType) + ')' + '</option>';
    });
    h += '</optgroup>';
  }

  if (otherResources && otherResources.length > 0) {
    var isRefField = /Ref(\.name)?$|Refs(\[\d+\])?(\.name)?$|Selector(\.matchLabels)?$/i.test(path);
    if (isRefField || fieldType === "string" || !fieldType) {
      h += '<optgroup label="Resource Name (*Ref)">';
      otherResources.forEach(function (r) {
        var wireVal = "resources." + r.name + ".status.atProvider.id";
        var isSel = currentFrom === wireVal || currentFrom === ("resources." + r.name + ".metadata.name");
        h += '<option value="' + esc(wireVal) + '"' + (isSel ? " selected" : "") + '>' + esc(r.name) + ' (name / ID)</option>';
      });
      h += '</optgroup>';
    }

    h += '<optgroup label="Resource Status">';
    otherResources.forEach(function (r) {
      var sfs = (otherStatusMap && otherStatusMap[r.name]) || [
        { path: "atProvider.url", type: "string" },
        { path: "atProvider.arn", type: "string" },
        { path: "atProvider.id", type: "string" },
      ];
      sfs.forEach(function (sf) {
        if (!fieldType || compatible(sf.type, fieldType)) {
          var wireVal = "resources." + r.name + ".status." + sf.path;
          var isSel = currentFrom === wireVal;
          h += '<option value="' + esc(wireVal) + '"' + (isSel ? " selected" : "") + '>' + esc(wireVal) + "</option>";
        }
      });
    });
    h += '</optgroup>';
  }

  h += '<option value="__new__">+ new XRD parameter&#8230;</option></select></div>';
  if (isRequired && currentFrom && isOptParamWire(currentFrom, params)) {
    h += '<div style="margin-top:2px"><span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span></div>';
  }
  if (pendingNewParam === npKey) {
    h += '<div class="frow" style="margin-top:4px;margin-bottom:0">' +
      '<input class="tin" data-npname="' + esc(npKey) + '" placeholder="parameterName" aria-label="New parameter name">' +
      '<select class="tsel" data-nptype="' + esc(npKey) + '" aria-label="New parameter type">' +
      PARAM_TYPES.map(function (t) {
        return "<option" + (t === suggestedParamType(fieldType) ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<label style="display:flex;align-items:center;gap:3px;font-size:11px;color:var(--faint);cursor:pointer" title="Required parameter">' +
      '<input type="checkbox" data-npreq="' + esc(npKey) + '"' + (isRequired ? ' checked' : '') + '> req</label>' +
      '<button class="btn sm" data-npok="' + esc(npKey) + '"' + (isRequired ? ' data-npreq="true"' : '') + '>Add</button>' +
      '<button class="del" data-npcancel="' + esc(npKey) + '" title="Cancel">&#215;</button></div>';
  }
  return h;
}

function buildSnippets(res, fieldPath, params, otherResources, otherStatusMap, doc) {
  var groups = [];

  // 1. XRD Parameters
  var paramItems = [];
  Object.keys(params || {}).sort().forEach(function (pn) {
    var p = params[pn] || {};
    if (p.type === "object") {
      paramItems.push({ label: "$spec." + pn + " (toYaml)", snippet: "{{ toYaml $spec." + pn + " | nindent 6 }}" });
      if (p.properties) {
        Object.keys(p.properties).sort().forEach(function (mn) {
          paramItems.push({ label: "$spec." + pn + "." + mn, snippet: "{{ $spec." + pn + "." + mn + " }}" });
        });
      }
    } else {
      paramItems.push({ label: "$spec." + pn, snippet: "{{ $spec." + pn + " }}" });
      paramItems.push({ label: "default($spec." + pn + ")", snippet: '{{ default "' + (p.default || "value") + '" $spec.' + pn + " }}" });
    }
  });
  if (paramItems.length > 0) {
    groups.push({ label: "XRD Parameters", items: paramItems });
  }

  // 2. Composite ($xr) & Loop ($i) Context
  var ctxItems = [
    { label: "$xr (Composite Name)", snippet: "{{ $xr }}" },
    { label: "$xrMeta.namespace", snippet: "{{ $xrMeta.namespace }}" }
  ];
  if (res && res.forEach) {
    ctxItems.push({ label: "$i (Loop Index)", snippet: "{{ $i }}" });
    ctxItems.push({ label: 'printf "%s-%d" $xr $i', snippet: '{{ printf "%s-%d" $xr $i }}' });
  }
  groups.push({ label: "Context & Loops", items: ctxItems });

  // 3. Sibling Resource Status
  if (otherResources && otherResources.length > 0) {
    var statusItems = [];
    otherResources.forEach(function (or) {
      var sfs = (otherStatusMap && otherStatusMap[or.name]) || [
        { path: "atProvider.id", type: "string" },
        { path: "atProvider.arn", type: "string" },
        { path: "atProvider.url", type: "string" }
      ];
      sfs.forEach(function (sf) {
        statusItems.push({
          label: or.name + ".status." + sf.path,
          snippet: '{{ (index $.observed.resources "' + or.name + '").resource.status.' + sf.path + " }}"
        });
      });
    });
    if (statusItems.length > 0) {
      groups.push({ label: "Sibling Resource Status", items: statusItems });
    }
  }

  // 4. Environment
  var envItems = [
    { label: "$env.env", snippet: "{{ $env.env }}" },
    { label: "$env.region", snippet: "{{ $env.region }}" },
    { label: "$env.account", snippet: "{{ $env.account }}" }
  ];
  groups.push({ label: "Environment ($env)", items: envItems });

  // 5. Templates
  var tmpls = (doc && doc.spec && doc.spec.templates) || {};
  var tmplNames = Object.keys(tmpls).sort();
  if (tmplNames.length > 0) {
    var tmplItems = tmplNames.map(function (tn) {
      return { label: 'include "' + tn + '"', snippet: '{{ include "' + tn + '" . }}' };
    });
    groups.push({ label: "Templates", items: tmplItems });
  }

  return groups;
}

function rawEditorHtml(path, val, isEnv, res, params, otherResources, otherStatusMap) {
  var doc = store.state.doc;
  var groups = buildSnippets(res, path, params, otherResources, otherStatusMap, doc);
  var rawAttr = isEnv ? 'data-env-raw="' : 'data-raw="';
  var snippetAttr = isEnv ? 'data-env-insert-snippet="' : 'data-insert-snippet="';
  var previewFor = isEnv ? ("env:" + path) : path;

  var h = '<div class="raw-editor-wrap">' +
    '<textarea class="val raw" ' + rawAttr + esc(path) + '" rows="2" placeholder="{{ }}">' + esc(val || "") + '</textarea>' +
    '<div class="snippets-bar">' +
    '<span class="snippets-lbl">Snippets:</span>' +
    '<select class="tsel snippet-select" ' + snippetAttr + esc(path) + '" aria-label="Insert snippet">' +
    '<option value="">+ Insert snippet…</option>';

  groups.forEach(function (g) {
    h += '<optgroup label="' + esc(g.label) + '">';
    g.items.forEach(function (item) {
      h += '<option value="' + esc(item.snippet) + '">' + esc(item.label) + '</option>';
    });
    h += '</optgroup>';
  });

  h += '</select>';

  var quickList = [];
  quickList.push({ label: "$xr", snippet: "{{ $xr }}" });
  if (res && res.forEach) {
    quickList.push({ label: "$i", snippet: "{{ $i }}" });
  }
  var pnames = Object.keys(params || {});
  if (pnames.length > 0) {
    quickList.push({ label: "$" + pnames[0], snippet: "{{ $spec." + pnames[0] + " }}" });
  }
  quickList.forEach(function (qc) {
    var chipAttr = isEnv ? 'data-env-quick-snippet="' : 'data-quick-snippet="';
    h += '<button type="button" class="snippet-chip" ' + chipAttr + esc(path) + '" data-snippet-val="' + esc(qc.snippet) + '" title="Insert ' + esc(qc.snippet) + '">' + esc(qc.label) + '</button>';
  });

  h += '</div>' +
    '<div class="expr-preview" data-preview-for="' + esc(previewFor) + '" style="display:none">' +
    '<div class="expr-preview-h">' +
    '<span class="expr-preview-title">Live Preview</span>' +
    '<span class="expr-preview-badge"></span>' +
    '</div>' +
    '<div class="expr-preview-body"></div>' +
    '</div>' +
    '</div>';

  return h;
}

function isFieldEffectivelyRequired(f, res) {
  if (!f) return false;
  if (f.branch) return true;
  if (!f.required && !f.requiredChain) return false;
  if (f.requiredChain) return true;
  var parts = f.path.split(".");
  var resFields = (res && res.fields) || {};
  for (var i = 1; i < parts.length; i++) {
    var ancestor = parts.slice(0, i).join(".");
    var hasSet = Object.keys(resFields).some(function (k) {
      if (k === ancestor || k.startsWith(ancestor + ".") || k.startsWith(ancestor + "[")) {
        return !!entryOf(res, k);
      }
      return false;
    });
    if (!hasSet) {
      return false;
    }
  }
  return true;
}

function fieldRow(res, f, params, otherResources, otherStatusMap, env) {
  var entry = entryOf(res, f.path);
  var dm = docMode(entry);
  var m = uiMode[f.path] || dm;

  var isMap = f.type === "map";
  var mapEntries = [];
  if (isMap && res.fields) {
    var prefix = f.path + "[";
    Object.keys(res.fields).forEach(function (k) {
      if (k.indexOf(prefix) === 0 && k.endsWith("]")) {
        var keyName = k.slice(prefix.length, k.length - 1);
        mapEntries.push({ fullPath: k, key: keyName, entry: res.fields[k] });
      }
    });
    mapEntries.sort(function (a, b) { return a.key.localeCompare(b.key); });
  }

  var isReq = isFieldEffectivelyRequired(f, res);
  if (filter === "req" && !(isReq || f.branch || entry || mapEntries.length)) return "";
  if (filter === "set" && !entry && !mapEntries.length) return "";

  var wired = m === "w" && dm === "w" && !uiMode[f.path] && entry;
  var isStatusWire = wired && entry.from && entry.from.indexOf("resources.") === 0;
  var isEnvWire = wired && entry.from && entry.from.indexOf("env.") === 0;
  var h = '<div class="fld' + (dm === "w" && entry ? " wired" : "") + '" style="padding-left:' + (12 + (f.depth || 0) * 11) + 'px">' +
    '<div class="fld-h"><span class="n" title="' + esc(f.path) + '">' + esc(f.path) + '</span><span class="t">' + esc(f.type) + "</span>" +
    (isReq ? '<span class="rq">req</span>' : "") +
    modeButtons(f.path, m, false) +
    '</div>' + formatDescHtml(f.description, f.path);

  if (isMap) {
    if (m === "w") {
      if (dm === "w" && !uiMode[f.path] && entry) {
        const wireCol = isStatusWire ? "var(--wire-status)" : (isEnvWire ? "var(--shared)" : "var(--wire-xrd)");
        const bgStyle = isStatusWire ? ' style="background:var(--wire-status-soft)"' : (isEnvWire ? ' style="background:var(--shared-soft)"' : "");
        h += '<div class="bound' + (isEnvWire ? ' shared' : '') + '"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
          '<span class="src' + (isEnvWire ? ' sh' : '') + '" style="color:' + wireCol + '">' + esc(entry.from || "") + "</span>" +
          '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
        if (isReq && isOptParamWire(entry.from, params)) {
          h += '<div style="margin-top:2px"><span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span></div>';
        }
      } else {
        h += wireSelectHtml(f.path, f.type, params, otherResources, otherStatusMap, false, !!isReq, entry && entry.from, env);
      }
    } else if (m === "r") {
      h += rawEditorHtml(f.path, (dm === "r" && entry) ? entry.raw : "", false, res, params, otherResources, otherStatusMap);
    } else {
      if (entry) {
        h += '<input class="val" data-v="' + esc(f.path) + '" value="' + esc((dm === "v" && entry) ? entry.value : "") +
          '" placeholder="whole map value">';
      }
      h += '<div class="map-entries" style="margin-top:6px;display:flex;flex-direction:column;gap:4px">';
      h += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:2px">' +
        '<span style="font-size:10px;font-weight:600;color:var(--faint);text-transform:uppercase">Map Entries (' + mapEntries.length + ')</span>' +
        '<button class="btn sm" data-add-map-entry="' + esc(f.path) + '" style="font-size:10px;padding:1px 6px">+ Add key</button>' +
        '</div>';

      mapEntries.forEach(function (me) {
        var meEntry = me.entry;
        var meDm = docMode(meEntry);
        var meM = uiMode[me.fullPath] || meDm;
        var meWired = meM === "w" && meDm === "w" && !uiMode[me.fullPath] && meEntry;
        var isMeStatus = meWired && meEntry.from && meEntry.from.indexOf("resources.") === 0;
        var isMeEnv = meWired && meEntry.from && meEntry.from.indexOf("env.") === 0;

        h += '<div class="map-entry-card" style="padding:6px 8px;background:var(--surface-2);border-radius:4px;border:1px solid var(--rule)">' +
          '<div class="frow" style="margin-bottom:3px;align-items:center">' +
          '<span style="font-family:var(--mono);font-size:11px;font-weight:600;color:var(--ink);flex:1;min-width:0;overflow-wrap:anywhere;word-break:break-word" title="[' + esc(me.key) + ']">[' + esc(me.key) + ']</span>' +
          modeButtons(me.fullPath, meM, false) +
          '<button class="del" data-del-map-entry="' + esc(me.fullPath) + '" title="Delete key" style="margin-left:4px">&#215;</button>' +
          '</div>';

        if (meM === "w") {
          if (meWired) {
            var wireCol = isMeStatus ? "var(--wire-status)" : (isMeEnv ? "var(--shared)" : "var(--wire-xrd)");
            var bgStyle = isMeStatus ? ' style="background:var(--wire-status-soft)"' : (isMeEnv ? ' style="background:var(--shared-soft)"' : "");
            h += '<div class="bound' + (isMeEnv ? ' shared' : '') + '"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
              '<span class="src' + (isMeEnv ? ' sh' : '') + '" style="color:' + wireCol + '">' + esc(meEntry.from || "") + "</span>" +
              '<span class="x" role="button" tabindex="0" data-unwire="' + esc(me.fullPath) + '" title="Remove wire">&#215;</span></div>';
          } else {
            h += wireSelectHtml(me.fullPath, "string", params, otherResources, otherStatusMap, false, false, meEntry && meEntry.from, env);
          }
        } else if (meM === "r") {
          h += rawEditorHtml(me.fullPath, (meDm === "r" && meEntry) ? meEntry.raw : "", false, res, params, otherResources, otherStatusMap);
        } else {
          h += '<input class="val" data-v="' + esc(me.fullPath) + '" value="' + esc((meDm === "v" && meEntry) ? meEntry.value : "") +
            '" placeholder="value">';
        }
        h += '</div>';
      });

      if (pendingNewMapEntry === f.path) {
        h += '<div class="frow" style="margin-top:6px;margin-bottom:0;gap:4px">' +
          '<input class="tin" data-new-map-key="' + esc(f.path) + '" placeholder="Key (e.g. Team)" autofocus aria-label="Key name" style="flex:1">' +
          '<input class="tin" data-new-map-val="' + esc(f.path) + '" placeholder="Value" aria-label="Initial value" style="flex:1">' +
          '<button class="btn sm pri" data-new-map-ok="' + esc(f.path) + '">Add</button>' +
          '<button class="del" data-new-map-cancel="' + esc(f.path) + '" title="Cancel">&#215;</button></div>';
      }
      h += '</div>';
    }
  } else {
    if (m === "w") {
      if (dm === "w" && !uiMode[f.path] && entry) {
        const wireCol = isStatusWire ? "var(--wire-status)" : (isEnvWire ? "var(--shared)" : "var(--wire-xrd)");
        const bgStyle = isStatusWire ? ' style="background:var(--wire-status-soft)"' : (isEnvWire ? ' style="background:var(--shared-soft)"' : "");
        h += '<div class="bound' + (isEnvWire ? ' shared' : '') + '"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
          '<span class="src' + (isEnvWire ? ' sh' : '') + '" style="color:' + wireCol + '">' + esc(entry.from || "") + "</span>" +
          '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
        if (isReq && isOptParamWire(entry.from, params)) {
          h += '<div style="margin-top:2px"><span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span></div>';
        }
      } else {
        h += wireSelectHtml(f.path, f.type, params, otherResources, otherStatusMap, false, !!isReq, entry && entry.from, env);
      }
    } else if (m === "r") {
      h += rawEditorHtml(f.path, (dm === "r" && entry) ? entry.raw : "", false, res, params, otherResources, otherStatusMap);
    } else if (f.type === "boolean") {
      var bVal = (dm === "v" && entry && entry.value !== undefined && entry.value !== null) ? String(entry.value).toLowerCase() : "";
      h += '<select class="val tsel" data-v="' + esc(f.path) + '">' +
        '<option value=""' + (bVal === "" ? " selected" : "") + '>unset &#8212; omitted from output</option>' +
        '<option value="true"' + (bVal === "true" ? " selected" : "") + '>true</option>' +
        '<option value="false"' + (bVal === "false" ? " selected" : "") + '>false</option>' +
        '</select>';
    } else {
      h += '<input class="val" data-v="' + esc(f.path) + '" value="' + esc((dm === "v" && entry) ? entry.value : "") +
        '" placeholder="' + (isReq ? "required &#8212; set a value or wire it" : "unset &#8212; omitted from output") + '">';
    }
  }
  return h + "</div>";
}

function envelopeFieldRow(res, f, params, otherResources, otherStatusMap, env) {
  var entry = envelopeEntryOf(res, f.path);
  var dm = docMode(entry, true);
  var mKey = "env:" + f.path;
  var m = uiMode[mKey] || dm;

  if (filter === "set" && !entry) return "";

  var wired = m === "w" && dm === "w" && !uiMode[mKey] && entry;
  var isStatusWire = wired && entry.from && entry.from.indexOf("resources.") === 0;
  var isEnvWire = wired && entry.from && entry.from.indexOf("env.") === 0;
  var isAuto = !entry && (f.path === "providerConfigRef.name" || f.path === "providerConfigRef.kind");
  var showReq = f.required && !isAuto;
  var isXr = entry && !entry.from && entry.raw === "{{ $xr }}";

  var h = '<div class="fld' + (dm === "w" && entry ? " wired" : "") + (isAuto ? " auto-defaulted" : "") + '" style="padding-left:' + (12 + (f.depth || 0) * 11) + 'px">' +
    '<div class="fld-h"><span class="n" title="' + esc(f.path) + '">' + esc(f.path) + '</span><span class="t">' + esc(f.type) + "</span>" +
    (showReq ? '<span class="rq">req</span>' : (isAuto ? '<span class="pill" style="font-size:9.5px;background:var(--wire-ref-soft);color:var(--wire-ref);padding:1px 4px;margin-left:2px" title="Filled automatically from providerName">auto</span>' : "")) +
    modeButtons(f.path, m, true) +
    '</div>' + formatDescHtml(f.description, f.path);

  if (m === "w") {
    if (dm === "w" && !uiMode[mKey] && entry) {
      var wireCol = isStatusWire ? "var(--wire-status)" : (isEnvWire ? "var(--shared)" : "var(--wire-xrd)");
      var bgStyle = isStatusWire ? ' style="background:var(--wire-status-soft)"' : (isEnvWire ? ' style="background:var(--shared-soft)"' : "");
      var wireLabel = isXr ? "XR name ($xr)" : (entry.from || "");
      h += '<div class="bound' + (isEnvWire ? ' shared' : '') + '"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
        '<span class="src' + (isEnvWire ? ' sh' : '') + '" style="color:' + wireCol + '">' + esc(wireLabel) + "</span>" +
        '<span class="x" role="button" tabindex="0" data-env-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
      if (showReq && !isXr && isOptParamWire(entry.from, params)) {
        h += '<div style="margin-top:2px"><span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span></div>';
      }
    } else {
      h += wireSelectHtml(f.path, f.type, params, otherResources, otherStatusMap, true, !!showReq, isXr ? "$xr" : (entry && entry.from), env);
    }
  } else if (m === "r") {
    h += rawEditorHtml(f.path, entry ? entry.raw : "", true, res, params, otherResources, otherStatusMap);
  } else if (f.type === "boolean") {
    var ebVal = (dm === "v" && entry && entry.value !== undefined && entry.value !== null) ? String(entry.value).toLowerCase() : "";
    h += '<select class="val tsel" data-env-v="' + esc(f.path) + '">' +
      '<option value=""' + (ebVal === "" ? " selected" : "") + '>unset &#8212; omitted from envelope</option>' +
      '<option value="true"' + (ebVal === "true" ? " selected" : "") + '>true</option>' +
      '<option value="false"' + (ebVal === "false" ? " selected" : "") + '>false</option>' +
      '</select>';
  } else {
    var ph = f.path === "providerConfigRef.name"
      ? "auto: ClusterProviderConfig / $spec.providerName"
      : (f.path === "providerConfigRef.kind"
        ? "auto: ClusterProviderConfig / $spec.providerName"
        : (f.required ? "required &#8212; set a value or wire it" : "unset &#8212; omitted from envelope"));
    h += '<input class="val" data-env-v="' + esc(f.path) + '" value="' + esc((dm === "v" && entry) ? entry.value : "") +
      '" placeholder="' + ph + '">';
  }
  return h + "</div>";
}

function workloadPresetHtml(res, doc, _allParams, _otherResources) {
  if (!res || res.provider !== "k8s") return "";
  var kind = res.kind;
  if (kind !== "Deployment" && kind !== "StatefulSet" && kind !== "DaemonSet" && kind !== "Job" && kind !== "Service") {
    return "";
  }
  var fields = res.fields || {};

  if (kind === "Deployment" || kind === "StatefulSet" || kind === "DaemonSet") {
    var replicasF = fields["spec.replicas"];
    var replicasVal = replicasF ? (replicasF.value || (replicasF.raw || "")) : "1";
    var imgF = fields["spec.template.spec.containers[0].image"];
    var imgVal = imgF ? (imgF.value || (imgF.from ? "← " + imgF.from : (imgF.raw || ""))) : "";
    var nameF = fields["spec.template.spec.containers[0].name"];
    var nameVal = nameF ? (nameF.value || nameF.raw || "") : res.name;
    var portF = fields["spec.template.spec.containers[0].ports[0].containerPort"];
    var portVal = portF ? (portF.value || portF.raw || "") : "";

    function extractApp(f) {
      if (!f) return "";
      if (f.value) return f.value;
      if (f.raw) {
        try {
          var parsed = JSON.parse(f.raw);
          if (parsed && typeof parsed === "object" && parsed.app) return parsed.app;
        } catch (_) {}
        var m = /app[:=]\s*["']?([a-zA-Z0-9_-]+)["']?/.exec(f.raw);
        if (m) return m[1];
        return f.raw;
      }
      return "";
    }

    const selAppF = fields["spec.selector.matchLabels"] || fields["spec.selector.matchLabels.app"] || fields["spec.selector.matchLabels[app]"];
    const tmplAppF = fields["spec.template.metadata.labels"] || fields["spec.template.metadata.labels.app"] || fields["spec.template.metadata.labels[app]"];
    const selAppVal = extractApp(selAppF);
    const tmplAppVal = extractApp(tmplAppF);
    const appLabel = selAppVal || tmplAppVal || res.name;
    const isSynced = selAppVal && tmplAppVal && selAppVal === tmplAppVal;

    const h = '<div class="insp-sec workload-card" style="margin:10px 0;padding:10px 12px;border:1px solid var(--wire-xrd);background:var(--surface-2);border-radius:6px">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:6px">' +
      '<span style="font-size:11px;font-weight:600;color:var(--wire-xrd);text-transform:uppercase;letter-spacing:0.5px">Workload Selectors &amp; Pod Spec</span>' +
      (isSynced ? '<span class="chip-ok" style="font-size:10px">Selectors Aligned</span>' : '<span style="color:var(--warn);font-size:10px;font-weight:600">Sync Required</span>') +
      '</div>' +
      '<div style="font-size:11px;color:var(--faint);margin-bottom:8px">Ensures <code>spec.selector.matchLabels</code> matches <code>spec.template.metadata.labels</code>:</div>' +
      '<div class="frow" style="margin-bottom:6px;align-items:center">' +
      '<span class="lbl" style="width:75px;font-size:10px">App Selector</span>' +
      '<input class="tin" data-wl-app="' + esc(res.name) + '" value="' + esc(appLabel) + '" placeholder="e.g. ' + esc(res.name) + '" style="flex:1" title="Sets both spec.selector.matchLabels and spec.template.metadata.labels">' +
      '<button class="btn sm pri" data-wl-sync-app="' + esc(res.name) + '" title="Sync App Label across Selector and Template">Sync</button>' +
      '</div>' +
      (kind === "DaemonSet" ? "" :
      '<div class="frow" style="margin-bottom:6px;align-items:center">' +
      '<span class="lbl" style="width:75px;font-size:10px">Replicas</span>' +
      '<input class="tin" type="number" min="1" max="100" data-wl-replicas="' + esc(res.name) + '" value="' + esc(replicasVal) + '" placeholder="1" style="width:60px">' +
      '<span class="dg" style="margin-left:8px;font-size:10.5px">spec.replicas</span>' +
      '</div>') +
      '<div class="frow" style="margin-bottom:6px;align-items:center">' +
      '<span class="lbl" style="width:75px;font-size:10px">Image</span>' +
      '<input class="tin" data-wl-image="' + esc(res.name) + '" value="' + esc(imgVal) + '" placeholder="nginx:alpine or repo/image:tag" style="flex:1">' +
      '</div>' +
      '<div class="frow" style="margin-bottom:2px;gap:6px">' +
      '<div style="flex:1"><span class="lbl" style="display:block;font-size:9.5px;margin-bottom:2px">Container Name</span>' +
      '<input class="tin" data-wl-cname="' + esc(res.name) + '" value="' + esc(nameVal) + '" placeholder="' + esc(res.name) + '" style="width:100%"></div>' +
      '<div style="width:75px"><span class="lbl" style="display:block;font-size:9.5px;margin-bottom:2px">Port</span>' +
      '<input class="tin" type="number" data-wl-cport="' + esc(res.name) + '" value="' + esc(portVal) + '" placeholder="8080" style="width:100%"></div>' +
      '</div></div>';
    return h;
  }

  if (kind === "Service") {
    const selAppF = fields["spec.selector"] || fields["spec.selector.app"] || fields["spec.selector[app]"];
    let selAppVal = "";
    if (selAppF) {
      if (selAppF.value) selAppVal = selAppF.value;
      else if (selAppF.raw) {
        try {
          var parsed = JSON.parse(selAppF.raw);
          if (parsed && typeof parsed === "object" && parsed.app) selAppVal = parsed.app;
        } catch (_) {}
        if (!selAppVal) {
          var sm = /app[:=]\s*["']?([a-zA-Z0-9_-]+)["']?/.exec(selAppF.raw);
          selAppVal = sm ? sm[1] : selAppF.raw;
        }
      }
    }
    const portF = fields["spec.ports[0].port"];
    const portVal = portF ? (portF.value || portF.raw || "") : "80";
    const tgtPortF = fields["spec.ports[0].targetPort"];
    const tgtPortVal = tgtPortF ? (tgtPortF.value || tgtPortF.raw || "") : "80";
    const svcTypeF = fields["spec.type"];
    const svcTypeVal = svcTypeF ? (svcTypeF.value || "") : "ClusterIP";

    const candidateWorkloads = (doc.spec && doc.spec.resources || []).filter(function (r) {
      return r.name !== res.name && (r.kind === "Deployment" || r.kind === "StatefulSet" || r.kind === "DaemonSet");
    });

    let h = '<div class="insp-sec service-card" style="margin:10px 0;padding:10px 12px;border:1px solid var(--wire-status);background:var(--surface-2);border-radius:6px">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:6px">' +
      '<span style="font-size:11px;font-weight:600;color:var(--wire-status);text-transform:uppercase;letter-spacing:0.5px">Service Selectors &amp; Ports</span>' +
      (selAppVal ? '<span class="chip-ok" style="font-size:10px">Target: ' + esc(selAppVal) + '</span>' : '<span style="color:var(--warn);font-size:10px;font-weight:600">Unset Selector</span>') +
      '</div>' +
      '<div style="font-size:11px;color:var(--faint);margin-bottom:8px">Routes traffic to pods matching <code>spec.selector</code>:</div>' +
      '<div class="frow" style="margin-bottom:6px;align-items:center">' +
      '<span class="lbl" style="width:75px;font-size:10px">Target Pod App</span>' +
      '<input class="tin" data-svc-app="' + esc(res.name) + '" value="' + esc(selAppVal) + '" placeholder="app label" style="flex:1">' +
      '</div>';

    if (candidateWorkloads.length > 0) {
      h += '<div style="margin-bottom:8px;display:flex;gap:4px;flex-wrap:wrap;align-items:center">' +
        '<span class="dg" style="font-size:10px">Quick match:</span>';
      candidateWorkloads.forEach(function (cw) {
        var cwFields = cw.fields || {};
        var cwMatchF = cwFields["spec.selector.matchLabels"] || cwFields["spec.template.metadata.labels"];
        var cwApp = "";
        if (cwMatchF && cwMatchF.raw) {
          try {
            var parsed = JSON.parse(cwMatchF.raw);
            if (parsed && typeof parsed === "object" && parsed.app) cwApp = parsed.app;
          } catch (_) {}
          if (!cwApp) {
            var cwm = /app[:=]\s*["']?([a-zA-Z0-9_-]+)["']?/.exec(cwMatchF.raw);
            cwApp = cwm ? cwm[1] : cw.name;
          }
        } else {
          cwApp = (cwFields["spec.selector.matchLabels.app"] && cwFields["spec.selector.matchLabels.app"].value) || cw.name;
        }
        h += '<button class="btn sm" data-svc-match-wl="' + esc(res.name) + '" data-match-app="' + esc(cwApp) + '" style="font-size:10px;padding:1px 6px">' + esc(cw.name) + ' (' + esc(cwApp) + ')</button>';
      });
      h += '</div>';
    }

    h += '<div class="frow" style="margin-bottom:2px;gap:6px">' +
      '<div style="width:70px"><span class="lbl" style="display:block;font-size:9.5px;margin-bottom:2px">Port</span>' +
      '<input class="tin" type="number" data-svc-port="' + esc(res.name) + '" value="' + esc(portVal) + '" placeholder="80" style="width:100%"></div>' +
      '<div style="width:75px"><span class="lbl" style="display:block;font-size:9.5px;margin-bottom:2px">Target Port</span>' +
      '<input class="tin" type="number" data-svc-tgtport="' + esc(res.name) + '" value="' + esc(tgtPortVal) + '" placeholder="80" style="width:100%"></div>' +
      '<div style="flex:1"><span class="lbl" style="display:block;font-size:9.5px;margin-bottom:2px">Type</span>' +
      '<select class="tsel" data-svc-type="' + esc(res.name) + '" style="width:100%">' +
      ['ClusterIP', 'NodePort', 'LoadBalancer'].map(function (st) {
        return '<option value="' + st + '"' + (svcTypeVal === st ? ' selected' : '') + '>' + st + '</option>';
      }).join('') +
      '</select></div>' +
      '</div></div>';
    return h;
  }
  return "";
}

function metadataConventionsHtml(res) {
  var h = '<div class="insp-sec" style="margin-top:14px;padding:8px 12px;border-top:1px solid var(--rule);background:var(--surface)">' +
    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px">' +
    '<span style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px">Naming &amp; Metadata Presets</span>' +
    '</div>' +
    '<div style="font-size:11px;color:var(--faint);margin-bottom:6px">Apply standard metadata, labels, and naming:</div>' +
    '<div style="display:flex;gap:4px;flex-wrap:wrap;margin-bottom:4px">' +
    '<button class="btn sm" data-apply-std-labels="' + esc(res.name) + '" title="Add app.kubernetes.io/name, instance, and managed-by labels">+ Standard Labels</button>' +
    '<button class="btn sm" data-apply-ext-name="' + esc(res.name) + '" title="Add crossplane.io/external-name annotation">+ External Name</button>' +
    '</div></div>';
  return h;
}

async function renderResource(res) {
  var t = renderToken;
  var doc = store.state.doc;
  var meta = null, flds = null, detail = null, loadErr = null;
  try {
    meta = await kindMeta(res);
    if (meta) {
      flds = await fieldsFor(meta.apiVersion, res.kind);
      detail = await kindDetail(meta.apiVersion, res.kind);
    }
  } catch (e) {
    loadErr = e && e.message || String(e);
  }
  if (t !== renderToken) return;

  if (annDraftRes !== res.name) {
    annDraftKey = "";
    annDraftRes = res.name;
  }

  var otherResources = (doc && doc.spec && doc.spec.resources || []).filter(function (r) {
    return r.name !== res.name && !r.forEach;
  });

  // Prefetch other resources' kind status schemas in parallel
  var otherStatusMap = {};
  await Promise.all(otherResources.map(async function (or) {
    try {
      var om = await kindMeta(or);
      if (om) {
        var od = await kindDetail(om.apiVersion, or.kind);
        if (od && od.status) otherStatusMap[or.name] = od.status;
      }
    } catch (_) {}
  }));
  if (t !== renderToken) return;

  var h = warnHtml();
  var fields = flds && flds.fields || [];
  var branchCount = (flds && flds.requiredBranches || []).length;
  var reqCount = fields.filter(function (f) { return isFieldEffectivelyRequired(f, res); }).length + branchCount;
  h += '<div class="insp-t"><div class="k">' + esc(res.kind) +
    ' <span style="color:var(--faint);font-weight:400">' + esc(res.name) + "</span></div>" +
    '<div class="g">' + esc(meta ? meta.apiVersion : res.provider) +
    (flds ? " &#183; " + flds.total + " leaf fields &#183; " + reqCount + " required" : "") +
    "</div></div>";

  if (res.kind === "Secret") {
    h += '<div class="g" style="margin:4px 12px 6px;padding:6px 8px;background:var(--surface-2);border:1px solid var(--rule);border-radius:4px;font-size:11px;line-height:1.4">' +
      '<strong style="color:var(--ink)">Secret data vs stringData:</strong><br>' +
      'Use <code style="color:var(--ink);background:var(--sunk);padding:0 3px;border-radius:2px">stringData</code> for unencoded plaintext. Values wired to <code style="color:var(--ink);background:var(--sunk);padding:0 3px;border-radius:2px">data</code> will be automatically base64-encoded.' +
      '</div>';
  }

  // for-each: repeat this resource N times, N from an integer parameter
  var allParams = paramsOf(doc);
  var intParams = Object.keys(allParams).filter(function (n) { return allParams[n].type === "integer"; });
  h += '<div class="fld"><div class="frow" style="margin-bottom:0">' +
    '<span class="lbl" style="flex:0 0 auto">for each</span>' +
    '<select class="tsel" data-foreach="' + esc(res.name) + '" style="flex:1" ' +
    'title="Repeat this resource N times \u2014 N comes from an integer parameter">' +
    '<option value=""' + (!res.forEach ? " selected" : "") + ">\u2014 no loop \u2014</option>" +
    intParams.map(function (n) {
      var v = "params." + n;
      return '<option value="' + esc(v) + '"' + (res.forEach === v ? " selected" : "") + ">" + esc(v) + "</option>";
    }).join("") +
    Object.keys(otherStatusMap).sort().map(function (rn) {
      // observed counts: integer/number status leaves of unlooped siblings —
      // zero instances until the source reports (engine semantics)
      return (otherStatusMap[rn] || []).filter(function (sf) {
        return sf.type === "integer" || sf.type === "number";
      }).map(function (sf) {
        var v = "resources." + rn + ".status." + sf.path;
        return '<option value="' + esc(v) + '"' + (res.forEach === v ? " selected" : "") + ">" +
          esc(rn) + ".status." + esc(sf.path) + "</option>";
      }).join("");
    }).join("") + "</select></div>" +
    (intParams.length ? "" : '<div class="g" style="padding:2px 0 0">declare an integer parameter to enable looping</div>') +
    "</div>";

  // when: conditional resource — builder for the engine's exact grammar:
  // bare boolean param, or  params.x == "literal" / != "literal"
  var w = parseWhen(res.when);
  var condParams = Object.keys(allParams).filter(function (n) {
    var t = allParams[n].type;
    return t === "boolean" || t === "string";
  });
  h += '<div class="fld"><div class="frow" style="margin-bottom:0">' +
    '<span class="lbl" style="flex:0 0 auto">when</span>' +
    '<select class="tsel" data-when-param="' + esc(res.name) + '" style="flex:1" ' +
    'title="Compose this resource only when the condition holds">' +
    '<option value=""' + (!w.param ? " selected" : "") + ">\u2014 always \u2014</option>" +
    condParams.map(function (n) {
      return '<option value="' + esc(n) + '"' + (w.param === n ? " selected" : "") + ">params." + esc(n) + "</option>";
    }).join("") + "</select>";
  if (w.param && allParams[w.param] && allParams[w.param].type === "string") {
    var vals = allParams[w.param].enum || [];
    h += '<select class="tsel" data-when-op="' + esc(res.name) + '" style="flex:0 0 auto">' +
      ["==", "!="].map(function (o) {
        return '<option value="' + o + '"' + (w.op === o ? " selected" : "") + ">" + o + "</option>";
      }).join("") + "</select>";
    h += vals.length
      ? '<select class="tsel" data-when-val="' + esc(res.name) + '" style="flex:1">' +
        vals.map(function (v) {
          return '<option value="' + esc(v) + '"' + (w.val === v ? " selected" : "") + ">" + esc(v) + "</option>";
        }).join("") + "</select>"
      : '<input class="tin" data-when-val="' + esc(res.name) + '" style="flex:1" value="' + esc(w.val || "") + '" placeholder="value">';
  }
  h += "</div></div>";

  if (loadErr) {
    h += '<div class="warnbar">' + esc(loadErr) + "</div>";
  } else if (!meta) {
    var provRef = res.provider || "<provider>";
    h += '<div class="empty">No schema found for kind ' + esc(res.kind) +
      '.<div class="dg" style="margin-top:4px">Run: <code>cf provider add ' + esc(provRef) + '</code></div></div>';
  } else {
    var params = paramsOf(doc);
    var env = (doc && doc.spec && doc.spec.environment) || {};
    // Required branches (e.g. Deployment's spec.selector / spec.template):
    // must-set objects with no chain-true leaves — surfaced as rows of their
    // own so the Required view shows what a user actually has to fill.
    var branches = (flds && flds.requiredBranches || []);
    var branchRows = (filter === "req" || filter === "all")
      ? branches.map(function (b) {
          return '<div class="fld"><div class="fld-h">' +
            '<span class="n" title="' + esc(b.path) + '">' + esc(b.path) + '</span>' +
            '<span class="t">' + esc(b.type || "object") + '</span>' +
            '<span class="rq">req</span></div>' +
            '<div class="fld-d">required object \u2014 set its member fields (expand via All / search)</div></div>';
        }).join("")
      : "";
    var wlHtml = workloadPresetHtml(res, doc, params, otherResources);
    h += wlHtml;

    var body = branchRows +
      fields.map(function (f) { return fieldRow(res, f, params, otherResources, otherStatusMap, env); }).join("");
    h += body || '<div class="empty">No fields match this filter.</div>';

    // Crossplane Envelope section (if this CRD defines envelope properties)
    if (detail && detail.envelope && detail.envelope.length > 0) {
      var envRows = detail.envelope.map(function (f) {
        return envelopeFieldRow(res, f, params, otherResources, otherStatusMap, env);
      }).join("");
      if (envRows) {
        var envSetCount = detail.envelope.filter(function (f) { return envelopeEntryOf(res, f.path); }).length;
        h += '<div class="insp-sec" style="margin-top:14px;padding:8px 12px;border-top:1px solid var(--rule);background:var(--surface)">' +
          '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px">' +
          '<span style="font-size:11px;font-weight:600;color:var(--wire-ref);text-transform:uppercase;letter-spacing:0.5px">Crossplane Envelope</span>' +
          (envSetCount > 0 ? '<span class="pill" style="background:var(--wire-ref-soft);color:var(--wire-ref);font-size:10px">' + envSetCount + ' configured</span>' : "") +
          '</div>' +
          '<div style="font-size:11px;color:var(--faint);margin-bottom:8px">Secrets, policies and metadata outside forProvider:</div>' +
          envRows +
          '</div>';
      }
    }

    // Conventions & Metadata Presets section
    h += metadataConventionsHtml(res);

    // Annotations section: authored metadata entries with the same forms
    var anns = res.annotations || {};
    var annKeys = Object.keys(anns).sort();
    h += '<div class="insp-sec" style="margin-top:14px;padding:8px 12px;border-top:1px solid var(--rule);background:var(--surface)">' +
      '<div style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;margin-bottom:4px">Annotations</div>' +
      annKeys.map(function (k) {
        var f = anns[k];
        var val = f.from ? "\u2190 " + f.from : (f.raw ? "raw" : f.value);
        return '<div class="frow" style="margin-bottom:2px">' +
          '<span class="ann-key" title="' + esc(k) + '">' + esc(k) + '</span>' +
          '<span class="ann-val dg" title="' + esc(val) + '">' + esc(val) + '</span>' +
          '<button class="del" data-ann-del="' + esc(k) + '" title="Remove annotation">\u00d7</button></div>';
      }).join("") +
      '<div class="frow" style="margin-top:4px;margin-bottom:0">' +
      '<input class="tin" data-ann-key placeholder="prefix/name" value="' + esc(annDraftKey || "") + '" style="flex:1;min-width:0">' +
      '<input class="tin" data-ann-value placeholder="value (or placeholder to wire)" style="flex:1;min-width:0">' +
      '<button class="btn sm" data-ann-add>Add</button></div></div>';

    // Status outputs section
    if (detail && detail.status && detail.status.length > 0) {
      h += '<div class="insp-sec" style="margin-top:14px;padding:8px 12px;border-top:1px solid var(--rule);background:var(--surface-2)">' +
        '<div style="font-size:11px;font-weight:600;color:var(--wire-status);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:4px">Status Outputs</div>' +
        '<div style="font-size:11px;color:var(--faint);margin-bottom:6px">Other resources can wire from this object\'s status:</div>' +
        detail.status.slice(0, 10).map(function (sf) {
          return '<div style="display:flex;align-items:center;justify-content:space-between;padding:2px 0;font-size:11px">' +
            '<code style="color:var(--wire-status);font-family:var(--mono)">status.' + esc(sf.path) + '</code>' +
            '<span style="color:var(--faint)">' + esc(sf.type) + '</span>' +
            '</div>';
        }).join("") +
        '</div>';
    }
  }
  var __snap = snapshotFocusedEdit();
  box.innerHTML = h;
  restoreFocusedEdit(__snap);
  updateAllPreviews();
}

/* ---------------- rendering: XRD ---------------- */

async function renderXRD() {
  var myToken = renderToken;
  var doc = store.state.doc;
  var xrd = doc.spec && doc.spec.xrd || {};
  var params = paramsOf(doc);
  var currentKeys = Object.keys(params);

  var curDocName = doc && doc.metadata && doc.metadata.name;
  if (curDocName !== lastDocName) {
    paramOrder = null;
    lastDocName = curDocName;
  }

  if (!paramOrder) {
    paramOrder = currentKeys.slice().sort();
  } else {
    var missingInParams = paramOrder.filter(function (k) { return !Object.prototype.hasOwnProperty.call(params, k); });
    var missingInOrder = currentKeys.filter(function (k) { return paramOrder.indexOf(k) === -1; });
    if (missingInParams.length === 1 && missingInOrder.length === 1) {
      var idx = paramOrder.indexOf(missingInParams[0]);
      paramOrder[idx] = missingInOrder[0];
    } else {
      var kept = paramOrder.filter(function (k) { return Object.prototype.hasOwnProperty.call(params, k); });
      currentKeys.forEach(function (k) {
        if (kept.indexOf(k) === -1) kept.push(k);
      });
      paramOrder = kept;
    }
  }
  var names = paramOrder;

  var h = warnHtml();
  h += '<div class="insp-t"><div class="frow">' +
    '<input class="tin" id="xk" value="' + esc(xrd.kind) + '" aria-label="Kind">' +
    '<select class="tsel" id="xs" aria-label="Scope">' +
    '<option' + (xrd.scope === "Namespaced" ? " selected" : "") + ">Namespaced</option>" +
    '<option' + (xrd.scope === "Cluster" ? " selected" : "") + ">Cluster</option></select></div>" +
    '<div class="g">' + esc((xrd.plural || "") + "." + (xrd.group || "")) + " &#183; " + esc(xrd.version) + "</div></div>";

  var env = (doc && doc.spec && doc.spec.environment) || {};
  var envKeysCount = Object.keys(env).length;
  h += '<div class="env-summary-row" style="padding:7px 12px 6px;border-bottom:1px solid var(--rule);font-size:11px;display:flex;align-items:center;gap:6px">' +
    '<span style="color:var(--ink)">Environment: ' + envKeysCount + ' key' + (envKeysCount === 1 ? '' : 's') + ' declared</span>' +
    '<span class="sp" style="flex:1"></span>' +
    '<button class="btn sm link" data-tab-switch="shared" style="color:var(--shared);font-weight:600;text-decoration:underline;cursor:pointer;background:none;border:none;padding:0;font-size:11px">SHARED</button>' +
    '</div>' +
    '<div style="padding:7px 12px 3px"><span class="lbl">Parameters (' + names.length + ")</span></div>";

  var MEMBER_TYPES = ["string", "integer", "number", "boolean", "object"];

  // memberTreeHtml renders an object parameter's members recursively \u2014 the
  // openapi-editor shape. Every row is addressed "<param>|<dot.path>", so
  // one delegated handler edits any depth.
  function memberTreeHtml(paramName, parentPath, props, depth) {
    var out = "";
    Object.keys(props || {}).sort().forEach(function (mn) {
      var mp = props[mn];
      var path = parentPath ? parentPath + "." + mn : mn;
      var key = paramName + "|" + path;
      out += '<div class="frow" style="margin:2px 0 0 ' + (depth * 12) + 'px">' +
        '<input class="tin" data-mname="' + esc(key) + '" value="' + esc(mn) + '" aria-label="Member name" style="flex:1;min-width:0">' +
        '<select class="tsel" data-mtype="' + esc(key) + '" aria-label="Member type">' +
        MEMBER_TYPES.map(function (t) {
          return "<option" + (t === mp.type ? " selected" : "") + ">" + t + "</option>";
        }).join("") + "</select>" +
        '<label class="g" style="display:inline-flex;align-items:center;gap:3px;font-size:11px">' +
        '<input type="checkbox" data-mreq="' + esc(key) + '"' + (mp.required ? " checked" : "") + ">req</label>" +
        (mp.type === "object" ? "" :
          '<input class="tin" data-mdef="' + esc(key) + '" value="' + esc(mp.default || "") + '" placeholder="default" style="flex:0 0 64px">') +
        '<button class="del" data-mdel="' + esc(key) + '" title="Remove member">\u00d7</button></div>';
      if (mp.type === "object") {
        out += memberTreeHtml(paramName, path, mp.properties, depth + 1);
        out += '<div style="margin-left:' + ((depth + 1) * 12) + 'px;padding:2px 0">' +
          '<button class="btn sm" data-madd="' + esc(paramName + "|" + path) + '">+ member</button></div>';
      }
    });
    return out;
  }

  function paramDetailRow(n, p, locked) {
    if (p.type === "object") {
      var mh = (p.properties && Object.keys(p.properties).length ? "" :
        '<div class="g" style="padding:2px 0 2px">no members \u2192 free-form map (string values); add members for a typed schema</div>');
      mh += memberTreeHtml(n, "", p.properties, 0);
      mh += '<div style="padding:3px 0 4px;display:flex;align-items:center;gap:4px">' +
        '<button class="btn sm" data-madd="' + esc(n + "|") + '">+ member</button>' +
        '<button class="del" data-pd="' + esc(n) + '"' +
        (locked ? ' disabled title="providerName is required for managed resources in Namespaced XRD" style="cursor:not-allowed;opacity:0.5;margin-left:auto"' : ' title="Delete parameter" style="margin-left:auto"') +
        '>&#215;</button></div>';
      return mh;
    }
    var h = '<div class="frow" style="margin-bottom:0;gap:4px">';
    if (p.type === "boolean") {
      h += '<select class="tsel" data-pdef="' + esc(n) + '" aria-label="Default value" style="flex:1">' +
        '<option value=""' + (!p.default ? " selected" : "") + ">no default</option>" +
        '<option' + (p.default === "true" ? " selected" : "") + ">true</option>" +
        '<option' + (p.default === "false" ? " selected" : "") + ">false</option></select>";
    } else if (p.enum && p.enum.length) {
      h += '<span class="g" style="flex:1;font-size:11px;overflow:hidden;text-overflow:ellipsis">enum: ' + esc(p.enum.join(", ")) + "</span>";
    } else {
      h += '<input class="tin" data-pdef="' + esc(n) + '" value="' + esc(p.default || "") +
        '" placeholder="default value" aria-label="Default value" style="flex:1;min-width:60px">';
    }
    if (p.type !== "boolean") {
      h += '<input class="tin" data-pe="' + esc(n) + '" value="' + esc((p.enum || []).join(",")) +
        '" placeholder="enum,values" title="Comma-separated allowed values" aria-label="Enum values" style="flex:1;min-width:60px">';
    }
    h += '<button class="del" data-pd="' + esc(n) + '"' +
      (locked ? ' disabled title="providerName is required for managed resources in Namespaced XRD" style="cursor:not-allowed;opacity:0.5"' : ' title="Delete parameter"') +
      '>&#215;</button></div>';
    return h;
  }

  names.forEach(function (n) {
    var p = params[n] || {};
    var fo = fanOut(doc, n);
    var locked = isParamLocked(doc, n);
    h += '<div class="fld"><div class="frow" style="margin-bottom:3px;gap:4px">' +
      '<input class="tin bold" data-pn="' + esc(n) + '" value="' + esc(n) + '" aria-label="Parameter name"' +
      (locked ? ' readonly title="providerName is required for managed resources in Namespaced XRD" style="min-width:70px;flex:1 1 auto;cursor:not-allowed;opacity:0.75"' : ' style="min-width:70px;flex:1 1 auto"') + '>' +
      '<select class="tsel" data-pt="' + esc(n) + '" aria-label="Parameter type" style="flex:0 0 auto"' +
      (locked ? ' disabled title="providerName type must be string" style="cursor:not-allowed"' : '') + '>' +
      PARAM_TYPES.map(function (t) {
        return "<option" + (t === p.type ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<label class="g" style="display:inline-flex;align-items:center;gap:3px;font-size:11px;flex:0 0 auto;white-space:nowrap"' +
      (locked ? ' title="providerName is required for managed resources in Namespaced XRD"' : '') + '>' +
      '<input type="checkbox" data-pr="' + esc(n) + '"' + (p.required ? " checked" : "") +
      (locked ? ' disabled style="cursor:not-allowed"' : '') + '>req</label>' +
      '<span class="fan" title="Wired into ' + fo + ' field' + (fo === 1 ? "" : "s") + '" style="flex:0 0 auto">&#215;' + fo + "</span></div>" +
      paramDetailRow(n, p, locked) + "</div>";
  });
  h += '<div style="padding:8px 12px 14px">' +
    '<button class="btn sm pri" id="addParamBtn">+ Add parameter</button></div>';

  /* ---------- pipeline steps ---------- */
  var pipeline = doc.spec && doc.spec.pipeline || [];
  h += '<div style="padding:14px 12px 3px;border-top:1px solid var(--rule);display:flex;align-items:center">' +
    '<span class="lbl">Pipeline (' + (pipeline.length ? pipeline.length + " custom" : "default") + ')</span>' +
    '</div>' +
    '<div class="g" style="padding:2px 12px 8px;font-size:11px">Functions executed during Composition render. <code>render-resources</code> (go-templating) runs at center.</div>';

  if (!pipeline.length) {
    h += '<div style="margin:0 12px 10px;padding:8px 10px;background:var(--surface-2);border:1px solid var(--rule);border-radius:6px;font-size:11px">' +
      '<div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">' +
      '<span style="font-family:var(--mono);font-weight:600;color:var(--ink)">1. render-resources</span>' +
      '<span class="dg">go-templating (core)</span></div>' +
      '<div style="display:flex;align-items:center;gap:6px">' +
      '<span style="font-family:var(--mono);color:var(--ink)">2. auto-ready</span>' +
      '<span class="dg">inferred default</span>' +
      '<button class="btn sm" id="addAutoReadyBtn" style="margin-left:auto;font-size:10px">+ Pin step</button>' +
      '</div></div>';
  } else {
    for (var i = 0; i < pipeline.length; i++) {
      var step = pipeline[i];
      var pos = step.position || "after";
      var parsedInp = parseInputYAML(step.input || "");
      var apiVer = parsedInp.apiVersion;
      var fKind = parsedInp.kind;
      if (!apiVer || !fKind) {
        var inf = inferFnMeta(step);
        if (inf) {
          apiVer = inf.apiVersion;
          fKind = inf.kind;
        }
      }
      var flds = null;
      if (apiVer && fKind) {
        try {
          flds = await fieldsFor(apiVer, fKind);
        } catch (_) {
          flds = null;
        }
      }
      var hasSchema = flds && flds.fields && flds.fields.length > 0;
      var mode = pipeInputMode[i] || (hasSchema ? "form" : "raw");

      h += '<div class="fld" style="margin:0 12px 8px;padding:8px 10px;background:var(--surface-2);border:1px solid var(--rule);border-radius:6px">' +
        '<div class="frow" style="margin-bottom:4px">' +
        '<input class="tin bold" data-pipe-name="' + i + '" value="' + esc(step.name || "") + '" placeholder="step-name" aria-label="Step name" style="flex:1">' +
        '<select class="tsel" data-pipe-pos="' + i + '" aria-label="Position">' +
        '<option value="before"' + (pos === "before" ? " selected" : "") + '>before render</option>' +
        '<option value="after"' + (pos === "after" ? " selected" : "") + '>after render</option></select>' +
        '<button class="del" data-pipe-del="' + i + '" title="Delete pipeline step">&#215;</button></div>' +
        '<div class="frow" style="margin-bottom:4px">' +
        '<input class="tin" data-pipe-fn="' + i + '" value="' + esc(step.functionRef || "") + '" placeholder="functionRef (e.g. function-auto-ready)" aria-label="Function ref">' +
        '</div>' +
        '<div class="frow" style="margin-bottom:4px">' +
        '<input class="tin" list="catalogueFnPkgs" data-pipe-pkg="' + i + '" value="' + esc(step.package || "") + '" placeholder="xpkg.crossplane.io/... (package)" aria-label="Function package">' +
        '</div>' +
        '<div style="margin-top:4px">';

      if (hasSchema && mode === "form") {
        h += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px">' +
          '<span class="dg" style="font-size:10px;font-weight:600">Input (' + esc(fKind) + '):</span>' +
          '<button class="btn sm" data-pipe-mode="' + i + '" data-mode-to="raw" style="font-size:9px;padding:1px 5px">Raw YAML</button>' +
          '</div>';
        flds.fields.forEach(function (f) {
          var curVal = getPathVal(parsedInp, f.path);
          h += '<div style="margin-bottom:4px;padding:3px 0">' +
            '<div style="display:flex;align-items:center;justify-content:space-between;font-size:11px">' +
            '<span><code style="font-family:var(--mono);color:var(--ink)">' + esc(f.path) + '</code>' +
            (f.required ? '<span class="rq" style="margin-left:4px;font-size:9px">req</span>' : "") + '</span>' +
            '<span style="color:var(--faint);font-size:10px">' + esc(f.type) + '</span></div>' +
            (f.description ? '<div class="g" style="font-size:10px;margin:1px 0 2px">' + esc(f.description) + '</div>' : "");
          if (f.type === "boolean") {
            h += '<select class="tsel" data-pipe-fld="' + i + '" data-fld-path="' + esc(f.path) + '">' +
              '<option value=""' + (!curVal ? " selected" : "") + '>unset</option>' +
              '<option value="true"' + (curVal === "true" ? " selected" : "") + '>true</option>' +
              '<option value="false"' + (curVal === "false" ? " selected" : "") + '>false</option></select>';
          } else if (f.type === "string" && (f.path.indexOf("template") !== -1 || f.path.indexOf("script") !== -1)) {
            h += '<textarea class="tin" data-pipe-fld="' + i + '" data-fld-path="' + esc(f.path) + '" rows="2" style="font-family:var(--mono);font-size:10px;width:100%">' + esc(curVal) + '</textarea>';
          } else {
            h += '<input class="tin" data-pipe-fld="' + i + '" data-fld-path="' + esc(f.path) + '" value="' + esc(curVal) + '" placeholder="' + esc(f.type) + '">';
          }
          h += '</div>';
        });
      } else {
        h += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:2px">' +
          '<span class="dg" style="font-size:10px">Input YAML' + (hasSchema ? "" : " (uncached)") + ':</span>' +
          (hasSchema ? '<button class="btn sm" data-pipe-mode="' + i + '" data-mode-to="form" style="font-size:9px;padding:1px 5px">Typed Form</button>' : "") +
          '</div>' +
          '<textarea class="tin" data-pipe-input="' + i + '" rows="3" style="font-family:var(--mono);font-size:10px;width:100%;resize:vertical" placeholder="apiVersion: ...\nkind: ...">' + esc(step.input || "") + '</textarea>';
      }

      h += '</div></div>';
    }
  }

  var catFns = await getCatalogueFunctions().catch(function () { return []; });
  h += '<datalist id="catalogueFnPkgs">';
  catFns.forEach(function (cf) {
    if (cf.ref) {
      h += '<option value="' + esc(cf.ref) + '">' + esc(cf.name || "") + '</option>';
    }
  });
  h += '</datalist>';

  h += '<div style="padding:4px 12px 14px;display:flex;gap:6px;flex-wrap:wrap">' +
    '<select class="tsel" id="pipePresetSelect" style="flex:1">' +
    '<option value="auto-ready">+ function-auto-ready</option>' +
    '<option value="environment-configs">+ function-environment-configs</option>' +
    '<option value="cel-filter">+ function-cel-filter</option>' +
    '<option value="extra-resources">+ function-extra-resources</option>' +
    '<option value="custom">+ Custom function step</option>' +
    '</select>' +
    '<button class="btn sm pri" id="addPipeStepBtn">+ Add step</button>' +
    '</div>';

  if (myToken !== renderToken) return;
  var __snap = snapshotFocusedEdit();
  box.innerHTML = h;
  restoreFocusedEdit(__snap);
  pendingRenamedParam = null;
  if (pendingFocusParam) {
    var pInp = box.querySelector('input[data-pn="' + CSS.escape(pendingFocusParam) + '"]');
    if (pInp) {
      pInp.focus();
      if (pInp.select) pInp.select();
      pendingFocusParam = null;
    }
  }
}

/* ---------------- render dispatch ---------------- */

/**
 * Re-renders must never discard an in-progress edit: snapshot the focused
 * control before innerHTML replacement and restore its identity, value,
 * caret and focus afterwards (the palette's preserver pattern). Deferring
 * renders instead proved too blunt — error warnbars must paint DURING
 * editing.
 */
var lastIntentAt = 0; // pointer/keydown after a snapshot means the user moved on
document.addEventListener("pointerdown", function () { lastIntentAt = Date.now(); }, true);

function snapshotFocusedEdit() {
  var ae = document.activeElement;
  if (!ae || !box || !box.contains(ae)) return null;
  if (ae.tagName !== "INPUT" && ae.tagName !== "TEXTAREA" && ae.tagName !== "SELECT") return null;
  var key = null;
  for (var i = 0; i < ae.attributes.length; i++) {
    var a = ae.attributes[i];
    if (a.name.indexOf("data-") === 0) { key = '[' + a.name + '="' + CSS.escape(a.value) + '"]'; break; }
  }
  if (!key) return null;
  return {
    sel: ae.tagName.toLowerCase() + key,
    value: ae.value,
    checked: ae.checked,
    selStart: ae.selectionStart, selEnd: ae.selectionEnd,
    at: Date.now(),
  };
}

function restoreFocusedEdit(snap) {
  if (!snap) return;
  var el = box.querySelector(snap.sel);
  if (!el && pendingRenamedParam) {
    var oldEsc = CSS.escape(pendingRenamedParam.from);
    var newEsc = CSS.escape(pendingRenamedParam.to);
    if (snap.sel.indexOf(oldEsc) !== -1) {
      var translatedSel = snap.sel.split(oldEsc).join(newEsc);
      el = box.querySelector(translatedSel);
    }
  }
  if (!el && pendingRenamedEnvKey) {
    var oldEscEnv = CSS.escape(pendingRenamedEnvKey.from);
    var newEscEnv = CSS.escape(pendingRenamedEnvKey.to);
    if (snap.sel.indexOf(oldEscEnv) !== -1) {
      var translatedSelEnv = snap.sel.split(oldEscEnv).join(newEscEnv);
      el = box.querySelector(translatedSelEnv);
    }
  }
  if (!el) return;
  if (el.type === "checkbox") el.checked = snap.checked;
  else el.value = snap.value;
  // Refocus only when the replacement itself killed focus — a pointerdown
  // since the snapshot means the user deliberately moved on; stealing focus
  // back would eat their click's consequences.
  if (lastIntentAt <= snap.at) {
    el.focus();
    try {
      if (snap.selStart !== null && el.setSelectionRange) el.setSelectionRange(snap.selStart, snap.selEnd);
    } catch (_) { /* selects don't */ }
  }
}

/* ---------------- EnvironmentConfig inspector ---------------- */

function getEnvConfigStep(doc) {
  var steps = (doc && doc.spec && doc.spec.pipeline) || [];
  for (var i = 0; i < steps.length; i++) {
    if (steps[i].functionRef === "function-environment-configs" || steps[i].name === "environment-configs") {
      return steps[i];
    }
  }
  return null;
}

function parseEnvSelection(doc) {
  var step = getEnvConfigStep(doc);
  if (!step || !step.input) {
    return { mode: "Reference", name: "default", labels: "" };
  }
  var input = step.input;
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

function updateEnvSelection(mode, name, labels) {
  return op(function () {
    return store.replaceDoc(function (d) {
      d.spec = d.spec || {};
      d.spec.pipeline = d.spec.pipeline || [];
      var step = null;
      for (var i = 0; i < d.spec.pipeline.length; i++) {
        if (d.spec.pipeline[i].functionRef === "function-environment-configs" || d.spec.pipeline[i].name === "environment-configs") {
          step = d.spec.pipeline[i];
          break;
        }
      }
      if (!step) {
        step = {
          name: "environment-configs",
          functionRef: "function-environment-configs",
          package: "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0",
          position: "before"
        };
        d.spec.pipeline.unshift(step);
      }
      if (mode === "Selector") {
        var labelsObj = {};
        if (typeof labels === "string" && labels.trim()) {
          labels.split(",").forEach(function (pair) {
            var parts = pair.split("=");
            if (parts.length === 2 && parts[0].trim()) {
              labelsObj[parts[0].trim()] = parts[1].trim();
            }
          });
        }
        var lblKeys = Object.keys(labelsObj);
        var lblLines = lblKeys.map(function (k) {
          return "        " + k + ": " + labelsObj[k];
        }).join("\n");
        step.input = "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Selector\n    selector:\n      matchLabels:\n" + (lblLines ? lblLines + "\n" : "        environment: default\n");
      } else {
        step.input = "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Reference\n    ref:\n      name: " + (name.trim() || "default") + "\n";
      }
    });
  }, "unable to update environment selection");
}

function setEnvKeyField(keyName, field, value) {
  return op(function () {
    return store.replaceDoc(function (d) {
      d.spec = d.spec || {};
      d.spec.environment = d.spec.environment || {};
      var k = d.spec.environment[keyName] || { type: "string" };
      if (field === "type") {
        k.type = value;
      } else if (field === "required") {
        k.required = !!value;
      } else if (field === "value") {
        k.default = value;
        delete k.value;
      }
      d.spec.environment[keyName] = k;
    });
  }, "unable to update environment key");
}

function deleteEnvKey(keyName) {
  var doc = store.state.doc;
  var wires = findEnvWires(doc, keyName);
  if (wires.length > 0) {
    var prompt = 'Environment key "' + keyName + '" is wired into ' + wires.length + " field" + (wires.length === 1 ? "" : "s") + ". Delete it and unwire all referencing fields?";
    if (!window.confirm(prompt)) {
      var msg = 'delete environment key "' + keyName + '": still referenced by wire ' + wires.join(", ");
      warnMsg = msg;
      store.emit("error", { message: msg });
      render();
      return;
    }
  }
  return op(function () {
    return store.replaceDoc(function (d) {
      deleteEnvKeyFromDoc(d, keyName);
    });
  }, "unable to delete environment key");
}

function addEnvKey(keyName, keyObj) {
  return op(function () {
    return store.replaceDoc(function (d) {
      d.spec = d.spec || {};
      d.spec.environment = d.spec.environment || {};
      d.spec.environment[keyName] = keyObj || { type: "string" };
    });
  }, "unable to add environment key");
}

function renameEnvKey(oldKey, newKey, inputEl) {
  var doc = store.state.doc;
  var env = (doc && doc.spec && doc.spec.environment) || {};

  var validNameRE = /^[a-zA-Z][a-zA-Z0-9_-]*$/;
  if (!validNameRE.test(newKey)) {
    var msg = 'Invalid environment key name "' + newKey + '": must start with a letter and contain only alphanumeric characters, underscores, or hyphens';
    warnMsg = msg;
    store.emit("error", { message: msg });
    if (inputEl) inputEl.value = oldKey;
    render();
    return;
  }

  if (env[newKey] && newKey !== oldKey) {
    msg = 'Environment key "' + newKey + '" already exists';
    warnMsg = msg;
    store.emit("error", { message: msg });
    if (inputEl) inputEl.value = oldKey;
    render();
    return;
  }

  pendingRenamedEnvKey = { from: oldKey, to: newKey };
  return op(function () {
    return store.replaceDoc(function (d) {
      renameEnvKeyInDoc(d, oldKey, newKey);
    });
  }, "unable to rename environment key").finally(function () {
    pendingRenamedEnvKey = null;
  });
}

function removeWire(resName, wirePath, isEnv, isAnn) {
  return op(function () {
    return store.replaceDoc(function (d) {
      var res = (d.spec && d.spec.resources || []).find(function (r) { return r.name === resName; });
      if (!res) return;
      if (isAnn) {
        var key = wirePath.replace(/^annotations\./, "");
        if (res.annotations && res.annotations[key]) {
          delete res.annotations[key];
          if (!Object.keys(res.annotations).length) delete res.annotations;
        }
      } else if (isEnv) {
        var envPath = wirePath.replace(/^envelope\./, "");
        if (res.envelope && res.envelope[envPath]) {
          delete res.envelope[envPath];
          if (!Object.keys(res.envelope).length) delete res.envelope;
        }
      } else {
        if (res.fields && res.fields[wirePath]) {
          delete res.fields[wirePath];
        }
      }
    });
  }, "unable to remove wire");
}

function countEmptyValues(doc) {
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

function generateEnvironmentConfigYAML(doc, selInfo) {
  var env = (doc && doc.spec && doc.spec.environment) || {};
  var keys = Object.keys(env).sort();
  var name = (selInfo && selInfo.mode === "Reference" && selInfo.name) ? selInfo.name : "default";
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

async function renderEnvironment() {
  var myToken = renderToken;
  var doc = store.state.doc;
  if (!doc) return;
  var env = (doc.spec && doc.spec.environment) || {};
  var envKeys = Object.keys(env).sort();
  var selInfo = parseEnvSelection(doc);
  var configName = selInfo.mode === "Reference" ? (selInfo.name || "default") : (selInfo.labels || "selector");

  var h = warnHtml();
  h += '<div class="insp-t"><div class="k"><span style="color:var(--shared)">EnvironmentConfig</span>' +
    ' <span style="color:var(--faint);font-weight:400">' + esc(configName) + '</span></div>' +
    '<div class="g">apiextensions.crossplane.io/v1beta1</div></div>';

  // Selection section
  h += '<div style="padding:10px 12px 8px;border-bottom:1px solid var(--rule)">' +
    '<div class="lbl" style="margin-bottom:4px">Selection</div>' +
    '<div class="frow" style="gap:6px">' +
    '<select class="tsel" id="envSelMode" aria-label="Selection mode" style="flex:0 0 auto">' +
    '<option value="Reference"' + (selInfo.mode === "Reference" ? " selected" : "") + '>Reference (by name)</option>' +
    '<option value="Selector"' + (selInfo.mode === "Selector" ? " selected" : "") + '>Selector (by labels)</option>' +
    '</select>' +
    (selInfo.mode === "Selector" ?
      '<input class="tin" id="envSelLabels" data-env-selection-labels="1" value="' + esc(selInfo.labels) + '" placeholder="key=value, ..." aria-label="EnvironmentConfig matchLabels" style="flex:1">' :
      '<input class="tin" id="envSelName" data-env-selection-name="1" value="' + esc(selInfo.name) + '" placeholder="default" aria-label="EnvironmentConfig name" style="flex:1">') +
    '</div></div>';

  // Keys section
  h += '<div style="padding:10px 12px 4px"><span class="lbl">Keys (' + envKeys.length + ')</span></div>';
  var allWires = listWires(doc);
  var ENV_TYPES = ["string", "integer", "number", "boolean"];

  if (envKeys.length === 0) {
    h += '<div class="g" style="padding:8px 12px">No environment keys declared.</div>';
  } else {
    envKeys.forEach(function (k) {
      var item = env[k] || {};
      var ty = item.type || "string";
      var req = !!item.required;
      var val = item.value !== undefined && item.value !== "" ? item.value : (item.default !== undefined ? item.default : "");
      var wires = allWires.filter(function (w) { return w.kind === "env" && w.envKey === k; });

      h += '<div class="fld" data-env-key="' + esc(k) + '" style="padding:8px 12px;border-bottom:1px solid var(--rule)">' +
        '<div class="frow" style="margin-bottom:4px;gap:4px">' +
        '<input class="tin bold" data-env-name="' + esc(k) + '" value="' + esc(k) + '" style="flex:1;min-width:70px" aria-label="Key name">' +
        '<select class="tsel" data-env-type="' + esc(k) + '" aria-label="Type" style="flex:0 0 auto">' +
        ENV_TYPES.map(function (t) {
          return '<option value="' + t + '"' + (t === ty ? ' selected' : '') + '>' + t + '</option>';
        }).join('') +
        '</select>' +
        '<label class="g" style="display:inline-flex;align-items:center;gap:3px;font-size:11px;flex:0 0 auto;white-space:nowrap">' +
        '<input type="checkbox" data-env-req="' + esc(k) + '"' + (req ? ' checked' : '') + '>req</label>' +
        '<input class="tin" data-env-val="' + esc(k) + '" value="' + esc(val) + '" placeholder="value" aria-label="Value" style="flex:1;min-width:60px">' +
        '<button class="del" data-env-del-key="' + esc(k) + '" title="Delete key">&#215;</button>' +
        '</div>';

      // Used by section
      h += '<div class="env-used-by" style="font-size:11px;padding:2px 0 2px 2px">';
      if (wires.length === 0) {
        h += '<span class="g">Not used by any resource</span>';
      } else {
        h += '<div class="g" style="margin-bottom:2px;font-weight:600">Used by:</div>';
        wires.forEach(function (w) {
          var wireTarget = w.resource + "." + w.path;
          h += '<div class="env-wire-row" style="display:flex;align-items:center;justify-content:space-between;padding:2px 0">' +
            '<span class="mono" style="color:var(--shared)">' + esc(wireTarget) + '</span>' +
            '<button class="del" data-env-wire-del="1" data-wire-res="' + esc(w.resource) + '" data-wire-path="' + esc(w.path) + '"' +
            (w.isEnvelope ? ' data-wire-env="1"' : '') +
            (w.isAnnotation ? ' data-wire-ann="1"' : '') +
            ' title="Remove wire">&#215;</button></div>';
        });
      }
      h += '</div></div>';
    });
  }

  h += '<div style="padding:8px 12px 14px">' +
    '<button class="btn sm pri" id="envAddKeyBtn">+ Add key</button></div>';

  // Generated file section
  var emptyCount = countEmptyValues(doc);
  var generatedYAML = generateEnvironmentConfigYAML(doc, selInfo);

  h += '<div class="env-gen-file" style="margin:8px 12px 16px;padding:10px 12px;background:var(--surface-2);border:1px solid var(--rule);border-radius:6px">' +
    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:6px">' +
    '<span class="lbl" style="font-size:11px">Generated Manifest</span>' +
    '<span class="env-empty-count" style="font-size:11px;color:' + (emptyCount > 0 ? 'var(--warn, #e67e22)' : 'var(--ok, #27ae60)') + '">' +
    emptyCount + ' empty value' + (emptyCount === 1 ? '' : 's') + '</span>' +
    '</div>' +
    '<pre style="margin:0;padding:8px;background:var(--sunk, #18181b);color:var(--code-ink, #f4f4f5);border-radius:4px;font-family:var(--mono);font-size:10.5px;overflow-x:auto;line-height:1.4"><code>' + esc(generatedYAML) + '</code></pre>' +
    '</div>';

  if (myToken !== renderToken) return;
  var __snap = snapshotFocusedEdit();
  box.innerHTML = h;
  restoreFocusedEdit(__snap);
  if (pendingFocusEnvKey) {
    var eInp = box.querySelector('input[data-env-name="' + CSS.escape(pendingFocusEnvKey) + '"]');
    if (eInp) {
      eInp.focus();
      if (eInp.select) eInp.select();
      pendingFocusEnvKey = null;
    }
  }
}

function render() {
  if (!box) return;
  renderToken++;
  var doc = store.state.doc;
  if (!doc) { box.innerHTML = '<div class="empty">No blueprint loaded.</div>'; return; }
  var sel = store.state.selectedResource;
  if (!sel || sel === "xrd") { renderXRD(); return; }
  if (sel === "environment") { renderEnvironment(); return; }
  var res = selectedResource();
  if (!res) {
    box.innerHTML = '<div class="empty">Resource "' + esc(sel) + '" not found in blueprint.</div>';
    return;
  }
  renderResource(res);
}

/* ---------------- mutations ---------------- */

function setField(path, form) {
  var sel = store.state.selectedResource;
  return op(function () {
    return store.replaceDoc(function (doc) {
      var rs = doc.spec.resources || [];
      for (var i = 0; i < rs.length; i++) {
        if (rs[i].name === sel) {
          rs[i].fields = rs[i].fields || {};
          if (form === null) delete rs[i].fields[path];
          else rs[i].fields[path] = form;
          return;
        }
      }
    });
  }, "unable to update field");
}

function setEnvelopeField(path, form) {
  var sel = store.state.selectedResource;
  return op(function () {
    return store.replaceDoc(function (doc) {
      var rs = doc.spec.resources || [];
      for (var i = 0; i < rs.length; i++) {
        if (rs[i].name === sel) {
          rs[i].envelope = rs[i].envelope || {};
          if (form === null) {
            delete rs[i].envelope[path];
            if (Object.keys(rs[i].envelope).length === 0) delete rs[i].envelope;
          } else {
            rs[i].envelope[path] = form;
          }
          return;
        }
      }
    });
  }, "unable to update envelope field");
}

function paramFrom(existing, patch) {
  var p = {
    type: existing.type,
    required: !!existing.required,
    enum: existing.enum || null,
    default: existing.default || "",
    description: existing.description || "",
    // carried, not rebuilt: dropping this here is how an object param
    // used to lose its whole member tree on any unrelated update
    properties: existing.properties || null,
  };
  Object.keys(patch).forEach(function (k) { p[k] = patch[k]; });
  return p;
}

/* ---- object-parameter member tree helpers (any nesting depth) ---- */

function cloneProps(p) { return p ? JSON.parse(JSON.stringify(p)) : {}; }

// memberParent walks a dot-path to the object holding its final segment.
function memberParent(props, path) {
  var segs = path.split(".");
  var cur = props;
  for (var i = 0; i < segs.length - 1; i++) {
    cur = (cur[segs[i]] || {}).properties;
    if (!cur) return null;
  }
  return cur[segs[segs.length - 1]] === undefined ? null : { parent: cur, key: segs[segs.length - 1] };
}

// memberContainer returns the properties map AT parentPath ("" = the root).
function memberContainer(props, parentPath) {
  if (!parentPath) return props;
  var segs = parentPath.split(".");
  var cur = props;
  for (var i = 0; i < segs.length; i++) {
    var m = cur[segs[i]];
    if (!m) return null;
    if (!m.properties) m.properties = {};
    cur = m.properties;
  }
  return cur;
}

// commitMembers writes a parameter's whole member tree back (empty = the
// free-form map again). Callers wrap it in op() for the shared error path.
function commitMembers(paramName, props) {
  var params = paramsOf(store.state.doc);
  return store.updateParameter(paramName,
    paramFrom(params[paramName], { properties: Object.keys(props).length ? props : null }));
}

/* ---------------- expression preview & snippets ---------------- */

var previewTimers = {};

function triggerExpressionPreview(textarea, isEnv, path) {
  var val = textarea.value.trim();
  var previewKey = isEnv ? ("env:" + path) : path;
  var previewEl = box.querySelector('.expr-preview[data-preview-for="' + CSS.escape(previewKey) + '"]');
  if (!previewEl) return;

  if (!val) {
    previewEl.style.display = "none";
    previewEl.className = "expr-preview";
    return;
  }

  if (previewTimers[previewKey]) {
    clearTimeout(previewTimers[previewKey]);
  }

  previewTimers[previewKey] = setTimeout(function () {
    var selRes = selectedResource();
    var resName = selRes ? selRes.name : "";
    api.previewExpression(val, resName).then(function (resp) {
      if (!previewEl) return;
      previewEl.style.display = "block";
      var badge = previewEl.querySelector(".expr-preview-badge");
      var body = previewEl.querySelector(".expr-preview-body");
      if (resp && resp.error) {
        previewEl.className = "expr-preview err";
        if (badge) badge.textContent = "error";
        if (body) body.textContent = resp.error;
      } else {
        previewEl.className = "expr-preview ok";
        if (badge) badge.textContent = "ok";
        if (body) body.textContent = (resp && resp.rendered) || "(empty output)";
      }
    }).catch(function (err) {
      if (!previewEl) return;
      previewEl.style.display = "block";
      previewEl.className = "expr-preview err";
      var badge = previewEl.querySelector(".expr-preview-badge");
      var body = previewEl.querySelector(".expr-preview-body");
      if (badge) badge.textContent = "error";
      if (body) body.textContent = err.message || String(err);
    });
  }, 100);
}

function updateAllPreviews() {
  if (!box) return;
  var rawTextareas = box.querySelectorAll("textarea.raw");
  Array.prototype.forEach.call(rawTextareas, function (ta) {
    var isEnv = ta.hasAttribute("data-env-raw");
    var path = ta.getAttribute(isEnv ? "data-env-raw" : "data-raw");
    if (ta.value.trim()) {
      triggerExpressionPreview(ta, isEnv, path);
    }
  });
}

function insertSnippetIntoTextarea(textarea, snippet) {
  var start = textarea.selectionStart;
  var end = textarea.selectionEnd;
  var text = textarea.value;
  if (start !== null && end !== null && start !== undefined) {
    textarea.value = text.substring(0, start) + snippet + text.substring(end);
    textarea.selectionStart = textarea.selectionEnd = start + snippet.length;
  } else {
    textarea.value = (text ? text + " " : "") + snippet;
  }
  var isEnv = textarea.hasAttribute("data-env-raw");
  var path = textarea.getAttribute(isEnv ? "data-env-raw" : "data-raw");
  triggerExpressionPreview(textarea, isEnv, path);
  if (isEnv) {
    commitEnvelopeValue(path, "raw", textarea.value);
  } else {
    commitValue(path, "raw", textarea.value);
  }
}

/* ---------------- events ---------------- */

async function commitValue(path, kind, text) {
  var res = selectedResource();
  if (!res) return;
  var entry = entryOf(res, path);
  if (entry && entry.from && kind === "value") {
    if (!confirm('This field is wired from "' + entry.from + '". Overwrite the wire with a literal value?')) {
      render();
      return;
    }
  }
  var ok;
  if (text === "") {
    ok = await setField(path, null);
  } else {
    var form = { value: "", from: "", raw: "" };
    form[kind] = text;
    ok = await setField(path, form);
  }
  if (ok !== null) delete uiMode[path];
}

async function commitEnvelopeValue(path, kind, text) {
  var res = selectedResource();
  if (!res) return;
  var entry = envelopeEntryOf(res, path);
  var isWired = entry && (entry.from || entry.raw === "{{ $xr }}");
  if (isWired && kind === "value") {
    var wireSrc = entry.from || "XR name ($xr)";
    if (!confirm('This envelope field is wired from "' + wireSrc + '". Overwrite the wire with a literal value?')) {
      render();
      return;
    }
  }
  var ok;
  if (text === "") {
    ok = await setEnvelopeField(path, null);
  } else {
    var form = { value: "", from: "", raw: "" };
    form[kind] = text;
    ok = await setEnvelopeField(path, form);
  }
  if (ok !== null) delete uiMode["env:" + path];
}

var boxClickActions = [
  {
    selector: "[data-quick-snippet], [data-env-quick-snippet]",
    run: function (chip) {
      var isEnv = chip.hasAttribute("data-env-quick-snippet");
      var path = chip.getAttribute(isEnv ? "data-env-quick-snippet" : "data-quick-snippet");
      var snippet = chip.getAttribute("data-snippet-val");
      if (!snippet) return;
      var taSelector = isEnv ? ('textarea[data-env-raw="' + CSS.escape(path) + '"]') : ('textarea[data-raw="' + CSS.escape(path) + '"]');
      var ta = box.querySelector(taSelector);
      if (ta) {
        insertSnippetIntoTextarea(ta, snippet);
      }
    }
  },
  {
    selector: "[data-toggle-desc]",
    run: function (el) {
      var descWrap = el.closest(".fld-d");
      if (descWrap) {
        var exp = descWrap.classList.toggle("expanded");
        el.textContent = exp ? "less" : "more";
      }
    }
  },
  {
    selector: "[data-ann-del]",
    run: function (el) {
      var adk = el.getAttribute("data-ann-del");
      var selRes = selectedResource();
      if (!selRes) return;
      store.replaceDoc(function (d) {
        var r = d.spec.resources.find(function (x) { return x.name === selRes.name; });
        if (r && r.annotations) { delete r.annotations[adk]; if (!Object.keys(r.annotations).length) delete r.annotations; }
      });
    }
  },
  {
    selector: "[data-ann-add]",
    run: function () {
      var keyEl = box.querySelector("[data-ann-key]");
      var valEl = box.querySelector("[data-ann-value]");
      var selRes2 = selectedResource();
      if (!selRes2 || !keyEl) return;
      var annKey = keyEl.value.trim();
      if (!annKey) {
        var kMsg = "Annotation key is required (e.g. prefix/name)";
        warnMsg = kMsg;
        store.emit("error", { message: kMsg });
        render();
        var kEl = box.querySelector("[data-ann-key]");
        if (kEl) kEl.focus();
        return;
      }
      var annVal = valEl ? valEl.value.trim() : "";
      if (!annVal) {
        annDraftKey = annKey;
        var vMsg = 'Annotation "' + annKey + '" requires a value (or a temporary placeholder to wire later)';
        warnMsg = vMsg;
        store.emit("error", { message: vMsg });
        render();
        var vEl = box.querySelector("[data-ann-value]");
        if (vEl) vEl.focus();
        return;
      }
      annDraftKey = annKey;
      op(function () {
        return store.replaceDoc(function (d) {
          var r = d.spec.resources.find(function (x) { return x.name === selRes2.name; });
          if (!r) return;
          r.annotations = r.annotations || {};
          r.annotations[annKey] = { value: annVal };
        });
      }).then(function (res) {
        if (res !== null) {
          annDraftKey = "";
          render();
        }
      });
    }
  },
  {
    selector: "button[data-m]",
    needsDoc: true,
    run: function (mb, _doc) {
      var isEnv = mb.hasAttribute("data-env");
      var path = mb.getAttribute("data-path");
      var m = mb.getAttribute("data-m");
      var res = selectedResource();
      var entry = isEnv ? (res ? envelopeEntryOf(res, path) : null) : (res ? entryOf(res, path) : null);
      var isWired = entry && (entry.from || (isEnv && entry.raw === "{{ $xr }}"));
      if (isWired && (m === "v" || m === "r")) {
        var wireSrc = entry.from || "XR name ($xr)";
        if (!confirm('This field is wired from "' + wireSrc + '". Switch modes and overwrite the wire?')) return;
      }
      var mKey = isEnv ? ("env:" + path) : path;
      uiMode[mKey] = m;
      if (m !== "w" && pendingNewParam === mKey) pendingNewParam = null;
      render();
    }
  },
  {
    selector: "[data-unwire]",
    needsDoc: true,
    run: function (un) {
      var p1 = un.getAttribute("data-unwire");
      setField(p1, null).then(function (r) { if (r !== null) delete uiMode[p1]; });
    }
  },
  {
    selector: "[data-env-unwire]",
    needsDoc: true,
    run: function (unEnv) {
      var pe = unEnv.getAttribute("data-env-unwire");
      setEnvelopeField(pe, null).then(function (r) { if (r !== null) delete uiMode["env:" + pe]; });
    }
  },
  {
    selector: "[data-add-map-entry]",
    needsDoc: true,
    run: function (addMap) {
      pendingNewMapEntry = addMap.getAttribute("data-add-map-entry");
      render();
    }
  },
  {
    selector: "[data-new-map-ok]",
    needsDoc: true,
    run: function (mapOk) {
      var mapPath = mapOk.getAttribute("data-new-map-ok");
      var keyInp = box.querySelector('[data-new-map-key="' + CSS.escape(mapPath) + '"]');
      var valInp = box.querySelector('[data-new-map-val="' + CSS.escape(mapPath) + '"]');
      var keyVal = keyInp && keyInp.value.trim();
      if (!keyVal) return;
      var initVal = (valInp && valInp.value.trim()) || "default";
      var fullPath = mapPath + "[" + keyVal + "]";
      setField(fullPath, { value: initVal }).then(function (r) {
        if (r !== null) {
          pendingNewMapEntry = null;
        }
      });
    }
  },
  {
    selector: "[data-new-map-cancel]",
    needsDoc: true,
    run: function () {
      pendingNewMapEntry = null;
      render();
    }
  },
  {
    selector: "[data-del-map-entry]",
    needsDoc: true,
    run: function (delMap) {
      var delPath = delMap.getAttribute("data-del-map-entry");
      setField(delPath, null).then(function (r) {
        if (r !== null) delete uiMode[delPath];
      });
    }
  },
  {
    selector: "[data-npok]",
    needsDoc: true,
    run: function (ok) {
      var p2 = ok.getAttribute("data-npok");
      var isEnv = p2.indexOf("env:") === 0;
      var realPath = isEnv ? p2.slice(4) : p2;
      var nameEl = box.querySelector('[data-npname="' + CSS.escape(p2) + '"]');
      var typeEl = box.querySelector('[data-nptype="' + CSS.escape(p2) + '"]');
      var reqEl = box.querySelector('input[type="checkbox"][data-npreq="' + CSS.escape(p2) + '"]');
      var isReq = reqEl ? reqEl.checked : (ok.getAttribute("data-npreq") === "true");
      var name = nameEl && nameEl.value.trim();
      var type = typeEl && typeEl.value || "string";
      if (!name) return;
      op(function () { return store.addParameter(name, { type: type, required: isReq }); })
        .then(function (docAfter) {
          if (docAfter === null) return null;
          if (isEnv) {
            return setEnvelopeField(realPath, { from: "params." + name, value: "", raw: "" });
          }
          return setField(realPath, { from: "params." + name, value: "", raw: "" });
        })
        .then(function (r) {
          if (r !== null) { pendingNewParam = null; delete uiMode[p2]; }
        });
    }
  },
  {
    selector: "[data-pipe-mode]",
    needsDoc: true,
    run: function (btn) {
      var pidx = parseInt(btn.getAttribute("data-pipe-mode"), 10);
      var target = btn.getAttribute("data-mode-to");
      pipeInputMode[pidx] = target;
      render();
    }
  },
  {
    selector: "[data-npcancel]",
    needsDoc: true,
    run: function () {
      pendingNewParam = null;
      render();
    }
  },
  {
    selector: "[data-madd]",
    needsDoc: true,
    run: function (madd, doc) {
      var maddKey = madd.getAttribute("data-madd").split("|");
      var maddParam = maddKey[0], maddPath = maddKey[1];
      var maddProps = cloneProps((paramsOf(doc)[maddParam] || {}).properties);
      var cont = memberContainer(maddProps, maddPath);
      if (!cont) return;
      var mBase = "member", mNm = mBase + "1", mI = 2;
      while (cont[mNm]) { mNm = mBase + mI; mI++; }
      cont[mNm] = { type: "string" };
      op(function () { return commitMembers(maddParam, maddProps); })
        .then(function (r) { if (r === null) render(); });
    }
  },
  {
    selector: "[data-mdel]",
    needsDoc: true,
    run: function (mdel, doc) {
      var mdelKey = mdel.getAttribute("data-mdel").split("|");
      var mdelProps = cloneProps((paramsOf(doc)[mdelKey[0]] || {}).properties);
      var mdelLoc = memberParent(mdelProps, mdelKey[1]);
      if (!mdelLoc) return;
      delete mdelLoc.parent[mdelLoc.key];
      op(function () { return commitMembers(mdelKey[0], mdelProps); })
        .then(function (r) { if (r === null) render(); });
    }
  },
  {
    selector: "[data-pd]",
    needsDoc: true,
    run: function (pd, doc) {
      if (pd.hasAttribute("disabled")) return;
      var pn = pd.getAttribute("data-pd");
      if (isParamLocked(doc, pn)) return;
      var fo = fanOut(doc, pn);
      if (fo > 0 && !confirm('Parameter "' + pn + '" is wired into ' + fo + " field" + (fo === 1 ? "" : "s") + ". Delete it and unwire all referencing fields?")) return;
      if (paramOrder) {
        var idx = paramOrder.indexOf(pn);
        if (idx !== -1) paramOrder.splice(idx, 1);
      }
      if (fo > 0) {
        var draft = JSON.parse(JSON.stringify(doc));
        cleanParamRefs(draft, pn);
        op(function () { return store.replaceDoc(draft); });
      } else {
        op(function () { return store.deleteParameter(pn); });
      }
    }
  },
  {
    selector: "#addParamBtn",
    needsDoc: true,
    run: function (_, doc) {
      var params = paramsOf(doc);
      var base = "newParam", nm = base, i = 2;
      while (params[nm]) { nm = base + i; i++; }
      pendingFocusParam = nm;
      if (paramOrder && paramOrder.indexOf(nm) === -1) {
        paramOrder.push(nm);
      }
      op(function () { return store.addParameter(nm, { type: "string", required: false }); })
        .then(function (res) {
          if (res === null) {
            pendingFocusParam = null;
            return;
          }
          var inp = box.querySelector('input[data-pn="' + CSS.escape(nm) + '"]');
          if (inp) {
            inp.focus();
            if (inp.select) inp.select();
            pendingFocusParam = null;
          }
        });
    }
  },
  {
    selector: "#addAutoReadyBtn",
    needsDoc: true,
    run: function () {
      op(function () {
        return store.replaceDoc(function (d) {
          d.spec.pipeline = d.spec.pipeline || [];
          d.spec.pipeline.push({
            name: "auto-ready",
            functionRef: "function-auto-ready",
            package: "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
            position: "after"
          });
        });
      });
    }
  },
  {
    selector: "#addPipeStepBtn",
    needsDoc: true,
    run: function () {
      var preset = (box.querySelector("#pipePresetSelect") && box.querySelector("#pipePresetSelect").value) || "custom";
      var pipePresetMap = {
        "auto-ready": {
          name: "auto-ready",
          functionRef: "function-auto-ready",
          package: "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
          position: "after"
        },
        "environment-configs": {
          name: "environment-configs",
          functionRef: "function-environment-configs",
          package: "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0",
          position: "before",
          input: "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Reference\n    ref:\n      name: default"
        },
        "cel-filter": {
          name: "cel-filter",
          functionRef: "function-cel-filter",
          package: "xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.3.0",
          position: "after"
        },
        "extra-resources": {
          name: "extra-resources",
          functionRef: "function-extra-resources",
          package: "xpkg.crossplane.io/crossplane-contrib/function-extra-resources:v0.3.0",
          position: "before"
        },
        "custom": {
          name: "custom-step",
          functionRef: "function-custom",
          package: "xpkg.crossplane.io/crossplane-contrib/function-custom:v0.1.0",
          position: "after"
        }
      };
      var newStep = pipePresetMap[preset] || pipePresetMap.custom;
      op(function () {
        return store.replaceDoc(function (d) {
          d.spec.pipeline = d.spec.pipeline || [];
          d.spec.pipeline.push(newStep);
        });
      });
    }
  },
  {
    selector: "[data-pipe-del]",
    needsDoc: true,
    run: function (pipeDel) {
      var pidx = parseInt(pipeDel.getAttribute("data-pipe-del"), 10);
      op(function () {
        return store.replaceDoc(function (d) {
          if (!d.spec.pipeline) return;
          d.spec.pipeline.splice(pidx, 1);
          if (!d.spec.pipeline.length) delete d.spec.pipeline;
        });
      });
    }
  },
  {
    selector: "[data-wl-sync-app]",
    needsDoc: true,
    run: function (syncBtn) {
      var rname = syncBtn.getAttribute("data-wl-sync-app");
      var appInp = box.querySelector('[data-wl-app="' + rname + '"]');
      var val = appInp ? appInp.value.trim() : "";
      if (!val) val = rname;
      op(function () {
        return store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname; });
          if (!r) return;
          r.fields = r.fields || {};
          delete r.fields["spec.selector.matchLabels.app"];
          delete r.fields["spec.template.metadata.labels.app"];
          r.fields["spec.selector.matchLabels"] = { raw: JSON.stringify({ app: val }) };
          r.fields["spec.template.metadata.labels"] = { raw: JSON.stringify({ app: val }) };
          if (!r.fields["spec.template.spec.containers[0].name"]) {
            r.fields["spec.template.spec.containers[0].name"] = { value: rname };
          }
        });
      });
    }
  },
  {
    selector: "[data-svc-match-wl]",
    needsDoc: true,
    run: function (svcMatchBtn) {
      var rname2 = svcMatchBtn.getAttribute("data-svc-match-wl");
      var matchApp = svcMatchBtn.getAttribute("data-match-app") || "";
      op(function () {
        return store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname2; });
          if (!r) return;
          r.fields = r.fields || {};
          delete r.fields["spec.selector.app"];
          r.fields["spec.selector"] = { raw: JSON.stringify({ app: matchApp }) };
          r.fields["spec.ports[0].port"] = { raw: "8080" };
        });
      });
    }
  },
  {
    selector: "[data-apply-std-labels]",
    needsDoc: true,
    run: function (stdLblBtn) {
      var rname3 = stdLblBtn.getAttribute("data-apply-std-labels");
      op(function () {
        return store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname3; });
          if (!r) return;
          r.annotations = r.annotations || {};
          r.annotations["app.kubernetes.io/managed-by"] = { value: "crossplane" };
          r.annotations["app.kubernetes.io/name"] = { raw: "'{{ $xr }}'" };
          r.annotations["app.kubernetes.io/instance"] = { raw: "'{{ $xr }}'" };
        });
      });
    }
  },
  {
    selector: "[data-apply-ext-name]",
    needsDoc: true,
    run: function (extNameBtn) {
      var rname4 = extNameBtn.getAttribute("data-apply-ext-name");
      op(function () {
        return store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname4; });
          if (!r) return;
          r.annotations = r.annotations || {};
          r.annotations["crossplane.io/external-name"] = { raw: "'{{ $xr }}-" + rname4 + "'" };
        });
      });
    }
  },
  {
    selector: "[data-tab-switch]",
    run: function (el) {
      var target = el.getAttribute("data-tab-switch");
      if (target === "sources") target = "src";
      var btn = document.querySelector('#rtabs button[data-r="' + target + '"]');
      if (btn) btn.click();
    }
  },
  {
    selector: "button[data-env-wire-del]",
    needsDoc: true,
    run: function (btn) {
      var resName = btn.getAttribute("data-wire-res");
      var wirePath = btn.getAttribute("data-wire-path");
      var isEnv = btn.hasAttribute("data-wire-env");
      var isAnn = btn.hasAttribute("data-wire-ann");
      removeWire(resName, wirePath, isEnv, isAnn);
    }
  },
  {
    selector: "button[data-env-del-key]",
    needsDoc: true,
    run: function (btn) {
      var keyName = btn.getAttribute("data-env-del-key");
      deleteEnvKey(keyName);
    }
  },
  {
    selector: "#envAddKeyBtn",
    needsDoc: true,
    run: function (_, doc) {
      var env = (doc && doc.spec && doc.spec.environment) || {};
      var base = "key", nm = base + "1", i = 2;
      while (env[nm]) { nm = base + i; i++; }
      pendingFocusEnvKey = nm;
      addEnvKey(nm, { type: "string" });
    }
  }
];

function onBoxClick(e) {
  for (var i = 0; i < boxClickActions.length; i++) {
    var item = boxClickActions[i];
    var el = e.target.closest(item.selector);
    if (el) {
      if (item.needsDoc) {
        var d = store.state.doc;
        if (!d) return;
        item.run(el, d);
      } else {
        item.run(el);
      }
      return;
    }
  }
}

var wlSimpleFieldMap = {
  "data-wl-replicas": "spec.replicas",
  "data-wl-image": "spec.template.spec.containers[0].image",
  "data-wl-cname": "spec.template.spec.containers[0].name",
  "data-wl-cport": "spec.template.spec.containers[0].ports[0].containerPort",
  "data-svc-app": "spec.selector.app",
  "data-svc-port": "spec.ports[0].port",
  "data-svc-tgtport": "spec.ports[0].targetPort",
  "data-svc-type": "spec.type"
};

var directCommitMap = {
  "data-v": function (t) { commitValue(t.getAttribute("data-v"), "value", t.value); },
  "data-raw": function (t) { commitValue(t.getAttribute("data-raw"), "raw", t.value); },
  "data-env-v": function (t) { commitEnvelopeValue(t.getAttribute("data-env-v"), "value", t.value); },
  "data-env-raw": function (t) { commitEnvelopeValue(t.getAttribute("data-env-raw"), "raw", t.value); }
};

var paramFieldUpdaters = {
  "data-pt": function (t) { return { type: t.value }; },
  "data-pr": function (t) { return { required: t.checked }; },
  "data-pdef": function (t) { return { default: t.value }; },
  "data-pe": function (t) {
    var vals = t.value.split(",").map(function (s) { return s.trim(); }).filter(Boolean);
    return { enum: vals.length ? vals : null };
  }
};

function inferPlural(kind) {
  var lower = (kind || "").toLowerCase();
  if (!lower) return "";
  if (lower.endsWith("s") || lower.endsWith("x") || lower.endsWith("z") || lower.endsWith("ch") || lower.endsWith("sh")) {
    return lower + "es";
  }
  if (lower.endsWith("y") && lower.length > 1) {
    var c = lower.charAt(lower.length - 2);
    if ("aeiou".indexOf(c) === -1) {
      return lower.slice(0, -1) + "ies";
    }
  }
  return lower + "s";
}

var xrdFieldUpdaters = {
  xk: function (t) {
    var kv = t.value.trim();
    if (!kv) { render(); return; }
    op(function () {
      return store.replaceDoc(function (d) {
        d.spec.xrd.kind = kv;
        d.spec.xrd.plural = inferPlural(kv);
      });
    }).then(function (r) { if (r === null) render(); });
  },
  xs: function (t) {
    var sv = t.value;
    op(function () {
      return store.replaceDoc(function (d) { d.spec.xrd.scope = sv; });
    }).then(function (r) { if (r === null) render(); });
  }
};

var pipeAttrs = {
  "data-pipe-name": "name",
  "data-pipe-pos": "position",
  "data-pipe-fn": "functionRef",
  "data-pipe-pkg": "package",
  "data-pipe-input": "input"
};

function onBoxChange(e) {
  var t = e.target;
  if (!t) return;
  var doc = store.state.doc;
  if (t.matches("#envSelName, input[data-env-selection-name]")) {
    var envSelNameMode = (box.querySelector("#envSelMode") && box.querySelector("#envSelMode").value) || "Reference";
    var envSelNameVal = t.value.trim() || "default";
    updateEnvSelection(envSelNameMode, envSelNameVal, "");
    return;
  }
  if (t.matches("#envSelLabels, input[data-env-selection-labels]")) {
    var envSelLblMode = (box.querySelector("#envSelMode") && box.querySelector("#envSelMode").value) || "Selector";
    var envSelLblVal = t.value.trim();
    updateEnvSelection(envSelLblMode, "", envSelLblVal);
    return;
  }
  if (t.matches("#envSelMode")) {
    var envChangeMode = t.value;
    var nameInp = box.querySelector("#envSelName");
    var lblInp = box.querySelector("#envSelLabels");
    var curName = nameInp ? nameInp.value.trim() : "default";
    var curLbl = lblInp ? lblInp.value.trim() : "environment=default";
    updateEnvSelection(envChangeMode, curName, curLbl);
    return;
  }
  if (t.matches("input[data-env-name]")) {
    var oldKey = t.getAttribute("data-env-name");
    var newKey = t.value.trim();
    if (!newKey || newKey === oldKey) {
      t.value = oldKey;
      return;
    }
    renameEnvKey(oldKey, newKey, t);
    return;
  }
  if (t.matches("select[data-env-type]")) {
    var envTypeKey = t.getAttribute("data-env-type");
    setEnvKeyField(envTypeKey, "type", t.value);
    return;
  }
  if (t.matches("input[data-env-req]")) {
    var envReqKey = t.getAttribute("data-env-req");
    setEnvKeyField(envReqKey, "required", t.checked);
    return;
  }
  if (t.matches("input[data-env-val]")) {
    var envValKey = t.getAttribute("data-env-val");
    setEnvKeyField(envValKey, "value", t.value.trim());
    return;
  }
  if (t.matches("select[data-insert-snippet], select[data-env-insert-snippet]")) {
    var isEnv = t.hasAttribute("data-env-insert-snippet");
    var path = t.getAttribute(isEnv ? "data-env-insert-snippet" : "data-insert-snippet");
    var snippet = t.value;
    t.value = "";
    if (!snippet) return;
    var taSelector = isEnv ? ('textarea[data-env-raw="' + CSS.escape(path) + '"]') : ('textarea[data-raw="' + CSS.escape(path) + '"]');
    var ta = box.querySelector(taSelector);
    if (ta) {
      insertSnippetIntoTextarea(ta, snippet);
    }
    return;
  }
  if (t.matches("[data-when-param],[data-when-op],[data-when-val]")) {
    var wrn = t.getAttribute("data-when-param") ||
      t.getAttribute("data-when-op") || t.getAttribute("data-when-val");
    var expr = whenFromControls(box, wrn);
    store.replaceDoc(function (d) {
      var r = d.spec.resources.find(function (x) { return x.name === wrn; });
      if (!r) return;
      if (expr) r.when = expr; else delete r.when;
    });
    return;
  }
  if (t.matches("select[data-foreach]")) {
    var rn = t.getAttribute("data-foreach");
    var val = t.value;
    store.replaceDoc(function (d) {
      var r = d.spec.resources.find(function (x) { return x.name === rn; });
      if (!r) return;
      if (val) r.forEach = val; else delete r.forEach;
    });
    return;
  }
  if (t.hasAttribute("data-wl-app")) {
    var wlAppRname = t.getAttribute("data-wl-app");
    var wlAppVal = t.value.trim();
    if (wlAppVal) {
      op(function () {
        return store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === wlAppRname; });
          if (!r) return;
          r.fields = r.fields || {};
          delete r.fields["spec.selector.matchLabels.app"];
          delete r.fields["spec.template.metadata.labels.app"];
          r.fields["spec.selector.matchLabels"] = { raw: JSON.stringify({ app: wlAppVal }) };
          r.fields["spec.template.metadata.labels"] = { raw: JSON.stringify({ app: wlAppVal }) };
        });
      });
    }
    return;
  }

  for (var wlAttr in wlSimpleFieldMap) {
    if (t.hasAttribute(wlAttr)) {
      var wlRname = t.getAttribute(wlAttr);
      var wlPath = wlSimpleFieldMap[wlAttr];
      var wlVal = t.value.trim();
      (function (rName, fPath, fVal) {
        op(function () {
          return store.replaceDoc(function (d) {
            var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rName; });
            if (!r) return;
            r.fields = r.fields || {};
            if (fVal) r.fields[fPath] = { value: fVal };
            else delete r.fields[fPath];
          });
        });
      })(wlRname, wlPath, wlVal);
      return;
    }
  }

  for (var directAttr in directCommitMap) {
    if (t.hasAttribute(directAttr)) {
      directCommitMap[directAttr](t);
      return;
    }
  }

  if (t.hasAttribute("data-wire")) {
    if (t.dataset.keyNav === "true") return;
    const path = t.getAttribute("data-wire");
    const v = t.value;
    if (v === "__new__") { pendingNewParam = path; render(); return; }
    if (!v) return;
    const fromVal = (v.indexOf("params.") === 0 || v.indexOf("resources.") === 0 || v.indexOf("env.") === 0) ? v : ("params." + v);
    if (t.getAttribute("data-fld-req") === "true" && isOptParamWire(fromVal, paramsOf(doc))) {
      const parentCard = t.closest(".bound");
      const existingWarn = parentCard && parentCard.parentElement && parentCard.parentElement.querySelector(".wire-warn");
      if (!existingWarn && parentCard) {
        const warnDiv = document.createElement("div");
        warnDiv.style.marginTop = "2px";
        warnDiv.innerHTML = '<span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span>';
        parentCard.after(warnDiv);
      }
    }
    setField(path, { from: fromVal, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete uiMode[path]; pendingNewParam = null; } });
    return;
  }

  if (t.hasAttribute("data-env-wire")) {
    if (t.dataset.keyNav === "true") return;
    const path = t.getAttribute("data-env-wire");
    const v = t.value;
    if (v === "__new__") { pendingNewParam = "env:" + path; render(); return; }
    if (!v) return;
    if (v === "$xr") {
      setEnvelopeField(path, { from: "", value: "", raw: "{{ $xr }}" })
        .then(function (r) { if (r !== null) { delete uiMode["env:" + path]; pendingNewParam = null; } });
      return;
    }
    const fromVal = (v.indexOf("params.") === 0 || v.indexOf("resources.") === 0 || v.indexOf("env.") === 0) ? v : ("params." + v);
    if (t.getAttribute("data-fld-req") === "true" && isOptParamWire(fromVal, paramsOf(doc))) {
      const parentCard = t.closest(".bound");
      const existingWarn = parentCard && parentCard.parentElement && parentCard.parentElement.querySelector(".wire-warn");
      if (!existingWarn && parentCard) {
        const warnDiv = document.createElement("div");
        warnDiv.style.marginTop = "2px";
        warnDiv.innerHTML = '<span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span>';
        parentCard.after(warnDiv);
      }
    }
    setEnvelopeField(path, { from: fromVal, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete uiMode["env:" + path]; pendingNewParam = null; } });
    return;
  }

  if (t.hasAttribute("data-pn")) {
    var oldName = t.getAttribute("data-pn"), newName = t.value.trim();
    if (t.hasAttribute("readonly") || t.hasAttribute("disabled") || isParamLocked(doc, oldName)) {
      render();
      return;
    }
    if (!newName || newName === oldName) { render(); return; }
    t.setAttribute("data-pn", newName);
    if (paramOrder) {
      var idx = paramOrder.indexOf(oldName);
      if (idx !== -1) paramOrder[idx] = newName;
    }
    pendingRenamedParam = { from: oldName, to: newName };
    op(function () { return store.renameParameter(oldName, newName); })
      .then(function (r) {
        if (r === null) {
          if (paramOrder) {
            var idx = paramOrder.indexOf(newName);
            if (idx !== -1) paramOrder[idx] = oldName;
          }
          render();
        }
      });
    return;
  }

  var mAttr = null;
  ["data-mname", "data-mtype", "data-mreq", "data-mdef"].some(function (a) {
    if (t.hasAttribute(a)) { mAttr = a; return true; }
    return false;
  });
  if (mAttr) {
    var mKey = t.getAttribute(mAttr).split("|");
    var mParam = mKey[0], mPath = mKey[1];
    var mProps = cloneProps((paramsOf(doc)[mParam] || {}).properties);
    var mLoc = memberParent(mProps, mPath);
    if (!mLoc) return;
    var memberHandler = {
      "data-mname": function () {
        var mNew = t.value.trim();
        if (!mNew || mNew === mLoc.key) { render(); return false; }
        if (mLoc.parent[mNew]) { render(); return false; }
        mLoc.parent[mNew] = mLoc.parent[mLoc.key];
        delete mLoc.parent[mLoc.key];
        return true;
      },
      "data-mtype": function () {
        mLoc.parent[mLoc.key].type = t.value;
        if (t.value !== "object") delete mLoc.parent[mLoc.key].properties;
        if (t.value === "object") { delete mLoc.parent[mLoc.key].default; delete mLoc.parent[mLoc.key].enum; }
        return true;
      },
      "data-mreq": function () {
        mLoc.parent[mLoc.key].required = t.checked;
        return true;
      },
      "data-mdef": function () {
        if (t.value) mLoc.parent[mLoc.key].default = t.value;
        else delete mLoc.parent[mLoc.key].default;
        return true;
      }
    };
    if (memberHandler[mAttr] && memberHandler[mAttr]()) {
      op(function () { return commitMembers(mParam, mProps); })
        .then(function (r) { if (r === null) render(); });
    }
    return;
  }

  var params = paramsOf(doc);
  for (var pAttr in paramFieldUpdaters) {
    if (t.hasAttribute(pAttr)) {
      if (t.hasAttribute("disabled")) return;
      var paramName = t.getAttribute(pAttr);
      if (isParamLocked(doc, paramName) && (pAttr === "data-pt" || pAttr === "data-pr")) {
        render();
        return;
      }
      var patch = paramFieldUpdaters[pAttr](t);
      (function (pn, pPatch) {
        op(function () { return store.updateParameter(pn, paramFrom(params[pn], pPatch)); })
          .then(function (r) { if (r === null) render(); });
      })(paramName, patch);
      return;
    }
  }

  if (xrdFieldUpdaters[t.id]) {
    xrdFieldUpdaters[t.id](t);
    return;
  }

  if (t.hasAttribute("data-pipe-fld")) {
    var pidx = parseInt(t.getAttribute("data-pipe-fld"), 10);
    var fpath = t.getAttribute("data-fld-path");
    var fval = t.value;
    (function (idx, path, val) {
      op(function () {
        return store.replaceDoc(function (d) {
          if (!d.spec.pipeline || !d.spec.pipeline[idx]) return;
          var cur = d.spec.pipeline[idx].input || "";
          var obj = parseInputYAML(cur);
          if (!obj.apiVersion || !obj.kind) {
            var inf = inferFnMeta(d.spec.pipeline[idx]);
            if (inf) {
              obj.apiVersion = obj.apiVersion || inf.apiVersion;
              obj.kind = obj.kind || inf.kind;
            }
          }
          setPathVal(obj, path, val);
          var serialized = serializeInputYAML(obj);
          if (serialized.trim()) d.spec.pipeline[idx].input = serialized;
          else delete d.spec.pipeline[idx].input;
        });
      }).then(function (r) { if (r === null) render(); });
    })(pidx, fpath, fval);
    return;
  }

  for (var pipeAttr in pipeAttrs) {
    if (t.hasAttribute(pipeAttr)) {
      var pidx2 = parseInt(t.getAttribute(pipeAttr), 10);
      var attrKey = pipeAttrs[pipeAttr];
      var pval = t.value;
      (function (idx, key, val) {
        op(function () {
          return store.replaceDoc(function (d) {
            if (!d.spec.pipeline || !d.spec.pipeline[idx]) return;
            if (key === "input" && !val.trim()) {
              delete d.spec.pipeline[idx].input;
            } else {
              d.spec.pipeline[idx][key] = val;
            }
          });
        }).then(function (r) { if (r === null) render(); });
      })(pidx2, attrKey, pval);
      return;
    }
  }
}

function onFsegClick(e) {
  var b = e.target.closest("button");
  if (!b) return;
  filter = b.getAttribute("data-f");
  Array.prototype.forEach.call(fseg.children, function (c) {
    c.setAttribute("aria-pressed", String(c === b));
  });
  render();
}

/* ---------------- init ---------------- */

var initialized = false;

/**
 * Initialize the inspector region (idempotent). main.js calls it once with
 * the region root and the shared store/api.
 * @param {HTMLElement} rootEl  #region-inspector
 * @param {{store?: Object, api?: Object}} [deps]
 */
export function init(rootEl, deps) {
  if (initialized) return;
  if (!rootEl) return;
  initialized = true;
  if (deps && deps.store) store = deps.store;
  if (deps && deps.api) api = deps.api;
  root = rootEl;
  box = root.querySelector("#insp");
  fseg = root.querySelector("#fseg");

  box.addEventListener("click", onBoxClick);
  box.addEventListener("change", onBoxChange);
  box.addEventListener("keydown", function (e) {
    var t = e.target;
    if (!t) return;
    if (t.matches && t.matches("input[data-env-name]")) {
      if (e.key === "Enter") {
        e.preventDefault();
        t.blur();
        return;
      }
      if (e.key === "Escape") {
        t.value = t.getAttribute("data-env-name") || "";
        t.blur();
        return;
      }
    }
    if (t.matches && t.matches("select[data-wire], select[data-env-wire]")) {
      if (e.key === "Enter") {
        delete t.dataset.keyNav;
        e.preventDefault();
        onBoxChange({ target: t });
        t.blur();
        return;
      }
      if (e.key === "Escape") {
        delete t.dataset.keyNav;
        t.blur();
        return;
      }
      if (e.key !== "Tab") {
        t.dataset.keyNav = "true";
        // Clear on next macrotask so any subsequent click or programmatic selection commits
        setTimeout(function () {
          delete t.dataset.keyNav;
        }, 50);
      }
      return;
    }
    if (e.key === "Enter" && (t.tagName === "INPUT" || t.tagName === "SELECT")) {
      if (t.matches && t.matches("[data-npname]")) {
        e.preventDefault();
        var npKey = t.getAttribute("data-npname");
        var npAddBtn = box.querySelector('[data-npok="' + CSS.escape(npKey) + '"]');
        if (npAddBtn) npAddBtn.click();
        return;
      }
      if (t.matches && (t.matches("[data-ann-key]") || t.matches("[data-ann-value]"))) {
        e.preventDefault();
        var addBtn = box.querySelector("[data-ann-add]");
        if (addBtn) addBtn.click();
        return;
      }
      e.preventDefault();
      t.blur();
    }
  });
  box.addEventListener("input", function (e) {
    if (warnMsg) {
      warnMsg = null;
      var wb = box.querySelector(".warnbar");
      if (wb) wb.remove();
    }
    var t = e.target;
    if (t && t.matches && t.matches("[data-ann-key]")) {
      annDraftKey = t.value;
    }
    if (!t || !t.matches("textarea.raw")) return;
    var isEnv = t.hasAttribute("data-env-raw");
    var path = t.getAttribute(isEnv ? "data-env-raw" : "data-raw");
    triggerExpressionPreview(t, isEnv, path);
  });
  if (fseg) fseg.addEventListener("click", onFsegClick);

  var lastSourcesSig = "";
  store.subscribe("doc", function () {
    warnMsg = null;
    var d = store.state.doc;
    var sel = store.state.selectedResource;
    if (sel === "environment") {
      var hasEnv = d && d.spec && d.spec.environment && Object.keys(d.spec.environment).length > 0;
      if (!hasEnv) {
        store.select(null);
        return;
      }
    } else if (sel && sel !== "xrd") {
      var found = d && d.spec && d.spec.resources && d.spec.resources.some(function (r) { return r.name === sel; });
      if (!found) {
        store.select(null);
        return;
      }
    }
    var sig = ((d && d.spec && d.spec.sources) || [])
      .map(function (s) { return s.provider; }).join("|");
    if (sig !== lastSourcesSig) {
      lastSourcesSig = sig;
      kindsPromise = null;
    }
    render();
  });
  store.subscribe("selection", function () {
    paramOrder = null;
    pendingRenamedParam = null;
    uiMode = {};
    pendingNewParam = null;
    pendingNewMapEntry = null;
    pendingFocusParam = null;
    warnMsg = null;
    render();
  });
  store.subscribe("generate", function () {
    if (warnMsg) {
      warnMsg = null;
      render();
    }
  });

  render();
}
