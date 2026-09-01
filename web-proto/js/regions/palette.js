/**
 * Region: PALETTE (left rail). Root element: #region-palette (see index.html).
 *
 * Tabs (prototype-exact):
 *   KINDS   — live /api/kinds grouped by group; server-side search (?q=);
 *             per-kind required-count badge; rows draggable with a
 *             dataTransfer JSON payload {kind, apiVersion, provider}.
 *   SHARED  — XRD parameters from doc.spec.xrd.parameters, rendered with the
 *             prototype's Vars card look; badge = fan-out count (wires.js).
 *   SOURCES — doc.spec.sources refs as src-rows; add flow disabled for now.
 *
 * Exported init(rootEl, {store, api}) is the single entry point — main.js
 * calls it once with the region root and the shared store/api.
 */

import { store as defaultStore } from "../store.js";
import * as defaultApi from "../api.js";
import { fanOut } from "../wires.js";

/* Node color families, exactly as the prototype's COLORS map. */
const COLORS = { aws: "var(--wire-ref)", k8s: "var(--wire-status)" };

const HINT_KINDS =
  'Drag a kind onto the canvas. Schemas load per-kind — <span class="mono">4.5 KB</span> median.';
const HINT_SHARED =
  'Wires are explicit in the doc — <span class="mono">from: params.X</span> on a resource field. Badge = fan-out.';
const HINT_SRC =
  'Pinned by digest in <span class="mono">.cf.lock</span>. Same blueprint + same lock = same YAML, forever.';

let booted = false;

