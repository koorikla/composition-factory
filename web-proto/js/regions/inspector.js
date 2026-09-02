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
import { fanOut } from "../wires.js";
import { esc } from "../dom.js";

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

function entryOf(res, path) {
  var f = res && res.fields && res.fields[path];
  if (!f || typeof f !== "object") return null;
  var from = typeof f.from === "string" ? f.from : "";
  var value = typeof f.value === "string" ? f.value : "";
  var raw = typeof f.raw === "string" ? f.raw : "";
  if (!from && !value && !raw) return null;
  return { from: from, value: value, raw: raw };
}

function envelopeEntryOf(res, path) {
  var f = res && res.envelope && res.envelope[path];
  if (!f || typeof f !== "object") return null;
  var from = typeof f.from === "string" ? f.from : "";
  var value = typeof f.value === "string" ? f.value : "";
  var raw = typeof f.raw === "string" ? f.raw : "";
  if (!from && !value && !raw) return null;
  return { from: from, value: value, raw: raw };
}

function docMode(entry) {
  if (!entry) return "v";
  if (entry.from) return "w";
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

/**
 * Run a store operation, capturing the "error" the store emits for it so the
 * inspector can show the server's message verbatim in its warnbar.
 * warnMsg is cleared up-front so the success-path "doc" re-render is clean.
 */
async function op(fn) {
  warnMsg = null;
  var err = null;
  var un = store.subscribe("error", function (e) { err = e; });
  var res;
  try {
    res = await fn();
  } finally {
    un();
  }
  if (res === null) {
    warnMsg = err ? err.message : "operation failed";
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

function modeButtons(path, pressed, isEnv) {
  var titles = { v: "Literal value", w: "Wire from a parameter or resource status", r: "Raw go-template" };
  var envAttr = isEnv ? ' data-env="1"' : "";
  return '<span class="modes">' + ["v", "w", "r"].map(function (x) {
    return '<button' + envAttr + ' data-m="' + x + '" data-path="' + esc(path) + '" aria-pressed="' +
      (pressed === x) + '" title="' + titles[x] + '">' + x.toUpperCase() + "</button>";
  }).join("") + "</span>";
}

function wireSelectHtml(path, fieldType, params, otherResources, otherStatusMap, isEnv) {
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
  var npKey = isEnv ? ("env:" + path) : path;
  var h = '<div class="bound"><span style="color:var(--faint)">&#8592;</span>' +
    '<select class="tsel" ' + wireAttr + esc(path) + '" style="flex:1">' +
    '<option value="">wire to&#8230;</option>';

  if (names.length > 0) {
    h += '<optgroup label="XRD Parameters">';
    names.forEach(function (n) {
      h += '<option value="params.' + esc(n) + '">params.' + esc(n) + "</option>";
    });
    h += '</optgroup>';
  }

  if (otherResources && otherResources.length > 0) {
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
          h += '<option value="' + esc(wireVal) + '">' + esc(wireVal) + "</option>";
        }
      });
    });
    h += '</optgroup>';

    var isRefField = /Ref(\.name)?$|Refs(\[\d+\])?(\.name)?$|Selector(\.matchLabels)?$/i.test(path);
    if (isRefField || fieldType === "string") {
      h += '<optgroup label="Resource Name (*Ref)">';
      otherResources.forEach(function (r) {
        var wireVal = "resources." + r.name + ".status.atProvider.id";
        h += '<option value="' + esc(wireVal) + '">' + esc(r.name) + ' (name / ID)</option>';
      });
      h += '</optgroup>';
    }
  }

  h += '<option value="__new__">+ new XRD parameter&#8230;</option></select></div>';
  if (pendingNewParam === npKey) {
    h += '<div class="frow" style="margin-top:4px;margin-bottom:0">' +
      '<input class="tin" data-npname="' + esc(npKey) + '" placeholder="parameterName" aria-label="New parameter name">' +
      '<select class="tsel" data-nptype="' + esc(npKey) + '" aria-label="New parameter type">' +
      PARAM_TYPES.map(function (t) {
        return "<option" + (t === suggestedParamType(fieldType) ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<button class="btn sm" data-npok="' + esc(npKey) + '">Add</button>' +
      '<button class="del" data-npcancel="' + esc(npKey) + '" title="Cancel">&#215;</button></div>';
  }
  return h;
}

function fieldRow(res, f, params, otherResources, otherStatusMap) {
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

  if (filter === "req" && !(f.requiredChain || f.branch || entry || mapEntries.length)) return "";
  if (filter === "set" && !entry && !mapEntries.length) return "";

  var wired = m === "w" && dm === "w" && !uiMode[f.path] && entry;
  var isStatusWire = wired && entry.from && entry.from.indexOf("resources.") === 0;
  var h = '<div class="fld' + (dm === "w" && entry ? " wired" : "") + '" style="padding-left:' + (12 + (f.depth || 0) * 11) + 'px">' +
    '<div class="fld-h"><span class="n">' + esc(f.path) + '</span><span class="t">' + esc(f.type) + "</span>" +
    (f.required ? '<span class="rq">req</span>' : "") +
    modeButtons(f.path, m, false) +
    '</div><div class="fld-d">' + esc(f.description) + "</div>";

  if (isMap) {
    if (m === "w") {
      if (dm === "w" && !uiMode[f.path] && entry) {
        var wireCol = isStatusWire ? "var(--wire-status)" : "var(--wire-xrd)";
        var bgStyle = isStatusWire ? ' style="background:var(--wire-status-soft)"' : "";
        h += '<div class="bound"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
          '<span class="src" style="color:' + wireCol + '">' + esc(entry.from || "") + "</span>" +
          '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
      } else {
        h += wireSelectHtml(f.path, f.type, params, otherResources, otherStatusMap, false);
      }
    } else if (m === "r") {
      h += '<textarea class="val raw" data-raw="' + esc(f.path) + '" rows="2" placeholder="{{ }}">' +
        esc((dm === "r" && entry) ? entry.raw : "") + "</textarea>";
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

        h += '<div class="map-entry-card" style="padding:6px 8px;background:var(--surface-2);border-radius:4px;border:1px solid var(--rule)">' +
          '<div class="frow" style="margin-bottom:3px;align-items:center">' +
          '<span style="font-family:var(--mono);font-size:11px;font-weight:600;color:var(--ink);flex:1">[' + esc(me.key) + ']</span>' +
          modeButtons(me.fullPath, meM, false) +
          '<button class="del" data-del-map-entry="' + esc(me.fullPath) + '" title="Delete key" style="margin-left:4px">&#215;</button>' +
          '</div>';

        if (meM === "w") {
          if (meWired) {
            var wireCol = isMeStatus ? "var(--wire-status)" : "var(--wire-xrd)";
            var bgStyle = isMeStatus ? ' style="background:var(--wire-status-soft)"' : "";
            h += '<div class="bound"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
              '<span class="src" style="color:' + wireCol + '">' + esc(meEntry.from || "") + "</span>" +
              '<span class="x" role="button" tabindex="0" data-unwire="' + esc(me.fullPath) + '" title="Remove wire">&#215;</span></div>';
          } else {
            h += wireSelectHtml(me.fullPath, "string", params, otherResources, otherStatusMap, false);
          }
        } else if (meM === "r") {
          h += '<textarea class="val raw" data-raw="' + esc(me.fullPath) + '" rows="1" placeholder="{{ }}">' +
            esc((meDm === "r" && meEntry) ? meEntry.raw : "") + "</textarea>";
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
        var wireCol = isStatusWire ? "var(--wire-status)" : "var(--wire-xrd)";
        var bgStyle = isStatusWire ? ' style="background:var(--wire-status-soft)"' : "";
        h += '<div class="bound"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
          '<span class="src" style="color:' + wireCol + '">' + esc(entry.from || "") + "</span>" +
          '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
      } else {
        h += wireSelectHtml(f.path, f.type, params, otherResources, otherStatusMap, false);
      }
    } else if (m === "r") {
      h += '<textarea class="val raw" data-raw="' + esc(f.path) + '" rows="2" placeholder="{{ }}">' +
        esc((dm === "r" && entry) ? entry.raw : "") + "</textarea>";
    } else {
      h += '<input class="val" data-v="' + esc(f.path) + '" value="' + esc((dm === "v" && entry) ? entry.value : "") +
        '" placeholder="' + (f.required ? "required &#8212; set a value or wire it" : "unset &#8212; omitted from output") + '">';
    }
  }
  return h + "</div>";
}

function envelopeFieldRow(res, f, params, otherResources, otherStatusMap) {
  var entry = envelopeEntryOf(res, f.path);
  var dm = docMode(entry);
  var mKey = "env:" + f.path;
  var m = uiMode[mKey] || dm;

  if (filter === "set" && !entry) return "";

  var wired = m === "w" && dm === "w" && !uiMode[mKey] && entry;
  var isStatusWire = wired && entry.from && entry.from.indexOf("resources.") === 0;
  var h = '<div class="fld' + (dm === "w" && entry ? " wired" : "") + '" style="padding-left:' + (12 + (f.depth || 0) * 11) + 'px">' +
    '<div class="fld-h"><span class="n">' + esc(f.path) + '</span><span class="t">' + esc(f.type) + "</span>" +
    (f.required ? '<span class="rq">req</span>' : "") +
    modeButtons(f.path, m, true) +
    '</div><div class="fld-d">' + esc(f.description) + "</div>";

  if (m === "w") {
    if (dm === "w" && !uiMode[mKey] && entry) {
      var wireCol = isStatusWire ? "var(--wire-status)" : "var(--wire-xrd)";
      var bgStyle = isStatusWire ? ' style="background:var(--wire-status-soft)"' : "";
      h += '<div class="bound"' + bgStyle + '><span style="color:' + wireCol + '">&#8592;</span>' +
        '<span class="src" style="color:' + wireCol + '">' + esc(entry.from || "") + "</span>" +
        '<span class="x" role="button" tabindex="0" data-env-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
    } else {
      h += wireSelectHtml(f.path, f.type, params, otherResources, otherStatusMap, true);
    }
  } else if (m === "r") {
    h += '<textarea class="val raw" data-env-raw="' + esc(f.path) + '" rows="2" placeholder="{{ }}">' +
      esc((dm === "r" && entry) ? entry.raw : "") + "</textarea>";
  } else {
    var ph = f.path === "providerConfigRef.name"
      ? "default: $spec.providerName"
      : (f.path === "providerConfigRef.kind"
        ? "default: ClusterProviderConfig"
        : (f.required ? "required &#8212; set a value or wire it" : "unset &#8212; omitted from envelope"));
    h += '<input class="val" data-env-v="' + esc(f.path) + '" value="' + esc((dm === "v" && entry) ? entry.value : "") +
      '" placeholder="' + ph + '">';
  }
  return h + "</div>";
}

function workloadPresetHtml(res, doc, allParams, otherResources) {
  if (!res || res.provider !== "k8s") return "";
  var kind = res.kind;
  if (kind !== "Deployment" && kind !== "StatefulSet" && kind !== "DaemonSet" && kind !== "Job" && kind !== "Service") {
    return "";
  }
  var fields = res.fields || {};

  if (kind === "Deployment" || kind === "StatefulSet" || kind === "DaemonSet") {
    var replicasF = fields["spec.replicas"];
    var replicasVal = replicasF ? (replicasF.value || (replicasF.raw || "")) : "1";
    var replicasWired = replicasF && replicasF.from ? "params." + replicasF.from : "";
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
        var m = /app:\s*([a-zA-Z0-9_-]+)/.exec(f.raw);
        if (m) return m[1];
        return f.raw;
      }
      return "";
    }

    var selAppF = fields["spec.selector.matchLabels"] || fields["spec.selector.matchLabels.app"] || fields["spec.selector.matchLabels[app]"];
    var tmplAppF = fields["spec.template.metadata.labels"] || fields["spec.template.metadata.labels.app"] || fields["spec.template.metadata.labels[app]"];
    var selAppVal = extractApp(selAppF);
    var tmplAppVal = extractApp(tmplAppF);
    var appLabel = selAppVal || tmplAppVal || res.name;
    var isSynced = selAppVal && tmplAppVal && selAppVal === tmplAppVal;

    var h = '<div class="insp-sec workload-card" style="margin:10px 0;padding:10px 12px;border:1px solid var(--wire-xrd);background:var(--surface-2);border-radius:6px">' +
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
    var selAppF = fields["spec.selector"] || fields["spec.selector.app"] || fields["spec.selector[app]"];
    var selAppVal = "";
    if (selAppF) {
      if (selAppF.value) selAppVal = selAppF.value;
      else if (selAppF.raw) {
        var sm = /app:\s*([a-zA-Z0-9_-]+)/.exec(selAppF.raw);
        selAppVal = sm ? sm[1] : selAppF.raw;
      }
    }
    var portF = fields["spec.ports[0].port"];
    var portVal = portF ? (portF.value || portF.raw || "") : "80";
    var tgtPortF = fields["spec.ports[0].targetPort"];
    var tgtPortVal = tgtPortF ? (tgtPortF.value || tgtPortF.raw || "") : "80";
    var svcTypeF = fields["spec.type"];
    var svcTypeVal = svcTypeF ? (svcTypeF.value || "") : "ClusterIP";

    var candidateWorkloads = (doc.spec && doc.spec.resources || []).filter(function (r) {
      return r.name !== res.name && (r.kind === "Deployment" || r.kind === "StatefulSet" || r.kind === "DaemonSet");
    });

    var h = '<div class="insp-sec service-card" style="margin:10px 0;padding:10px 12px;border:1px solid var(--wire-status);background:var(--surface-2);border-radius:6px">' +
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
          var cwm = /app:\s*([a-zA-Z0-9_-]+)/.exec(cwMatchF.raw);
          cwApp = cwm ? cwm[1] : cw.name;
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
  var reqCount = fields.filter(function (f) { return f.required; }).length;
  h += '<div class="insp-t"><div class="k">' + esc(res.kind) +
    ' <span style="color:var(--faint);font-weight:400">' + esc(res.name) + "</span></div>" +
    '<div class="g">' + esc(meta ? meta.apiVersion : res.provider) +
    (flds ? " &#183; " + flds.total + " leaf fields &#183; " + reqCount + " required" : "") +
    "</div></div>";

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
    h += '<div class="empty">No schema found for kind ' + esc(res.kind) + ".</div>";
  } else {
    var params = paramsOf(doc);
    // Required branches (e.g. Deployment's spec.selector / spec.template):
    // must-set objects with no chain-true leaves — surfaced as rows of their
    // own so the Required view shows what a user actually has to fill.
    var branches = (flds && flds.requiredBranches || []);
    var branchRows = (filter === "req" || filter === "all")
      ? branches.map(function (b) {
          return '<div class="fld"><div class="fld-h">' +
            '<span class="n">' + esc(b.path) + '</span>' +
            '<span class="t">' + esc(b.type || "object") + '</span>' +
            '<span class="rq">req</span></div>' +
            '<div class="fld-d">required object \u2014 set its member fields (expand via All / search)</div></div>';
        }).join("")
      : "";
    var wlHtml = workloadPresetHtml(res, doc, params, otherResources);
    h += wlHtml;

    var body = branchRows +
      fields.map(function (f) { return fieldRow(res, f, params, otherResources, otherStatusMap); }).join("");
    h += body || '<div class="empty">No fields match this filter.</div>';

    // Crossplane Envelope section (if this CRD defines envelope properties)
    if (detail && detail.envelope && detail.envelope.length > 0) {
      var envRows = detail.envelope.map(function (f) {
        return envelopeFieldRow(res, f, params, otherResources, otherStatusMap);
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
        return '<div class="frow" style="margin-bottom:2px"><span class="lbl" style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis">' + esc(k) + "</span>" +
          '<span class="dg" style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis">' + esc(val) + "</span>" +
          '<button class="del" data-ann-del="' + esc(k) + '" title="Remove annotation">\u00d7</button></div>';
      }).join("") +
      '<div class="frow" style="margin-top:4px;margin-bottom:0">' +
      '<input class="tin" data-ann-key placeholder="prefix/name" style="flex:1;min-width:0">' +
      '<input class="tin" data-ann-value placeholder="value" style="flex:1;min-width:0">' +
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
}

/* ---------------- rendering: XRD ---------------- */

function renderXRD() {
  var doc = store.state.doc;
  var xrd = doc.spec && doc.spec.xrd || {};
  var params = paramsOf(doc);
  var names = Object.keys(params);

  var h = warnHtml();
  h += '<div class="insp-t"><div class="frow">' +
    '<input class="tin" id="xk" value="' + esc(xrd.kind) + '" aria-label="Kind">' +
    '<select class="tsel" id="xs" aria-label="Scope">' +
    '<option' + (xrd.scope === "Namespaced" ? " selected" : "") + ">Namespaced</option>" +
    '<option' + (xrd.scope === "Cluster" ? " selected" : "") + ">Cluster</option></select></div>" +
    '<div class="g">' + esc((xrd.plural || "") + "." + (xrd.group || "")) + " &#183; " + esc(xrd.version) + "</div></div>" +
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

  function paramDetailRow(n, p) {
    if (p.type === "object") {
      var mh = (p.properties && Object.keys(p.properties).length ? "" :
        '<div class="g" style="padding:2px 0 2px">no members \u2192 free-form map (string values); add members for a typed schema</div>');
      mh += memberTreeHtml(n, "", p.properties, 0);
      mh += '<div style="padding:3px 0 4px"><button class="btn sm" data-madd="' + esc(n + "|") + '">+ member</button></div>';
      return mh;
    }
    var h = '<div class="frow" style="margin-bottom:0">';
    if (p.type === "boolean") {
      h += '<select class="tsel" data-pdef="' + esc(n) + '" aria-label="Default value">' +
        '<option value=""' + (!p.default ? " selected" : "") + ">no default</option>" +
        '<option' + (p.default === "true" ? " selected" : "") + ">true</option>" +
        '<option' + (p.default === "false" ? " selected" : "") + ">false</option></select>";
    } else if (p.enum && p.enum.length) {
      h += '<span class="g" style="flex:1">enum: ' + esc(p.enum.join(", ")) + "</span>";
    } else {
      h += '<input class="tin" data-pdef="' + esc(n) + '" value="' + esc(p.default || "") +
        '" placeholder="default value" aria-label="Default value">';
    }
    if (p.type !== "boolean") {
      h += '<input class="tin" data-pe="' + esc(n) + '" value="' + esc((p.enum || []).join(",")) +
        '" placeholder="enum,values" title="Comma-separated allowed values" aria-label="Enum values">';
    }
    h += '<button class="del" data-pd="' + esc(n) + '" title="Delete parameter">&#215;</button></div>';
    return h;
  }

  names.forEach(function (n) {
    var p = params[n] || {};
    var fo = fanOut(doc, n);
    h += '<div class="fld"><div class="frow" style="margin-bottom:3px">' +
      '<input class="tin bold" data-pn="' + esc(n) + '" value="' + esc(n) + '" aria-label="Parameter name">' +
      '<select class="tsel" data-pt="' + esc(n) + '" aria-label="Parameter type">' +
      PARAM_TYPES.map(function (t) {
        return "<option" + (t === p.type ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<label class="g" style="display:inline-flex;align-items:center;gap:3px;font-size:11px">' +
      '<input type="checkbox" data-pr="' + esc(n) + '"' + (p.required ? " checked" : "") + ">req</label>" +
      '<span class="fan" title="Wired into ' + fo + ' field' + (fo === 1 ? "" : "s") + '">&#215;' + fo + "</span></div>" +
      paramDetailRow(n, p) + "</div>";
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
    pipeline.forEach(function (step, i) {
      var pos = step.position || "after";
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
        '<input class="tin" data-pipe-pkg="' + i + '" value="' + esc(step.package || "") + '" placeholder="xpkg.crossplane.io/... (package)" aria-label="Function package">' +
        '</div>' +
        '<div style="margin-top:4px">' +
        '<div class="dg" style="font-size:10px;margin-bottom:2px">Input YAML (optional):</div>' +
        '<textarea class="tin" data-pipe-input="' + i + '" rows="3" style="font-family:var(--mono);font-size:10px;width:100%;resize:vertical" placeholder="apiVersion: ...\nkind: ...">' + esc(step.input || "") + '</textarea>' +
        '</div>' +
        '</div>';
    });
  }

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

  var __snap = snapshotFocusedEdit();
  box.innerHTML = h;
  restoreFocusedEdit(__snap);
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

function render() {
  if (!box) return;
  renderToken++;
  var doc = store.state.doc;
  if (!doc) { box.innerHTML = '<div class="empty">No blueprint loaded.</div>'; return; }
  var sel = store.state.selectedResource;
  if (!sel || sel === "xrd") { renderXRD(); return; }
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
  });
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
  });
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
  if (entry && entry.from && kind === "value") {
    if (!confirm('This envelope field is wired from "' + entry.from + '". Overwrite the wire with a literal value?')) {
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

function onBoxClick(e) {
  var annDel = e.target.closest("[data-ann-del]");
  if (annDel) {
    var adk = annDel.getAttribute("data-ann-del");
    var selRes = selectedResource();
    if (!selRes) return;
    store.replaceDoc(function (d) {
      var r = d.spec.resources.find(function (x) { return x.name === selRes.name; });
      if (r && r.annotations) { delete r.annotations[adk]; if (!Object.keys(r.annotations).length) delete r.annotations; }
    });
    return;
  }
  if (e.target.closest("[data-ann-add]")) {
    var keyEl = box.querySelector("[data-ann-key]");
    var valEl = box.querySelector("[data-ann-value]");
    var selRes2 = selectedResource();
    if (!selRes2 || !keyEl || !keyEl.value.trim()) return;
    var annKey = keyEl.value.trim(), annVal = (valEl && valEl.value) || "";
    op(function () {
      return store.replaceDoc(function (d) {
        var r = d.spec.resources.find(function (x) { return x.name === selRes2.name; });
        if (!r) return;
        r.annotations = r.annotations || {};
        r.annotations[annKey] = { value: annVal };
      });
    });
    return;
  }
  var doc = store.state.doc;
  if (!doc) return;

  var mb = e.target.closest("button[data-m]");
  if (mb) {
    var isEnv = mb.hasAttribute("data-env");
    var path = mb.getAttribute("data-path");
    var m = mb.getAttribute("data-m");
    var res = selectedResource();
    var entry = isEnv ? (res ? envelopeEntryOf(res, path) : null) : (res ? entryOf(res, path) : null);
    if (entry && entry.from && (m === "v" || m === "r")) {
      if (!confirm('This field is wired from "' + entry.from + '". Switch modes and overwrite the wire?')) return;
    }
    var mKey = isEnv ? ("env:" + path) : path;
    uiMode[mKey] = m;
    if (m !== "w" && pendingNewParam === mKey) pendingNewParam = null;
    render();
    return;
  }

  var un = e.target.closest("[data-unwire]");
  if (un) {
    var p1 = un.getAttribute("data-unwire");
    setField(p1, null).then(function (r) { if (r !== null) delete uiMode[p1]; });
    return;
  }

  var unEnv = e.target.closest("[data-env-unwire]");
  if (unEnv) {
    var pe = unEnv.getAttribute("data-env-unwire");
    setEnvelopeField(pe, null).then(function (r) { if (r !== null) delete uiMode["env:" + pe]; });
    return;
  }

  var addMap = e.target.closest("[data-add-map-entry]");
  if (addMap) {
    pendingNewMapEntry = addMap.getAttribute("data-add-map-entry");
    render();
    return;
  }

  var mapOk = e.target.closest("[data-new-map-ok]");
  if (mapOk) {
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
    return;
  }

  var mapCancel = e.target.closest("[data-new-map-cancel]");
  if (mapCancel) {
    pendingNewMapEntry = null;
    render();
    return;
  }

  var delMap = e.target.closest("[data-del-map-entry]");
  if (delMap) {
    var delPath = delMap.getAttribute("data-del-map-entry");
    setField(delPath, null).then(function (r) {
      if (r !== null) delete uiMode[delPath];
    });
    return;
  }

  var ok = e.target.closest("[data-npok]");
  if (ok) {
    var p2 = ok.getAttribute("data-npok");
    var isEnv = p2.indexOf("env:") === 0;
    var realPath = isEnv ? p2.slice(4) : p2;
    var nameEl = box.querySelector('[data-npname="' + CSS.escape(p2) + '"]');
    var typeEl = box.querySelector('[data-nptype="' + CSS.escape(p2) + '"]');
    var name = nameEl && nameEl.value.trim();
    var type = typeEl && typeEl.value || "string";
    if (!name) return;
    op(function () { return store.addParameter(name, { type: type, required: false }); })
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
    return;
  }

  var cancel = e.target.closest("[data-npcancel]");
  if (cancel) { pendingNewParam = null; render(); return; }

  var madd = e.target.closest("[data-madd]");
  if (madd) {
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
    return;
  }

  var mdel = e.target.closest("[data-mdel]");
  if (mdel) {
    var mdelKey = mdel.getAttribute("data-mdel").split("|");
    var mdelProps = cloneProps((paramsOf(doc)[mdelKey[0]] || {}).properties);
    var mdelLoc = memberParent(mdelProps, mdelKey[1]);
    if (!mdelLoc) return;
    delete mdelLoc.parent[mdelLoc.key];
    op(function () { return commitMembers(mdelKey[0], mdelProps); })
      .then(function (r) { if (r === null) render(); });
    return;
  }

  var pd = e.target.closest("[data-pd]");
  if (pd) {
    var pn = pd.getAttribute("data-pd");
    var fo = fanOut(doc, pn);
    if (fo > 0 && !confirm('Parameter "' + pn + '" is wired into ' + fo + " field" + (fo === 1 ? "" : "s") + ". Delete it?")) return;
    op(function () { return store.deleteParameter(pn); });
    return;
  }

  if (e.target.closest("#addParamBtn")) {
    var params = paramsOf(doc);
    var base = "newParam", nm = base, i = 2;
    while (params[nm]) { nm = base + i; i++; }
    op(function () { return store.addParameter(nm, { type: "string", required: false }); });
    return;
  }

  if (e.target.closest("#addAutoReadyBtn") || (e.target.closest("#addPipeStepBtn") && box.querySelector("#pipePresetSelect") && box.querySelector("#pipePresetSelect").value === "auto-ready")) {
    op(function () {
      return store.replaceDoc(function (d) {
        d.spec.pipeline = d.spec.pipeline || [];
        d.spec.pipeline.push({
          name: "auto-ready",
          functionRef: "function-auto-ready",
          package: "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1",
          position: "after"
        });
      });
    });
    return;
  }

  if (e.target.closest("#addPipeStepBtn")) {
    var preset = (box.querySelector("#pipePresetSelect") && box.querySelector("#pipePresetSelect").value) || "custom";
    var newStep = { name: "custom-step", functionRef: "function-custom", package: "xpkg.crossplane.io/crossplane-contrib/function-custom:v0.1.0", position: "after" };
    if (preset === "environment-configs") {
      newStep = {
        name: "environment-configs",
        functionRef: "function-environment-configs",
        package: "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0",
        position: "before",
        input: "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Reference\n    ref:\n      name: default"
      };
    } else if (preset === "cel-filter") {
      newStep = {
        name: "cel-filter",
        functionRef: "function-cel-filter",
        package: "xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.3.0",
        position: "after"
      };
    } else if (preset === "extra-resources") {
      newStep = {
        name: "extra-resources",
        functionRef: "function-extra-resources",
        package: "xpkg.crossplane.io/crossplane-contrib/function-extra-resources:v0.3.0",
        position: "before"
      };
    }
    op(function () {
      return store.replaceDoc(function (d) {
        d.spec.pipeline = d.spec.pipeline || [];
        d.spec.pipeline.push(newStep);
      });
    });
    return;
  }

  var pipeDel = e.target.closest("[data-pipe-del]");
  if (pipeDel) {
    var pidx = parseInt(pipeDel.getAttribute("data-pipe-del"), 10);
    op(function () {
      return store.replaceDoc(function (d) {
        if (!d.spec.pipeline) return;
        d.spec.pipeline.splice(pidx, 1);
        if (!d.spec.pipeline.length) delete d.spec.pipeline;
      });
    });
    return;
  }

  var syncBtn = e.target.closest("[data-wl-sync-app]");
  if (syncBtn) {
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
        r.fields["spec.selector.matchLabels"] = { raw: "{app: " + val + "}" };
        r.fields["spec.template.metadata.labels"] = { raw: "{app: " + val + "}" };
        if (!r.fields["spec.template.spec.containers[0].name"]) {
          r.fields["spec.template.spec.containers[0].name"] = { value: rname };
        }
      });
    });
    return;
  }

  var svcMatchBtn = e.target.closest("[data-svc-match-wl]");
  if (svcMatchBtn) {
    var rname2 = svcMatchBtn.getAttribute("data-svc-match-wl");
    var matchApp = svcMatchBtn.getAttribute("data-match-app") || "";
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname2; });
        if (!r) return;
        r.fields = r.fields || {};
        delete r.fields["spec.selector.app"];
        r.fields["spec.selector"] = { raw: "{app: " + matchApp + "}" };
        r.fields["spec.ports[0].port"] = { raw: "8080" };
      });
    });
    return;
  }

  var stdLblBtn = e.target.closest("[data-apply-std-labels]");
  if (stdLblBtn) {
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
    return;
  }

  var extNameBtn = e.target.closest("[data-apply-ext-name]");
  if (extNameBtn) {
    var rname4 = extNameBtn.getAttribute("data-apply-ext-name");
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname4; });
        if (!r) return;
        r.annotations = r.annotations || {};
        r.annotations["crossplane.io/external-name"] = { raw: "'{{ $xr }}-" + rname4 + "'" };
      });
    });
    return;
  }
}

