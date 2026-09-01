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
    return h;
  }

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

  function drawRail() {
    if (searchWrapEl) searchWrapEl.style.display = rail === "kinds" ? "" : "none";
    let h, hint;
    if (rail === "kinds") { h = drawKinds(); hint = HINT_KINDS; }
    else if (rail === "shared") { h = drawShared(); hint = HINT_SHARED; }
    else { h = drawSources(); hint = HINT_SRC; }
    // A re-render (e.g. the providers list arriving) must not eat what the
    // user is typing into the add-provider field.
    var prev = railEl.querySelector("#src-add-ref");
    var keep = prev ? { v: prev.value, focus: document.activeElement === prev } : null;
    railEl.innerHTML = h;
    if (keep) {
      var next = railEl.querySelector("#src-add-ref");
      if (next) { next.value = keep.v; if (keep.focus) next.focus(); }
    }
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
  store.subscribe("doc", function () {
    // Fan-out counts and sources come from the doc; kinds tab is unaffected.
    if (rail !== "kinds") drawRail();
  });

  /* ---- boot ------------------------------------------------------------ */
  drawRail();   // paints "Loading kinds…" immediately
  loadKinds();
}
