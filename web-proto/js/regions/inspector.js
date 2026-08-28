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

/* ---------------- helpers ---------------- */

function esc(s) {
  return String(s === undefined || s === null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

function selectedResource() {
  var doc = store.state.doc, sel = store.state.selectedResource;
  if (!doc || !sel || sel === "xrd") return null;
  var rs = doc.spec && doc.spec.resources || [];
  for (var i = 0; i < rs.length; i++) if (rs[i].name === sel) return rs[i];
  return null;
}

/** Normalize a doc field entry: "" means absent (server pads). */
function entryOf(res, path) {
  var f = res.fields && res.fields[path];
  if (!f) return null;
  var from = typeof f.from === "string" ? f.from : "";
  var value = typeof f.value === "string" ? f.value : "";
  var raw = typeof f.raw === "string" ? f.raw : "";
  if (!from && !value && !raw) return null;
  return { from: from, value: value, raw: raw };
}

function docMode(entry) {
  if (!entry) return null;
  if (entry.from) return "w";
  if (entry.raw) return "r";
  return "v";
}

function compatible(paramType, fieldType) {
  if (fieldType === "string") return paramType === "string";
  if (fieldType === "number" || fieldType === "integer")
    return paramType === "number" || paramType === "integer";
  if (fieldType === "boolean") return paramType === "boolean";
  if (fieldType === "map" || fieldType === "object") return paramType === "object";
  return true; // unknown/array field types: don't block wiring
}

function suggestedParamType(fieldType) {
  if (fieldType === "number") return "number";
  if (fieldType === "boolean") return "boolean";
  if (fieldType === "map" || fieldType === "object") return "object";
  return "string";
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
  return m;
}

async function fieldsFor(apiVersion, kind) {
  var key = apiVersion + "|" + kind;
  if (!fieldsCache[key]) {
    fieldsCache[key] = await api.getKindFields(apiVersion, kind);
  }
  return fieldsCache[key];
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

function modeButtons(path, pressed) {
  var titles = { v: "Literal value", w: "Wire from a parameter", r: "Raw go-template" };
  return '<span class="modes">' + ["v", "w", "r"].map(function (x) {
    return '<button data-m="' + x + '" data-path="' + esc(path) + '" aria-pressed="' +
      (pressed === x) + '" title="' + titles[x] + '">' + x.toUpperCase() + "</button>";
  }).join("") + "</span>";
}

function wireSelectHtml(path, fieldType, params) {
  var names = Object.keys(params).filter(function (n) {
    return compatible(params[n].type, fieldType);
  });
  var h = '<div class="bound"><span style="color:var(--faint)">&#8592;</span>' +
    '<select class="tsel" data-wire="' + esc(path) + '" style="flex:1">' +
    '<option value="">wire to&#8230;</option>' +
    names.map(function (n) {
      return '<option value="' + esc(n) + '">params.' + esc(n) + "</option>";
    }).join("") +
    '<option value="__new__">new parameter&#8230;</option></select></div>';
  if (pendingNewParam === path) {
    h += '<div class="frow" style="margin-top:4px;margin-bottom:0">' +
      '<input class="tin" data-npname="' + esc(path) + '" placeholder="parameterName" aria-label="New parameter name">' +
      '<select class="tsel" data-nptype="' + esc(path) + '" aria-label="New parameter type">' +
      PARAM_TYPES.map(function (t) {
        return "<option" + (t === suggestedParamType(fieldType) ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<button class="btn sm" data-npok="' + esc(path) + '">Add</button>' +
      '<button class="del" data-npcancel="' + esc(path) + '" title="Cancel">&#215;</button></div>';
  }
  return h;
}

function fieldRow(res, f, params) {
  var entry = entryOf(res, f.path);
  var dm = docMode(entry);
  var m = uiMode[f.path] || dm;

  if (filter === "req" && !(f.required || entry)) return "";
  if (filter === "set" && !entry) return "";

  var wired = m === "w" && dm === "w" && !uiMode[f.path];
  var h = '<div class="fld' + (dm === "w" ? " wired" : "") + '" style="padding-left:' + (12 + (f.depth || 0) * 11) + 'px">' +
    '<div class="fld-h"><span class="n">' + esc(f.path) + '</span><span class="t">' + esc(f.type) + "</span>" +
    (f.required ? '<span class="rq">req</span>' : "") +
    modeButtons(f.path, m) +
    '</div><div class="fld-d">' + esc(f.description) + "</div>";

  if (m === "w") {
    if (dm === "w" && !uiMode[f.path]) {
      h += '<div class="bound"><span style="color:var(--faint)">&#8592;</span>' +
        '<span class="src">' + esc(entry.from) + "</span>" +
        '<span class="x" role="button" tabindex="0" data-unwire="' + esc(f.path) + '" title="Remove wire">&#215;</span></div>';
    } else {
      h += wireSelectHtml(f.path, f.type, params);
    }
  } else if (m === "r") {
    h += '<textarea class="val raw" data-raw="' + esc(f.path) + '" rows="2" placeholder="{{ }}">' +
      esc(dm === "r" ? entry.raw : "") + "</textarea>";
  } else {
    h += '<input class="val" data-v="' + esc(f.path) + '" value="' + esc(dm === "v" ? entry.value : "") +
      '" placeholder="' + (f.required ? "required &#8212; set a value or wire it" : "unset &#8212; omitted from output") + '">';
  }
  return h + "</div>";
}

async function renderResource(res) {
  var t = renderToken;
  var doc = store.state.doc;
  var meta = null, flds = null, loadErr = null;
  try {
    meta = await kindMeta(res);
    if (meta) flds = await fieldsFor(meta.apiVersion, res.kind);
  } catch (e) {
    loadErr = e && e.message || String(e);
  }
  if (t !== renderToken) return;

  var h = warnHtml();
  var fields = flds && flds.fields || [];
  var reqCount = fields.filter(function (f) { return f.required; }).length;
  h += '<div class="insp-t"><div class="k">' + esc(res.kind) +
    ' <span style="color:var(--faint);font-weight:400">' + esc(res.name) + "</span></div>" +
    '<div class="g">' + esc(meta ? meta.apiVersion : res.provider) +
    (flds ? " &#183; " + flds.total + " leaf fields &#183; " + reqCount + " required" : "") +
    "</div></div>";

  if (loadErr) {
    h += '<div class="warnbar">' + esc(loadErr) + "</div>";
  } else if (!meta) {
    h += '<div class="empty">No schema found for kind ' + esc(res.kind) + ".</div>";
  } else {
    var params = paramsOf(doc);
    var body = fields.map(function (f) { return fieldRow(res, f, params); }).join("");
    h += body || '<div class="empty">No fields match this filter.</div>';
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

  names.forEach(function (n) {
    var p = params[n];
    var fo = fanOut(doc, n);
    h += '<div class="fld' + (fo > 0 ? " wired" : "") + '"><div class="frow">' +
      '<input class="tin" data-pn="' + esc(n) + '" value="' + esc(n) + '" aria-label="Parameter name">' +
      '<select class="tsel" data-pt="' + esc(n) + '" aria-label="Type">' +
      PARAM_TYPES.map(function (ty) {
        return "<option" + (ty === p.type ? " selected" : "") + ">" + ty + "</option>";
      }).join("") + "</select>" +
      '<label class="ck"><input type="checkbox" data-pr="' + esc(n) + '"' + (p.required ? " checked" : "") + ">req</label>" +
      '<button class="del" data-pd="' + esc(n) + '" title="Delete parameter">&#215;</button></div>' +
      '<div class="frow" style="margin-bottom:0">' +
      '<input class="tin" data-pdef="' + esc(n) + '" value="' + esc(p.default || "") + '" placeholder="default" aria-label="Default value">' +
      '<input class="tin" data-pe="' + esc(n) + '" value="' + esc((p.enum || []).join(", ")) + '" placeholder="enum values, comma-separated" aria-label="Enum values"></div>' +
      (fo > 0 ? '<div class="xf">wired into ' + fo + " field" + (fo === 1 ? "" : "s") + "</div>" : "");
    h += "</div>";
  });

  h += '<div style="padding:9px 12px"><button class="btn sm" id="addParamBtn">+ Add parameter</button></div>';
  box.innerHTML = h;
}

/* ---------------- render dispatch ---------------- */

function render() {
  if (!box) return;
  renderToken++;
  var doc = store.state.doc, sel = store.state.selectedResource;
  if (!doc) { box.innerHTML = warnHtml() + '<div class="empty">Loading&#8230;</div>'; return; }
  if (!sel) { box.innerHTML = warnHtml() + '<div class="empty">Select a node.</div>'; return; }
  if (sel === "xrd") { renderXRD(); return; }
  var res = selectedResource();
  if (!res) { box.innerHTML = warnHtml() + '<div class="empty">Select a node.</div>'; return; }
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

function onBoxClick(e) {
  var doc = store.state.doc;
  if (!doc) return;

  var mb = e.target.closest("button[data-m]");
  if (mb) {
    var path = mb.getAttribute("data-path");
    var m = mb.getAttribute("data-m");
    var res = selectedResource();
    var entry = res ? entryOf(res, path) : null;
    if (entry && entry.from && (m === "v" || m === "r")) {
      if (!confirm('This field is wired from "' + entry.from + '". Switch modes and overwrite the wire?')) return;
    }
    uiMode[path] = m;
    if (m !== "w" && pendingNewParam === path) pendingNewParam = null;
    render();
    return;
  }

  var un = e.target.closest("[data-unwire]");
  if (un) {
    var p1 = un.getAttribute("data-unwire");
    setField(p1, null).then(function (r) { if (r !== null) delete uiMode[p1]; });
    return;
  }

  var ok = e.target.closest("[data-npok]");
  if (ok) {
    var p2 = ok.getAttribute("data-npok");
    var nameEl = box.querySelector('[data-npname="' + CSS.escape(p2) + '"]');
    var typeEl = box.querySelector('[data-nptype="' + CSS.escape(p2) + '"]');
    var name = nameEl && nameEl.value.trim();
    var type = typeEl && typeEl.value || "string";
    if (!name) return;
    op(function () { return store.addParameter(name, { type: type, required: false }); })
      .then(function (docAfter) {
        if (docAfter === null) return null;
        return setField(p2, { from: "params." + name, value: "", raw: "" });
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
  var t = e.target;
  var doc = store.state.doc;
  if (!doc) return;

  if (t.hasAttribute("data-v")) { commitValue(t.getAttribute("data-v"), "value", t.value); return; }
  if (t.hasAttribute("data-raw")) { commitValue(t.getAttribute("data-raw"), "raw", t.value); return; }

  if (t.hasAttribute("data-wire")) {
    var path = t.getAttribute("data-wire");
    var v = t.value;
    if (v === "__new__") { pendingNewParam = path; render(); return; }
    if (!v) return;
    setField(path, { from: "params." + v, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete uiMode[path]; pendingNewParam = null; } });
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
 * Initialize the inspector region. Idempotent — the integrator may call it
 * explicitly; importing this module also auto-initializes against the default
 * store/api and #region-inspector.
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

  store.subscribe("doc", function () { render(); });
  store.subscribe("selection", function () {
    uiMode = {};
    pendingNewParam = null;
    warnMsg = null;
    render();
  });

  render();
}

/* Auto-init on import (main.js imports region modules for side effects). */
var autoRoot = typeof document !== "undefined" && document.getElementById("region-inspector");
if (autoRoot) init(autoRoot, { store: defaultStore, api: defaultApi });