function onBoxChange(e) {
  var t = e.target;
  if (!t) return;
  var doc = store.state.doc;
  if (e.target.matches("[data-when-param],[data-when-op],[data-when-val]")) {
    var wrn = e.target.getAttribute("data-when-param") ||
      e.target.getAttribute("data-when-op") || e.target.getAttribute("data-when-val");
    var expr = whenFromControls(box, wrn);
    store.replaceDoc(function (d) {
      var r = d.spec.resources.find(function (x) { return x.name === wrn; });
      if (!r) return;
      if (expr) r.when = expr; else delete r.when;
    });
    return;
  }
  if (e.target.matches("select[data-foreach]")) {
    var rn = e.target.getAttribute("data-foreach");
    var val = e.target.value;
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
          r.fields["spec.selector.matchLabels"] = { raw: "{app: " + wlAppVal + "}" };
          r.fields["spec.template.metadata.labels"] = { raw: "{app: " + wlAppVal + "}" };
        });
      });
    }
    return;
  }

  if (t.hasAttribute("data-wl-replicas")) {
    var wlRepRname = t.getAttribute("data-wl-replicas");
    var wlRepVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === wlRepRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (wlRepVal) r.fields["spec.replicas"] = { value: wlRepVal };
        else delete r.fields["spec.replicas"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-wl-image")) {
    var wlImgRname = t.getAttribute("data-wl-image");
    var wlImgVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === wlImgRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (wlImgVal) r.fields["spec.template.spec.containers[0].image"] = { value: wlImgVal };
        else delete r.fields["spec.template.spec.containers[0].image"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-wl-cname")) {
    var wlCnRname = t.getAttribute("data-wl-cname");
    var wlCnVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === wlCnRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (wlCnVal) r.fields["spec.template.spec.containers[0].name"] = { value: wlCnVal };
        else delete r.fields["spec.template.spec.containers[0].name"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-wl-cport")) {
    var wlCpRname = t.getAttribute("data-wl-cport");
    var wlCpVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === wlCpRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (wlCpVal) r.fields["spec.template.spec.containers[0].ports[0].containerPort"] = { value: wlCpVal };
        else delete r.fields["spec.template.spec.containers[0].ports[0].containerPort"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-svc-app")) {
    var svcAppRname = t.getAttribute("data-svc-app");
    var svcAppVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === svcAppRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (svcAppVal) r.fields["spec.selector.app"] = { value: svcAppVal };
        else delete r.fields["spec.selector.app"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-svc-port")) {
    var svcPortRname = t.getAttribute("data-svc-port");
    var svcPortVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === svcPortRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (svcPortVal) r.fields["spec.ports[0].port"] = { value: svcPortVal };
        else delete r.fields["spec.ports[0].port"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-svc-tgtport")) {
    var svcTgtRname = t.getAttribute("data-svc-tgtport");
    var svcTgtVal = t.value.trim();
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === svcTgtRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (svcTgtVal) r.fields["spec.ports[0].targetPort"] = { value: svcTgtVal };
        else delete r.fields["spec.ports[0].targetPort"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-svc-type")) {
    var svcTypeRname = t.getAttribute("data-svc-type");
    var svcTypeVal = t.value;
    op(function () {
      return store.replaceDoc(function (d) {
        var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === svcTypeRname; });
        if (!r) return;
        r.fields = r.fields || {};
        if (svcTypeVal) r.fields["spec.type"] = { value: svcTypeVal };
        else delete r.fields["spec.type"];
      });
    });
    return;
  }

  if (t.hasAttribute("data-v")) { commitValue(t.getAttribute("data-v"), "value", t.value); return; }
  if (t.hasAttribute("data-raw")) { commitValue(t.getAttribute("data-raw"), "raw", t.value); return; }
  if (t.hasAttribute("data-env-v")) { commitEnvelopeValue(t.getAttribute("data-env-v"), "value", t.value); return; }
  if (t.hasAttribute("data-env-raw")) { commitEnvelopeValue(t.getAttribute("data-env-raw"), "raw", t.value); return; }

  if (t.hasAttribute("data-wire")) {
    var path = t.getAttribute("data-wire");
    var v = t.value;
    if (v === "__new__") { pendingNewParam = path; render(); return; }
    if (!v) return;
    var fromVal = (v.indexOf("params.") === 0 || v.indexOf("resources.") === 0) ? v : ("params." + v);
    setField(path, { from: fromVal, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete uiMode[path]; pendingNewParam = null; } });
    return;
  }

  if (t.hasAttribute("data-env-wire")) {
    var path = t.getAttribute("data-env-wire");
    var v = t.value;
    if (v === "__new__") { pendingNewParam = "env:" + path; render(); return; }
    if (!v) return;
    var fromVal = (v.indexOf("params.") === 0 || v.indexOf("resources.") === 0) ? v : ("params." + v);
    setEnvelopeField(path, { from: fromVal, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete uiMode["env:" + path]; pendingNewParam = null; } });
    return;
  }

  if (t.hasAttribute("data-pn")) {
    var oldName = t.getAttribute("data-pn"), newName = t.value.trim();
    if (!newName || newName === oldName) { render(); return; }
    op(function () { return store.renameParameter(oldName, newName); })
      .then(function (r) { if (r === null) render(); });
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
    if (mAttr === "data-mname") {
      var mNew = t.value.trim();
      if (!mNew || mNew === mLoc.key) { render(); return; }
      if (mLoc.parent[mNew]) { render(); return; } // duplicate name: keep the old
      mLoc.parent[mNew] = mLoc.parent[mLoc.key];
      delete mLoc.parent[mLoc.key];
    } else if (mAttr === "data-mtype") {
      mLoc.parent[mLoc.key].type = t.value;
      if (t.value !== "object") delete mLoc.parent[mLoc.key].properties;
      if (t.value === "object") { delete mLoc.parent[mLoc.key].default; delete mLoc.parent[mLoc.key].enum; }
    } else if (mAttr === "data-mreq") {
      mLoc.parent[mLoc.key].required = t.checked;
    } else {
      if (t.value) mLoc.parent[mLoc.key].default = t.value;
      else delete mLoc.parent[mLoc.key].default;
    }
    op(function () { return commitMembers(mParam, mProps); })
      .then(function (r) { if (r === null) render(); });
    return;
  }

  var params = paramsOf(doc);
  if (t.hasAttribute("data-pt")) {
    var n1 = t.getAttribute("data-pt");
    op(function () { return store.updateParameter(n1, paramFrom(params[n1], { type: t.value })); })
      .then(function (r) { if (r === null) render(); });
    return;
  }
  if (t.hasAttribute("data-pr")) {
    var n2 = t.getAttribute("data-pr");
    op(function () { return store.updateParameter(n2, paramFrom(params[n2], { required: t.checked })); })
      .then(function (r) { if (r === null) render(); });
    return;
  }
  if (t.hasAttribute("data-pdef")) {
    var n3 = t.getAttribute("data-pdef");
    op(function () { return store.updateParameter(n3, paramFrom(params[n3], { default: t.value })); })
      .then(function (r) { if (r === null) render(); });
    return;
  }
  if (t.hasAttribute("data-pe")) {
    var n4 = t.getAttribute("data-pe");
    var vals = t.value.split(",").map(function (s) { return s.trim(); }).filter(Boolean);
    op(function () { return store.updateParameter(n4, paramFrom(params[n4], { enum: vals.length ? vals : null })); })
      .then(function (r) { if (r === null) render(); });
    return;
  }

  if (t.id === "xk") {
    var kv = t.value.trim();
    if (!kv) { render(); return; }
    op(function () {
      return store.replaceDoc(function (d) { d.spec.xrd.kind = kv; });
    }).then(function (r) { if (r === null) render(); });
    return;
  }
  if (t.id === "xs") {
    var sv = t.value;
    op(function () {
      return store.replaceDoc(function (d) { d.spec.xrd.scope = sv; });
    }).then(function (r) { if (r === null) render(); });
    return;
  }

  if (t.hasAttribute("data-pipe-name") || t.hasAttribute("data-pipe-pos") || t.hasAttribute("data-pipe-fn") || t.hasAttribute("data-pipe-pkg") || t.hasAttribute("data-pipe-input")) {
    var pidx2 = parseInt(t.getAttribute("data-pipe-name") || t.getAttribute("data-pipe-pos") || t.getAttribute("data-pipe-fn") || t.getAttribute("data-pipe-pkg") || t.getAttribute("data-pipe-input"), 10);
    var attr = t.hasAttribute("data-pipe-name") ? "name"
      : t.hasAttribute("data-pipe-pos") ? "position"
      : t.hasAttribute("data-pipe-fn") ? "functionRef"
      : t.hasAttribute("data-pipe-pkg") ? "package" : "input";
    var pval = t.value;
    op(function () {
      return store.replaceDoc(function (d) {
        if (!d.spec.pipeline || !d.spec.pipeline[pidx2]) return;
        if (attr === "input" && !pval.trim()) {
          delete d.spec.pipeline[pidx2].input;
        } else {
          d.spec.pipeline[pidx2][attr] = pval;
        }
      });
    }).then(function (r) { if (r === null) render(); });
    return;
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
  if (fseg) fseg.addEventListener("click", onFsegClick);

  var lastSourcesSig = "";
  store.subscribe("doc", function () {
    var d = store.state.doc;
    var sig = ((d && d.spec && d.spec.sources) || [])
      .map(function (s) { return s.provider; }).join("|");
    if (sig !== lastSourcesSig) {
      lastSourcesSig = sig;
      kindsPromise = null;
    }
    render();
  });
  store.subscribe("selection", function () {
    uiMode = {};
    pendingNewParam = null;
    pendingNewMapEntry = null;
    warnMsg = null;
    render();
  });

  render();
}
