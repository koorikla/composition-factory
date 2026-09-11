/**
 * Region: INSPECTOR. Root element: #region-inspector (body: #insp, filter: #fseg).
 * Modular coordinator orchestrating:
 *   - inspector/xrd.js: XRD composite form rendering, parameter member tree, pipeline step schemas
 *   - inspector/preview.js: CEL & Go-template expression preview, snippet catalogue, live debouncing
 *   - inspector/events.js: DOM event listeners, action dispatch maps, and mutation commits
 */

import { store as defaultStore } from "../store.js";
import * as defaultApi from "../api.js";
import { esc } from "../dom.js";
import { parseFrom, findEnvWires, listWires } from "../wires.js";
import { mapResourceCoordinates, deleteEnvKeyFromDoc, renameEnvKeyInDoc, parseEnvSelection } from "../utils.js";

import { state, PARAM_TYPES } from "./inspector/state.js";
import {
  renderXRD, paramsOf, isParamLocked, cleanParamRefs,
  paramFrom, cloneProps, memberParent, memberContainer, commitMembers,
  inferFnMeta, parseInputYAML, serializeInputYAML, getPathVal, setPathVal,
  fieldsFor, getCatalogueFunctions, parseWhen,
} from "./inspector/xrd.js";
import {
  rawEditorHtml, buildSnippets, triggerExpressionPreview,
  updateAllPreviews, insertSnippetIntoTextarea,
} from "./inspector/preview.js";
import {
  commitValue, commitEnvelopeValue, onBoxClick, onBoxChange,
  bindInspectorEvents, boxClickActions, directCommitMap, inferPlural,
} from "./inspector/events.js";

export {
  mapResourceCoordinates,
  PARAM_TYPES,
  renderXRD, paramsOf, isParamLocked, cleanParamRefs,
  paramFrom, cloneProps, memberParent, memberContainer, commitMembers,
  inferFnMeta, parseInputYAML, serializeInputYAML, getPathVal, setPathVal,
  fieldsFor, getCatalogueFunctions, parseWhen,
  rawEditorHtml, buildSnippets, triggerExpressionPreview,
  updateAllPreviews, insertSnippetIntoTextarea,
  commitValue, commitEnvelopeValue, onBoxClick, onBoxChange,
  bindInspectorEvents, boxClickActions, directCommitMap, inferPlural,
};

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

function formatWireBinding(fromExpr, targetRes, targetPath) {
  if (!fromExpr || typeof fromExpr !== "string") return "";
  var parsed = parseFrom(fromExpr);
  if (parsed) {
    if (parsed.kind === "status") {
      return parsed.resource + ".status." + parsed.statusPath + " \u2192 " + targetRes + "." + targetPath;
    } else if (parsed.kind === "env") {
      return "env." + parsed.key + " \u2192 " + targetRes + "." + targetPath;
    } else if (parsed.kind === "param") {
      return "$" + parsed.param + " \u2192 " + targetRes + "." + targetPath;
    }
  }
  return fromExpr + " \u2192 " + targetRes + "." + targetPath;
}

var STATUS_SEGMENT_RE = /^[a-zA-Z_][a-zA-Z0-9_]*$/;

function isValidStatusPath(p) {
  if (!p || typeof p !== "string") return false;
  var segs = p.split(".");
  for (var i = 0; i < segs.length; i++) {
    if (!STATUS_SEGMENT_RE.test(segs[i])) return false;
  }
  return true;
}

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

