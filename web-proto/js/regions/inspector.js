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
var renderToken = 0;

var kindsPromise = null;         // cached GET /api/kinds
var fieldsCache = {};            // "apiVersion|kind" -> {fields,total}
var kindDetailCache = {};        // "apiVersion|kind" -> {kind, envelope, status}

/* ---------------- helpers ---------------- */

function esc(s) {
  return String(s === undefined || s === null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

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
  Object.keys(params).forEach(function (n) {
    var p = params[n];
    if (p.type === "object" && p.properties) {
      Object.keys(p.properties).sort().forEach(function (mn) {
        if (compatible(p.properties[mn].type, fieldType)) names.push(n + "." + mn);
      });
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

  if (filter === "req" && !(f.requiredChain || f.branch || entry)) return "";
  if (filter === "set" && !entry) return "";

  var wired = m === "w" && dm === "w" && !uiMode[f.path] && entry;
  var isStatusWire = wired && entry.from && entry.from.indexOf("resources.") === 0;
  var h = '<div class="fld' + (dm === "w" && entry ? " wired" : "") + '" style="padding-left:' + (12 + (f.depth || 0) * 11) + 'px">' +
    '<div class="fld-h"><span class="n">' + esc(f.path) + '</span><span class="t">' + esc(f.type) + "</span>" +
    (f.required ? '<span class="rq">req</span>' : "") +
    modeButtons(f.path, m, false) +
    '</div><div class="fld-d">' + esc(f.description) + "</div>";

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
  return h + "</div>";
}

function envelopeFieldRow(res, f, params, otherResources, otherStatusMap) {
  // providerConfigRef is derived from the providerName parameter — the
  // engine refuses envelope entries for it, so offering rows would only
  // manufacture 400s.
  if (f.path.indexOf("providerConfigRef") === 0) return "";
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
    h += '<input class="val" data-env-v="' + esc(f.path) + '" value="' + esc((dm === "v" && entry) ? entry.value : "") +
      '" placeholder="' + (f.required ? "required &#8212; set a value or wire it" : "unset &#8212; omitted from envelope") + '">';
  }
  return h + "</div>";
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
  box.innerHTML = h;
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

  function paramDetailRow(n, p) {
    if (p.type === "object") {
      return '<div class="g" style="padding:2px 0 4px">free-form map (string values) \u2014 bind map fields like tags</div>';
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
    h += '<input class="tin" data-pe="' + esc(n) + '" value="' + esc((p.enum || []).join(",")) +
      '" placeholder="enum,values" title="Comma-separated allowed values" aria-label="Enum values">' +
      '<button class="del" data-pd="' + esc(n) + '" title="Delete parameter">&#215;</button></div>';
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
  h += '<div style="padding:8px 12px">' +
    '<button class="btn sm pri" id="addParamBtn">+ Add parameter</button></div>';
  box.innerHTML = h;
}

/* ---------------- render dispatch ---------------- */

function render() {
  renderToken++;
  if (!box) return;
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
  };
  Object.keys(patch).forEach(function (k) { p[k] = patch[k]; });
  return p;
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
}

function onBoxChange(e) {
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
  var t = e.target;
  var doc = store.state.doc;
  if (!doc) return;

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

  store.subscribe("doc", function () { kindsPromise = null; render(); });
  store.subscribe("selection", function () {
    uiMode = {};
    pendingNewParam = null;
    warnMsg = null;
    render();
  });

  render();
}