/** Prototype's esc(). */
function esc(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/** Color family for a live kind row (heuristic: provider/group naming). */
function famOf(k) {
  const g = (k && k.group || "") + " " + (k && k.provider || "");
  return /(^|[^a-z])aws|upbound\.io/.test(g) ? "aws" : "k8s";
}

/**
 * init — wire the palette region into its root element.
 * @param {HTMLElement} rootEl #region-palette
 * @param {{store: Object, api: Object}} deps
 */
export function init(rootEl, deps) {
  if (booted) return;
  booted = true;

  const store = deps && deps.store || defaultStore;
  const api = deps && deps.api || defaultApi;

  const tabsEl = rootEl.querySelector("#rtabs");
  const searchWrapEl = rootEl.querySelector("#lsearch");
  const searchEl = rootEl.querySelector("#psearch");
  const railEl = rootEl.querySelector("#lrail");
  const hintEl = rootEl.querySelector("#lhint");

  let rail = "kinds";          // "kinds" | "shared" | "src"
  let kinds = [];              // last /api/kinds result rows
  let kindsError = null;       // verbatim server message, or null
  let kindsLoaded = false;
  let searchSeq = 0;
  let debounceTimer = null;

  /* ---- kinds fetch (server-side search via api.getKinds(q)) ---- */
  function loadKinds() {
    const q = (searchEl && searchEl.value || "").trim();
    const seq = ++searchSeq;
    api.getKinds(q).then(function (d) {
      if (seq !== searchSeq) return; // stale response
      kinds = d && d.kinds || [];
      kindsError = null;
      kindsLoaded = true;
      if (rail === "kinds") drawRail();
    }, function (e) {
      if (seq !== searchSeq) return;
      kindsError = e.message;
      kindsLoaded = true;
      if (rail === "kinds") drawRail();
    });
  }

  /* ---- render ---------------------------------------------------------- */
  function drawKinds() {
    if (kindsError) return '<div class="empty">' + esc(kindsError) + "</div>";
    if (!kindsLoaded) return '<div class="empty">Loading kinds…</div>';
    if (!kinds.length) return '<div class="empty">No kinds match.</div>';
    // Group by `group`, first-appearance order (header rows like the prototype).
    const order = [];
    const byGroup = {};
    kinds.forEach(function (k) {
      const g = k.group || k.apiVersion || "";
      if (!byGroup[g]) { byGroup[g] = []; order.push(g); }
      byGroup[g].push(k);
    });
    let h = "";
    order.forEach(function (g) {
      const items = byGroup[g];
      h += '<div class="grp"><span class="lbl">' + esc(g) + '</span><span class="n">' + items.length + "</span></div>";
      items.forEach(function (k) {
        const fam = famOf(k);
        h += '<div class="kind" draggable="true"' +
          ' data-kind="' + esc(k.kind) + '"' +
          ' data-av="' + esc(k.apiVersion) + '"' +
          ' data-provider="' + esc(k.provider || "") + '"' +
          ' data-fam="' + esc(fam) + '">' +
          '<span class="sw" style="background:' + COLORS[fam] + '"></span>' +
          '<span class="nm" style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(k.kind) + '">' + esc(k.kind) + '</span>' +
          '<span class="req">' + (k.required | 0) + " req</span></div>";
      });
    });
    return h;
  }

  function paramLines(p) {
    const lines = ["type " + (p.type || "string") + (p.required ? ", required" : "")];
    if (p.enum && p.enum.length) lines.push("enum " + p.enum.join(" | "));
    if (p.default !== undefined && p.default !== "") lines.push("default " + p.default);
    if (p.description) lines.push(p.description);
    return lines.map(esc).join("\n");
  }

  function drawShared() {
    const doc = store.state.doc;
    if (!doc) return '<div class="empty">No document loaded.</div>';
    const params = doc.spec && doc.spec.xrd && doc.spec.xrd.parameters || {};
    const names = Object.keys(params);
    let h = '<div class="grp"><span class="lbl">Parameters</span><span class="n">' + names.length + "</span></div>";
    if (!names.length) h += '<div class="empty">No parameters declared.</div>';
    names.forEach(function (n) {
      h += '<div class="card"><div class="card-h">' +
        '<span class="nm" style="color:var(--shared)">$' + esc(n) + "</span>" +
        '<span class="sp"></span><span class="bind">' + fanOut(doc, n) + " bound</span></div>" +
        '<div class="card-b">' + paramLines(params[n]) + "</div></div>";
    });
    if (!paramFormOpen) {
      h += '<div style="padding:8px 10px"><button class="btn sm" id="param-add-btn">+ Add parameter</button></div>';
    } else {
      h += '<div class="card" id="param-add-form" style="padding:8px 10px;display:flex;flex-direction:column;gap:6px">' +
        '<input id="param-add-name" class="search" placeholder="parameterName" aria-label="Parameter name">' +
        '<div style="display:flex;gap:6px;align-items:center">' +
        '<select id="param-add-type" class="search" style="flex:1" aria-label="Type">' +
        ["string","integer","number","boolean","object"].map(function (t) {
          return '<option value="' + t + '">' + t + "</option>";
        }).join("") + "</select>" +
        '<label style="display:flex;gap:4px;align-items:center;font-size:10.5px;color:var(--faint)">' +
        '<input type="checkbox" id="param-add-req">required</label></div>' +
        '<div style="display:flex;gap:6px">' +
        '<button class="btn sm pri" id="param-add-submit">Add</button>' +
        '<button class="btn sm" id="param-add-cancel">Cancel</button></div></div>';
    }
    if (paramErr) h += '<div class="warnbar" role="alert" style="margin:0 10px">' + esc(paramErr) + "</div>";
    return h;
  }

  let paramFormOpen = false;
  let paramErr = null;

  let providers = null;        // server-side cached providers, null = not loaded
  let providersErr = null;     // verbatim server error from the last add/list

  function loadProviders() {
    api.getProviders().then(function (r) {
      providers = r.providers || [];
      if (rail === "src") drawRail();
    }).catch(function () {
      providers = null;        // endpoint absent or down: fall back to doc sources
      if (rail === "src") drawRail();
    });
  }

  function drawSources() {
    const doc = store.state.doc;
    if (!doc) return '<div class="empty">No document loaded.</div>';
    const sources = providers !== null
      ? providers.map(function (p) { return { provider: p.ref, digest: p.digest, kinds: p.kinds }; })
      : (doc.spec && doc.spec.sources || []);
    let h = '<div class="grp"><span class="lbl">Providers</span><span class="n">' + sources.length + "</span></div>";
    if (!sources.length) h += '<div class="empty">No sources declared.</div>';
    sources.forEach(function (s) {
      const ref = s && s.provider || "";
      const fam = /aws/.test(ref) ? "aws" : "k8s";
      const meta = (s.digest ? s.digest.slice(0, 19) : "") + (s.kinds ? " \u00b7 " + s.kinds + " kinds" : "");
      h += '<div class="src-row">' +
        '<span class="sw" style="width:5px;height:22px;border-radius:1.5px;background:' + COLORS[fam] + '"></span>' +
        '<span style="min-width:0"><span class="nm" style="display:block">' + esc(ref.split("/").pop()) + "</span>" +
        '<span class="dg">' + esc(meta || ref) + '</span></span><span class="sp"></span></div>';
    });
    h += '<div style="padding:8px 10px;display:flex;gap:6px">' +
      '<input id="src-add-ref" class="search" style="flex:1;min-width:0" placeholder="ghcr.io/\u2026/provider-x:vN" aria-label="Provider ref">' +
      '<button class="btn sm" id="src-add-btn">Add</button></div>';
    if (providersErr) h += '<div class="warnbar" role="alert" style="margin:0 10px">' + esc(providersErr) + "</div>";
    return h;
  }

  function drawGuide() {
    function sec(t, body) {
      return '<div class="grp"><span class="lbl">' + t + '</span></div>' +
        '<div style="padding:4px 12px 10px;font-size:11px;line-height:1.55;color:var(--ink-2)">' + body + "</div>";
    }
    function kbd(k) { return '<b style="font-family:var(--mono);font-size:10px">' + k + "</b>"; }
    const mod = /Mac/.test(navigator.platform) ? "\u2318" : "Ctrl";
    return sec("The loop",
        "Drag a kind from KINDS onto the canvas, wire XR parameters to resource fields, " +
        "edit values in the inspector \u2014 the generated YAML below updates live and is " +
        "written by <b>cf gen</b> byte-for-byte the same.") +
      sec("Wires",
        '<span style="color:var(--wire-xrd)">\u2500\u2500</span> XRD spec \u00b7 ' +
        '<span style="color:var(--shared)">\u2500\u2500</span> shared (one parameter feeding ' +
        "several fields) \u00b7 status and native-ref wires arrive with the engine work.") +
      sec("Keyboard",
        kbd(mod + "C") + " / " + kbd(mod + "V") + " copy &amp; paste to duplicate a resource \u00b7 " +
        kbd("Delete") + " remove (confirms when wires would drop) \u00b7 " +
        "wheel zooms to the cursor, " + kbd("Shift+wheel") + " pans (or drag the empty ground), " + kbd("\u2302") + " resets.") +
      sec("Validate",
        "Runs a real <b>crossplane composition render</b> against a sample XR synthesized " +
        "from your XRD \u2014 the chip reports the composed resource count or the engine's " +
        "error verbatim.") +
      sec("Files",
        "Generate writes compositions/, xrds/ and functions.yaml to the output directory " +
        "cf serve was started with; the blueprint file is the single source of truth.");
  }

  function drawRail() {
    if (searchWrapEl) searchWrapEl.style.display = rail === "kinds" ? "" : "none";
    let h, hint;
    if (rail === "kinds") { h = drawKinds(); hint = HINT_KINDS; }
    else if (rail === "shared") { h = drawShared(); hint = HINT_SHARED; }
    else if (rail === "guide") { h = drawGuide(); hint = ""; }
    else { h = drawSources(); hint = HINT_SRC; }
    // A re-render (e.g. the providers list arriving) must not eat what the
    // user is typing into the add-provider field.
    var keepIds = ["src-add-ref", "param-add-name", "param-add-type", "param-add-req"];
    var kept = {};
    keepIds.forEach(function (id) {
      var el = railEl.querySelector("#" + id);
      if (el) kept[id] = {
        v: el.type === "checkbox" ? el.checked : el.value,
        focus: document.activeElement === el,
      };
    });
    railEl.innerHTML = h;
    keepIds.forEach(function (id) {
      var st = kept[id], el = railEl.querySelector("#" + id);
      if (!st || !el) return;
      if (el.type === "checkbox") el.checked = st.v; else el.value = st.v;
      if (st.focus) el.focus();
    });
    if (hintEl) hintEl.innerHTML = hint;
  }

  /* ---- events ---------------------------------------------------------- */
  if (tabsEl) tabsEl.addEventListener("click", function (e) {
    const b = e.target.closest("button");
    if (!b) return;
    rail = b.getAttribute("data-r");
    if (rail === "src" && providers === null) loadProviders();
    [].forEach.call(tabsEl.children, function (c) {
      c.setAttribute("aria-pressed", String(c === b));
    });
    drawRail();
  });

  if (searchEl) searchEl.addEventListener("input", function () {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(loadKinds, 150);
  });

  railEl.addEventListener("click", function (e) {
    if (e.target.closest("#param-add-btn")) { paramFormOpen = true; paramErr = null; drawRail(); return; }
    if (e.target.closest("#param-add-cancel")) { paramFormOpen = false; paramErr = null; drawRail(); return; }
    if (e.target.closest("#param-add-submit")) {
      const name = (railEl.querySelector("#param-add-name") || {}).value || "";
      const type = (railEl.querySelector("#param-add-type") || {}).value || "string";
      const req = !!(railEl.querySelector("#param-add-req") || {}).checked;
      if (!name.trim()) return;
      paramErr = null;
      store.addParameter(name.trim(), { type: type, required: req }).then(function (res) {
        // the store resolves null on failure and emits the verbatim error;
        // the error subscription below paints it — keep the form open.
        if (res) { paramFormOpen = false; paramErr = null; drawRail(); }
      });
      return;
    }
    if (!e.target.closest("#src-add-btn")) return;
    const input = railEl.querySelector("#src-add-ref");
    const ref = input && input.value.trim();
    if (!ref) return;
    e.target.disabled = true;
    providersErr = null;
    api.addProvider(ref).then(function () {
      loadProviders();
      loadKinds();             // new kinds must appear in the KINDS tab
    }).catch(function (err) {
      providersErr = err && err.message || String(err);
      drawRail();
    });
  });

  railEl.addEventListener("dragstart", function (e) {
    const k = e.target.closest(".kind");
    if (!k) return;
    const payload = JSON.stringify({
      kind: k.getAttribute("data-kind"),
      apiVersion: k.getAttribute("data-av"),
      provider: k.getAttribute("data-provider"),
    });
    e.dataTransfer.effectAllowed = "copy";
    try {
      e.dataTransfer.setData("application/json", payload);
      e.dataTransfer.setData("text/plain", payload);
    } catch (_) { /* older engines */ }
  });

  /* ---- store subscriptions --------------------------------------------- */
  store.subscribe("error", function (e) {
    if (e && e.source === "addParameter") { paramErr = e.message; drawRail(); }
  });

  store.subscribe("doc", function () {
    // Fan-out counts and sources come from the doc; kinds tab is unaffected.
    if (rail !== "kinds") drawRail();
  });

  /* ---- boot ------------------------------------------------------------ */
  drawRail();   // paints "Loading kinds…" immediately
  loadKinds();
}