async function kindDetail(apiVersion, kind) {
  var key = apiVersion + "|" + kind;
  if (!kindDetailCache[key]) {
    kindDetailCache[key] = await api.getKind(apiVersion, kind).catch(function () { return null; });
  }
  return kindDetailCache[key];
}

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
      var rawSfs = (otherStatusMap && otherStatusMap[r.name]) || [
        { path: "atProvider.url", type: "string" },
        { path: "atProvider.arn", type: "string" },
        { path: "atProvider.id", type: "string" },
      ];
      var sfs = rawSfs.filter(function (sf) { return isValidStatusPath(sf.path); });
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
        const fullBinding = formatWireBinding(entry.from, res.name, f.path);
        h += '<div class="bound' + (isEnvWire ? ' shared' : '') + '"' + bgStyle + (fullBinding ? ' title="' + esc(fullBinding) + '" data-wire-binding="' + esc(fullBinding) + '" tabindex="0" role="button"' : '') + '><span style="color:' + wireCol + '">&#8592;</span>' +
          '<span class="src' + (isEnvWire ? ' sh' : '') + '" style="color:' + wireCol + '"' + (fullBinding ? ' title="' + esc(fullBinding) + '"' : '') + '>' + esc(entry.from || "") + "</span>" +
          '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span>' +
          (fullBinding ? '<div class="bound-binding-detail" style="color:' + wireCol + '">' + esc(fullBinding) + '</div>' : '') +
          '</div>';
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
            var meFullBinding = formatWireBinding(meEntry.from, res.name, me.fullPath);
            h += '<div class="bound' + (isMeEnv ? ' shared' : '') + '"' + bgStyle + (meFullBinding ? ' title="' + esc(meFullBinding) + '" data-wire-binding="' + esc(meFullBinding) + '" tabindex="0" role="button"' : '') + '><span style="color:' + wireCol + '">&#8592;</span>' +
              '<span class="src' + (isMeEnv ? ' sh' : '') + '" style="color:' + wireCol + '"' + (meFullBinding ? ' title="' + esc(meFullBinding) + '"' : '') + '>' + esc(meEntry.from || "") + "</span>" +
              '<span class="x" role="button" tabindex="0" data-unwire="' + esc(me.fullPath) + '" title="Remove wire">&#215;</span>' +
              (meFullBinding ? '<div class="bound-binding-detail" style="color:' + wireCol + '">' + esc(meFullBinding) + '</div>' : '') +
              '</div>';
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
        const fullBinding = formatWireBinding(entry.from, res.name, f.path);
        h += '<div class="bound' + (isEnvWire ? ' shared' : '') + '"' + bgStyle + (fullBinding ? ' title="' + esc(fullBinding) + '" data-wire-binding="' + esc(fullBinding) + '" tabindex="0" role="button"' : '') + '><span style="color:' + wireCol + '">&#8592;</span>' +
          '<span class="src' + (isEnvWire ? ' sh' : '') + '" style="color:' + wireCol + '"' + (fullBinding ? ' title="' + esc(fullBinding) + '"' : '') + '>' + esc(entry.from || "") + "</span>" +
          '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span>' +
          (fullBinding ? '<div class="bound-binding-detail" style="color:' + wireCol + '">' + esc(fullBinding) + '</div>' : '') +
          '</div>';
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
      var envFullBinding = isXr ? ("$xr \u2192 " + res.name + ".envelope." + f.path) : formatWireBinding(entry.from, res.name, "envelope." + f.path);
      h += '<div class="bound' + (isEnvWire ? ' shared' : '') + '"' + bgStyle + (envFullBinding ? ' title="' + esc(envFullBinding) + '" data-wire-binding="' + esc(envFullBinding) + '" tabindex="0" role="button"' : '') + '><span style="color:' + wireCol + '">&#8592;</span>' +
        '<span class="src' + (isEnvWire ? ' sh' : '') + '" style="color:' + wireCol + '"' + (envFullBinding ? ' title="' + esc(envFullBinding) + '"' : '') + '>' + esc(wireLabel) + "</span>" +
        '<span class="x" role="button" tabindex="0" data-env-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span>' +
        (envFullBinding ? '<div class="bound-binding-detail" style="color:' + wireCol + '">' + esc(envFullBinding) + '</div>' : '') +
        '</div>';
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
        if (od && od.status) otherStatusMap[or.name] = od.status.filter(function (sf) {
          return isValidStatusPath(sf.path);
        });
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
    '<select class="tsel" data-foreach="' + esc(res.name) + '" style="flex:1;min-width:0" ' +
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
  // bare boolean param or env, or (params|env).x == "literal" / != "literal"
  var w = parseWhen(res.when);
  var env = (doc && doc.spec && doc.spec.environment) || {};
  var condParams = Object.keys(allParams).filter(function (n) {
    var t = allParams[n] && allParams[n].type;
    return t === "boolean" || t === "string";
  });
  var condEnv = Object.keys(env).filter(function (k) {
    var t = env[k] && env[k].type;
    return t === "boolean" || t === "string";
  });

  var selectedWhenVal = "";
  if (w.param) {
    var src = w.source || (env[w.param] && !allParams[w.param] ? "env" : "params");
    selectedWhenVal = src + "." + w.param;
  }

  h += '<div class="fld"><div class="frow" style="margin-bottom:0">' +
    '<span class="lbl" style="flex:0 0 auto">when</span>' +
    '<select class="tsel" data-when-param="' + esc(res.name) + '" style="flex:1;min-width:0" ' +
    'title="Compose this resource only when the condition holds">' +
    '<option value=""' + (!selectedWhenVal ? " selected" : "") + ">\u2014 always \u2014</option>" +
    condEnv.map(function (k) {
      var val = "env." + k;
      return '<option value="' + esc(val) + '"' + (selectedWhenVal === val ? " selected" : "") + ">" + esc(val) + "</option>";
    }).join("") +
    condParams.map(function (n) {
      var val = "params." + n;
      return '<option value="' + esc(val) + '"' + (selectedWhenVal === val ? " selected" : "") + ">" + esc(val) + "</option>";
    }).join("");

  if (selectedWhenVal && selectedWhenVal.indexOf("env.") === 0 && condEnv.indexOf(w.param) === -1) {
    h += '<option value="' + esc(selectedWhenVal) + '" selected>' + esc(selectedWhenVal) + "</option>";
  } else if (selectedWhenVal && selectedWhenVal.indexOf("params.") === 0 && condParams.indexOf(w.param) === -1) {
    h += '<option value="' + esc(selectedWhenVal) + '" selected>' + esc(selectedWhenVal) + "</option>";
  }

  h += "</select>";

  var whenDecl = null;
  if (w.param) {
    if (w.source === "env" || (!w.source && env[w.param])) {
      whenDecl = env[w.param];
    } else {
      whenDecl = allParams[w.param];
    }
  }

  if (whenDecl && whenDecl.type === "string") {
    var vals = whenDecl.enum || [];
    h += '<select class="tsel" data-when-op="' + esc(res.name) + '" style="flex:0 0 auto">' +
      ["==", "!="].map(function (o) {
        return '<option value="' + o + '"' + (w.op === o ? " selected" : "") + ">" + o + "</option>";
      }).join("") + "</select>";
    h += vals.length
      ? '<select class="tsel" data-when-val="' + esc(res.name) + '" style="flex:1;min-width:0">' +
        vals.map(function (v) {
          return '<option value="' + esc(v) + '"' + (w.val === v ? " selected" : "") + ">" + esc(v) + "</option>";
        }).join("") + "</select>"
      : '<input class="tin" data-when-val="' + esc(res.name) + '" style="flex:1;min-width:0" value="' + esc(w.val || "") + '" placeholder="value">';
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
    env = (doc && doc.spec && doc.spec.environment) || {};
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
        var fullBinding = f.from ? formatWireBinding(f.from, res.name, "annotations." + k) : "";
        var rowTitle = fullBinding || (k + (f.from ? " \u2190 " + f.from : ""));
        return '<div class="frow ann-row' + (fullBinding ? ' has-wire' : '') + '" style="margin-bottom:2px" title="' + esc(rowTitle) + '" data-wire-binding="' + esc(fullBinding) + '" tabindex="0" role="button">' +
          '<span class="ann-key" title="' + esc(k) + '">' + esc(k) + '</span>' +
          '<span class="ann-val dg" title="' + esc(rowTitle) + '">' + esc(val) + '</span>' +
          '<button class="del" data-ann-del="' + esc(k) + '" title="Remove annotation">\u00d7</button>' +
          (fullBinding ? '<div class="ann-binding-detail" title="' + esc(fullBinding) + '">' + esc(fullBinding) + '</div>' : '') +
          '</div>';
      }).join("") +
      '<div class="frow" style="margin-top:4px;margin-bottom:0">' +
      '<input class="tin" data-ann-key placeholder="prefix/name" value="' + esc(annDraftKey || "") + '" style="flex:1;min-width:0">' +
      '<input class="tin" data-ann-value placeholder="value (or placeholder to wire)" style="flex:1;min-width:0">' +
      '<button class="btn sm" data-ann-add>Add</button></div></div>';

    // Status outputs section
    var validStatus = (detail && detail.status || []).filter(function (sf) {
      return isValidStatusPath(sf.path);
    });
    if (validStatus.length > 0) {
      h += '<div class="insp-sec" style="margin-top:14px;padding:8px 12px;border-top:1px solid var(--rule);background:var(--surface-2)">' +
        '<div style="font-size:11px;font-weight:600;color:var(--wire-status);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:4px">Status Outputs</div>' +
        '<div style="font-size:11px;color:var(--faint);margin-bottom:6px">Other resources can wire from this object\'s status:</div>' +
        validStatus.slice(0, 10).map(function (sf) {
          return '<div style="display:flex;align-items:center;justify-content:space-between;gap:6px;padding:2px 0;font-size:11px;min-width:0">' +
            '<code style="color:var(--wire-status);font-family:var(--mono);min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="status.' + esc(sf.path) + '">status.' + esc(sf.path) + '</code>' +
            '<span style="color:var(--faint);flex-shrink:0">' + esc(sf.type) + '</span>' +
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

      var hasEnv = d.spec.environment && Object.keys(d.spec.environment).length > 0;
      var cfg = (d.spec.environmentConfigs && d.spec.environmentConfigs[0]) ? d.spec.environmentConfigs[0] : {};

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
        if (lblKeys.length === 0) {
          labelsObj = { environment: "default" };
          lblKeys = ["environment"];
        }
        var lblLines = lblKeys.map(function (k) {
          return "        " + k + ": " + labelsObj[k];
        }).join("\n");
        step.input = "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Selector\n    selector:\n      matchLabels:\n" + (lblLines ? lblLines + "\n" : "        environment: default\n");

        if (hasEnv || (d.spec.environmentConfigs && d.spec.environmentConfigs.length > 0)) {
          delete cfg.name;
          cfg.selector = { matchLabels: labelsObj };
          if (!Array.isArray(d.spec.environmentConfigs) || d.spec.environmentConfigs.length === 0) {
            d.spec.environmentConfigs = [cfg];
          } else {
            d.spec.environmentConfigs[0] = cfg;
          }
        }
      } else {
        var refName = (name && name.trim()) || "default";
        step.input = "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Reference\n    ref:\n      name: " + refName + "\n";

        if (hasEnv || (d.spec.environmentConfigs && d.spec.environmentConfigs.length > 0)) {
          delete cfg.selector;
          cfg.name = refName;
          if (!Array.isArray(d.spec.environmentConfigs) || d.spec.environmentConfigs.length === 0) {
            d.spec.environmentConfigs = [cfg];
          } else {
            d.spec.environmentConfigs[0] = cfg;
          }
        }
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
      store.emit("error", { status: 400, message: msg, source: "inspector" });
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
    store.emit("error", { status: 400, message: msg, source: "inspector" });
    if (inputEl) inputEl.value = oldKey;
    render();
    return;
  }

  if (env[newKey] && newKey !== oldKey) {
    msg = 'Environment key "' + newKey + '" already exists';
    warnMsg = msg;
    store.emit("error", { status: 400, message: msg, source: "inspector" });
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
      var item = /** @type {EnvironmentKey|Record<string, any>} */ (env[k] || {});
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


Object.defineProperties(state, {
  store: { get() { return store; }, set(v) { store = v; }, configurable: true },
  api: { get() { return api; }, set(v) { api = v; }, configurable: true },
  root: { get() { return root; }, set(v) { root = v; }, configurable: true },
  box: { get() { return box; }, set(v) { box = v; }, configurable: true },
  fseg: { get() { return fseg; }, set(v) { fseg = v; }, configurable: true },
  filter: { get() { return filter; }, set(v) { filter = v; }, configurable: true },
  warnMsg: { get() { return warnMsg; }, set(v) { warnMsg = v; }, configurable: true },
  uiMode: { get() { return uiMode; }, set(v) { uiMode = v; }, configurable: true },
  pendingNewParam: { get() { return pendingNewParam; }, set(v) { pendingNewParam = v; }, configurable: true },
  pendingNewMapEntry: { get() { return pendingNewMapEntry; }, set(v) { pendingNewMapEntry = v; }, configurable: true },
  pendingFocusParam: { get() { return pendingFocusParam; }, set(v) { pendingFocusParam = v; }, configurable: true },
  paramOrder: { get() { return paramOrder; }, set(v) { paramOrder = v; }, configurable: true },
  pendingRenamedParam: { get() { return pendingRenamedParam; }, set(v) { pendingRenamedParam = v; }, configurable: true },
  pendingFocusEnvKey: { get() { return pendingFocusEnvKey; }, set(v) { pendingFocusEnvKey = v; }, configurable: true },
  pendingRenamedEnvKey: { get() { return pendingRenamedEnvKey; }, set(v) { pendingRenamedEnvKey = v; }, configurable: true },
  lastDocName: { get() { return lastDocName; }, set(v) { lastDocName = v; }, configurable: true },
  annDraftKey: { get() { return annDraftKey; }, set(v) { annDraftKey = v; }, configurable: true },
  annDraftRes: { get() { return annDraftRes; }, set(v) { annDraftRes = v; }, configurable: true },
  renderToken: { get() { return renderToken; }, set(v) { renderToken = v; }, configurable: true },
  kindsPromise: { get() { return kindsPromise; }, set(v) { kindsPromise = v; }, configurable: true },
  fieldsCache: { get() { return fieldsCache; }, set(v) { fieldsCache = v; }, configurable: true },
  kindDetailCache: { get() { return kindDetailCache; }, set(v) { kindDetailCache = v; }, configurable: true },
});
state.render = render;
state.op = op;
state.selectedResource = selectedResource;
state.entryOf = entryOf;
state.envelopeEntryOf = envelopeEntryOf;
state.docMode = docMode;
state.setField = setField;
state.setEnvelopeField = setEnvelopeField;
state.updateEnvSelection = updateEnvSelection;
state.renameEnvKey = renameEnvKey;
state.setEnvKeyField = setEnvKeyField;
state.deleteEnvKey = deleteEnvKey;
state.addEnvKey = addEnvKey;
state.removeWire = removeWire;
state.snapshotFocusedEdit = snapshotFocusedEdit;
state.restoreFocusedEdit = restoreFocusedEdit;

var initialized = false;

export function init(rootEl, deps) {
  if (initialized) return;
  if (!rootEl) return;
  initialized = true;
  if (deps && deps.store) store = deps.store;
  if (deps && deps.api) api = deps.api;
  root = rootEl;
  box = root.querySelector("#insp");
  fseg = root.querySelector("#fseg");

  bindInspectorEvents(box, fseg);

  if (box) {
    box.addEventListener("click", function (e) {
      var target = e.target.closest(".ann-row, .bound");
      if (target && !e.target.closest("button, .del, .x, input, select, textarea")) {
        target.classList.toggle("expanded");
      }
    });
  }

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

  render();
}
