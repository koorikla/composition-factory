/**
 * Region: CANVAS. Root element: #cw (contains #wires svg and #canvas layer).
 *
 * Renders one prototype-markup card per doc resource plus the XR card,
 * draws the wire layer from listWires(doc), handles drag-to-move (positions
 * live client-side in store.state.positions), palette drag-and-drop
 * (replaceDoc adding the resource), and click-to-select (store.select).
 *
 * Contract: subscribes to store topics "doc" and "selection"; wires are
 * derived from the doc via ../wires.js. Never edits store.js/api.js/wires.js.
 *
 * Exported init(rootEl, {store, api}) is the single entry point — main.js
 * calls it once with the region root and the shared store/api (idempotent).
 */

import { store as defaultStore } from "../store.js";
import * as defaultApi from "../api.js";
import { listWires, fanOut, parseFrom } from "../wires.js";

const XR_ID = "xrd"; // store.selectedResource / positions key for the composite node

let S = defaultStore;
let A = defaultApi;
let cwEl = null;      // #cw wrapper (drop target, wire coordinate frame)
let canvasEl = null;  // #canvas node layer
let wiresEl = null;   // #wires svg

let kindsCache = null;          // /api/kinds result .kinds (array) or null
const schemaCache = new Map();  // "apiVersion|kind" -> {byPath:Object, requiredPaths:string[]}
const schemaLoading = new Set();
let inited = false;
let rafWires = 0;

/* ---------- small helpers ---------- */

function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function shortPath(p) {
  const seg = String(p).split(".");
  return seg.length <= 2 ? p : "…" + seg.slice(-2).join(".");
}

/** Provider family for the prototype's color scheme. */
function famOf(provider) {
  if (!provider) return "k8s";
  if (provider.indexOf("kubernetes") >= 0) return "k8s";
  return "aws";
}
const COLORS = { aws: "var(--wire-ref)", k8s: "var(--wire-status)", xrd: "var(--wire-xrd)" };

/** Prototype slug: CamelCase -> camel-case. */
function slug(k) {
  return String(k).replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}

function doc() { return S.state.doc; }

/** Which of the exactly-one-of forms a field uses ("" means absent). */
function formOf(f) {
  if (!f) return null;
  if (typeof f.from === "string" && f.from) return "from";
  if (typeof f.raw === "string" && f.raw) return "raw";
  return "value";
}

/* ---------- kind metadata + field schemas (best-effort, for type labels) ---------- */

/** Match a doc resource to a /api/kinds entry (kind + provider, prefer namespaced). */
function kindMeta(resource) {
  if (!kindsCache) return null;
  const matches = kindsCache.filter(function (k) { return k.kind === resource.kind; });
  if (!matches.length) return null;
  const withProv = matches.filter(function (k) { return k.provider === resource.provider; });
  const pool = withProv.length ? withProv : matches;
  const ns = pool.filter(function (k) { return k.namespaced; });
  return (ns.length ? ns : pool)[0];
}

function schemaFor(resource) {
  const meta = kindMeta(resource);
  if (!meta) return null;
  const key = meta.apiVersion + "|" + meta.kind;
  if (schemaCache.has(key)) return schemaCache.get(key);
  if (!schemaLoading.has(key)) {
    schemaLoading.add(key);
    A.getKindFields(meta.apiVersion, meta.kind).then(function (res) {
      const byPath = {};
      const requiredPaths = [];
      (res.fields || []).forEach(function (f) {
        byPath[f.path] = f;
        if (f.required) requiredPaths.push(f.path);
      });
      schemaCache.set(key, { byPath: byPath, requiredPaths: requiredPaths });
      render();
    }).catch(function () { /* type labels stay blank; not fatal */ });
  }
  return null;
}

/* ---------- default layout ---------- */

function ensurePositions(d) {
  if (!S.getPosition(XR_ID)) S.setPosition(XR_ID, { x: 36, y: 48 });
  const resources = d.spec && d.spec.resources || [];
  resources.forEach(function (r, i) {
    if (!S.getPosition(r.name)) {
      S.setPosition(r.name, {
        x: 330 + (i % 2) * 258,
        y: 40 + Math.floor(i / 2) * 200,
      });
    }
  });
}

/* ---------- rendering ---------- */

function portRow(owner, path, opts) {
  // opts: {dir, dotColor, req, ty, label, title, fan, cls}
  return '<div class="port' + (opts.req ? " req" : "") + (opts.cls ? " " + opts.cls : "") + '"' +
    ' data-owner="' + esc(owner) + '" data-path="' + esc(path) + '"' +
    ' title="' + esc(opts.title || path) + '">' +
    '<span class="d ' + opts.dir + '" style="background:' + opts.dotColor + '"></span>' +
    '<span class="nm">' + esc(opts.label || path) + '</span>' +
    '<span class="ty">' + esc(opts.ty || "") + '</span>' +
    (opts.fan || "") + '</div>';
}

function xrCardHTML(d, sel) {
  const xrd = d.spec.xrd || {};
  const params = xrd.parameters || {};
  const pos = S.getPosition(XR_ID) || { x: 36, y: 48 };
  let h = '<div class="node' + (sel === XR_ID ? " sel" : "") + '" data-id="' + esc(XR_ID) + '"' +
    ' style="left:' + pos.x + 'px;top:' + pos.y + 'px">' +
    '<div class="node-h" style="background:var(--wire-xrd-soft)">' +
    '<span class="sw" style="background:' + COLORS.xrd + '"></span>' +
    '<span class="k">' + esc(xrd.kind || "XR") + '</span>' +
    '<span class="nm">' + esc(d.metadata && d.metadata.name || "") + '</span></div>' +
    '<div class="node-grp">' + esc((xrd.group || "") + "/" + (xrd.version || "")) + '</div>' +
    '<div class="ports">';
  Object.keys(params).forEach(function (name) {
    const p = params[name] || {};
    const n = fanOut(d, name);
    h += portRow(XR_ID, name, {
      dir: "out",
      dotColor: n > 1 ? "var(--shared)" : COLORS.xrd,
      req: !!p.required,
      ty: p.type || "",
      label: name,
      title: name + (p.description ? " — " + p.description : ""),
      fan: n > 1 ? '<span class="fan">×' + n + '</span>' : "",
    });
  });
  h += '</div><button class="node-add" data-addxr="1">+ add field</button></div>';
  return h;
}

function resourceCardHTML(d, r, sel) {
  const pos = S.getPosition(r.name) || { x: 330, y: 40 };
  const fam = famOf(r.provider);
  const meta = kindMeta(r);
  const schema = schemaFor(r);
  const grp = meta ? meta.apiVersion
    : (r.provider ? r.provider.split("/").pop() : "");

  let h = '<div class="node' + (sel === r.name ? " sel" : "") +
    (r.forEach ? " stack" : "") + '" data-id="' + esc(r.name) + '"' +
    ' style="left:' + pos.x + 'px;top:' + pos.y + 'px">' +
    '<div class="node-h" style="background:var(--surface-2)">' +
    '<span class="sw" style="background:' + COLORS[fam] + '"></span>' +
    '<span class="k">' + esc(r.kind) + '</span>' +
    '<span class="nm">' + esc(r.name) + '</span>' +
    '<button class="del" data-act="duplicate" data-res="' + esc(r.name) + '" title="Duplicate (\u2318C \u2318V)">\u29c9</button>' +
    '<button class="del" data-act="delete" data-res="' + esc(r.name) + '" title="Remove (Delete)">\u00d7</button></div>' +
    '<span data-resize data-res="' + esc(r.name) + '" title="Drag to resize \u00b7 double-click to reset"' +
    ' style="position:absolute;right:-2px;bottom:-2px;width:14px;height:14px;cursor:nwse-resize;' +
    'border-right:2px solid var(--faint);border-bottom:2px solid var(--faint);border-radius:0 0 4px 0"></span>' +
    '<div class="node-grp">' + esc(grp) + '</div>' +
    '<div class="ports">';

  const fields = r.fields || {};
  const paths = Object.keys(fields).filter(function (p) { return formOf(fields[p]); }).sort();
  const seen = {};
  paths.forEach(function (p) { seen[p] = true; });
  // Required-but-unset schema fields also get a row (prototype look: required *).
  // For kinds with hundreds of nested required schema leaves (like native Deployment),
  // only surface shallow unset fields on the card so it stays compact.
  const extra = schema ? schema.requiredPaths.filter(function (p) {
    if (seen[p]) return false;
    const sf = schema.byPath[p];
    if (schema.requiredPaths.length <= 8) return true;
    return sf && (sf.depth !== undefined ? sf.depth <= 1 : (p.split(".").length <= 2));
  }).sort() : [];

  paths.concat(extra).forEach(function (p) {
    const f = fields[p] || null;
    const form = formOf(f);
    const sf = schema ? schema.byPath[p] : null;
    const parsed = form === "from" ? parseFrom(f.from) : null;
    let dot = "var(--rule-2)";
    if (parsed) {
      if (parsed.kind === "param") {
        dot = fanOut(d, parsed.param) > 1 ? "var(--shared)" : COLORS.xrd;
      } else if (parsed.kind === "status") {
        dot = "var(--wire-status)";
      }
    }
    h += portRow(r.name, p, {
      dir: "in",
      dotColor: dot,
      req: !!(sf && sf.required),
      ty: sf ? sf.type : "",
      label: shortPath(p),
      title: p + (sf ? " \u00b7 " + sf.type + (sf.required ? " \u00b7 required" : "") : "") +
        (sf && sf.description ? "\n" + sf.description : ""),
    });
  });

  // Configured envelope fields (e.g. writeConnectionSecretToRef.name)
  const envFields = r.envelope || {};
  const envKeys = Object.keys(envFields).filter(function (p) { return formOf(envFields[p]); }).sort();
  envKeys.forEach(function (p) {
    const f = envFields[p];
    const parsed = f && f.from ? parseFrom(f.from) : null;
    let dot = "var(--rule-2)";
    if (parsed) {
      if (parsed.kind === "param") {
        dot = fanOut(d, parsed.param) > 1 ? "var(--shared)" : COLORS.xrd;
      } else if (parsed.kind === "status") {
        dot = "var(--wire-status)";
      }
    }
    h += portRow(r.name, "envelope." + p, {
      dir: "in",
      dotColor: dot,
      req: false,
      ty: "env",
      label: "env." + shortPath(p),
      title: r.name + ".envelope." + p + " (Crossplane envelope)",
    });
  });

  // Status outputs: wired paths always, plus the top atProvider leaves from
  // the schema — displayed like inputs so "object depends on object" is
  // visible before any wire exists.
  const outStatusWires = listWires(d).filter(function (w) {
    return w.kind === "status" && w.srcResource === r.name;
  });
  const seenStatus = {};
  const statusRows = [];
  outStatusWires.forEach(function (w) {
    if (seenStatus[w.srcPath]) return;
    seenStatus[w.srcPath] = true;
    statusRows.push(w.srcPath);
  });
  const schemaLeaves = statusLeavesFor(meta) || [];
  for (let si = 0; si < schemaLeaves.length && statusRows.length < STATUS_ROWS_SHOWN + Object.keys(seenStatus).length; si++) {
    const p = schemaLeaves[si];
    if (seenStatus[p]) continue;
    seenStatus[p] = true;
    statusRows.push(p);
    if (statusRows.length >= STATUS_ROWS_SHOWN && si >= STATUS_ROWS_SHOWN) break;
  }
  if (statusRows.length) {
    h += '<div class="node-grp" style="color:var(--wire-status);text-align:right">outputs</div>';
    statusRows.forEach(function (p) {
      h += portRow(r.name, "status." + p, {
        dir: "out",
        dotColor: "var(--wire-status)",
        req: false,
        ty: "",
        cls: "status",
        // outputs read right-aligned and short: the atProvider prefix is
        // noise at a glance, the full path lives in the title
        label: shortPath(p.replace(/^atProvider\./, "")),
        title: r.name + ".status." + p + " (status output \u2014 other objects can wire from this)",
      });
    });
  }
  h += '</div>';

  if (r.when || r.forEach || envKeys.length > 0) {
    h += '<div class="node-f">';
    if (envKeys.length > 0) {
      h += '<span class="pill" style="background:var(--wire-ref-soft);color:var(--wire-ref)" title="' + esc(envKeys.join(", ")) + '">envelope (' + envKeys.length + ')</span>';
    }
    if (r.forEach) {
      const fe = typeof r.forEach === "string" ? r.forEach
        : (r.forEach && r.forEach.over) ? r.forEach.over : JSON.stringify(r.forEach);
      h += '<span class="pill loop">for each</span><span>' + esc(fe) + '</span>';
    }
    if (r.when) {
      const w = typeof r.when === "string" ? r.when : JSON.stringify(r.when);
      h += '<span class="pill cond">when</span><span>' + esc(w) + '</span>';
    }
    h += '</div>';
  }
  h += '</div>';
  return h;
}

function render() {
  if (!canvasEl) return;
  const d = doc();
  if (!d) { canvasEl.innerHTML = ""; wiresEl.innerHTML = ""; return; }
  ensurePositions(d);
  const sel = S.state.selectedResource;
  let h = xrCardHTML(d, sel);
  (d.spec.resources || []).forEach(function (r) {
    h += resourceCardHTML(d, r, sel);
  });
  canvasEl.innerHTML = h;
  Object.keys(cardSizes).forEach(function (n) {
    const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(n) + '"]');
    if (el) applyCardSize(el, n);
  });
  drawWires();
  // one extra pass after layout/fonts settle
  scheduleWires();
}

/* ---------- wires ---------- */

function portPos(owner, path) {
  const el = canvasEl.querySelector(
    '.port[data-owner="' + CSS.escape(owner) + '"][data-path="' + CSS.escape(path) + '"] .d');
  if (!el) return null;
  const cw = cwEl.getBoundingClientRect();
  const r = el.getBoundingClientRect();
  return { x: r.left - cw.left + r.width / 2, y: r.top - cw.top + r.height / 2 };
}

function drawWires() {
  if (!wiresEl) return;
  const d = doc();
  if (!d) { wiresEl.innerHTML = ""; return; }
  const ws = listWires(d);
  const fans = {};
  ws.forEach(function (w) { if (w.kind === "param") fans[w.param] = (fans[w.param] || 0) + 1; });
  let s = "";
  ws.forEach(function (w) {
    let a, b, cls, col, title;
    if (w.kind === "status") {
      a = portPos(w.srcResource, "status." + w.srcPath) || portPos(w.srcResource, w.srcPath);
      b = portPos(w.resource, w.path);
      cls = "wire-status";
      col = "var(--wire-status)";
      title = esc(w.srcResource) + ".status." + esc(w.srcPath) + " \u2192 " + esc(w.resource) + "." + esc(w.path);
    } else {
      a = portPos(XR_ID, w.param);
      b = portPos(w.resource, w.path);
      const shared = fans[w.param] > 1;
      cls = shared ? "wire-shared" : "wire-xrd";
      col = shared ? "var(--shared)" : "var(--wire-xrd)";
      title = "$" + esc(w.param) + " \u2192 " + esc(w.resource) + "." + esc(w.path);
    }
    if (!a || !b) return;
    const dx = Math.max(34, Math.abs(b.x - a.x) * 0.42);
    s += '<path class="' + cls + '" d="M' + a.x + ',' + a.y +
      ' C' + (a.x + dx) + ',' + a.y + ' ' + (b.x - dx) + ',' + b.y +
      ' ' + b.x + ',' + b.y + '" stroke="' + col + '" pointer-events="stroke">' +
      '<title>' + title + '</title></path>';
  });
  wiresEl.innerHTML = s;
}

function scheduleWires() {
  if (rafWires) return;
  rafWires = requestAnimationFrame(function () { rafWires = 0; drawWires(); });
}

/* ---------- view transform: pan + zoom (slice 6) ---------- */

const view = { x: 0, y: 0, k: 1 };
const K_MIN = 0.4, K_MAX = 2.5;

function applyView() {
  canvasEl.style.transformOrigin = "0 0";
  canvasEl.style.transform =
    "translate(" + view.x + "px," + view.y + "px) scale(" + view.k + ")";
  const pct = document.getElementById("zoom-pct");
  if (pct) pct.textContent = Math.round(view.k * 100) + "%";
  drawWires(); // synchronous: wires must never lag the transform by a frame
}

/** screen point (relative to #cw) -> canvas space */
function toCanvas(sx, sy) {
  return { x: (sx - view.x) / view.k, y: (sy - view.y) / view.k };
}

function zoomAt(sx, sy, factor) {
  const k = Math.min(K_MAX, Math.max(K_MIN, view.k * factor));
  // keep the point under the cursor fixed
  view.x = sx - (k / view.k) * (sx - view.x);
  view.y = sy - (k / view.k) * (sy - view.y);
  view.k = k;
  applyView();
}

function onWheel(e) {
  if (e.target.closest("#region-output")) return;
  e.preventDefault();
  const rect = cwEl.getBoundingClientRect();
  if (e.shiftKey) {
    // shift+wheel pans (vertical delta doubles as horizontal when the
    // device only reports one axis)
    view.x -= e.deltaX || e.deltaY;
    view.y -= e.deltaX ? e.deltaY : 0;
    applyView();
  } else {
    // wheel zooms to the cursor; ctrl+wheel (trackpad pinch) too
    zoomAt(e.clientX - rect.left, e.clientY - rect.top, Math.pow(1.0015, -e.deltaY));
  }
}

function onPanDown(e) {
  // drag on empty canvas ground pans the view
  if (e.button !== 0) return;
  if (e.target.closest(".node") || e.target.closest("button") || e.target.closest("svg path")) return;
  const sx = e.clientX, sy = e.clientY, ox = view.x, oy = view.y;
  let moved = false;
  function mv(ev) {
    if (!ev.buttons) { up(); return; } // release happened while unfocused
    moved = true;
    view.x = ox + ev.clientX - sx;
    view.y = oy + ev.clientY - sy;
    applyView();
  }
  function up() {
    document.removeEventListener("pointermove", mv);
    document.removeEventListener("pointerup", up);
  }
  document.addEventListener("pointermove", mv);
  document.addEventListener("pointerup", up);
}

function buildZoomControls() {
  const bar = document.createElement("div");
  bar.id = "zoom-bar";
  bar.style.cssText = "position:absolute;right:10px;bottom:10px;display:flex;gap:4px;align-items:center;z-index:5";
  bar.innerHTML =
    '<button class="btn sm" id="zoom-out" title="Zoom out">\u2212</button>' +
    '<span id="zoom-pct" style="font-size:10px;color:var(--faint);min-width:34px;text-align:center">100%</span>' +
    '<button class="btn sm" id="zoom-in" title="Zoom in">+</button>' +
    '<button class="btn sm" id="zoom-reset" title="Reset view">\u2302</button>';
  cwEl.appendChild(bar);
  const rect = function () { const r = cwEl.getBoundingClientRect(); return { x: r.width / 2, y: r.height / 2 }; };
  bar.querySelector("#zoom-in").addEventListener("click", function () { const c = rect(); zoomAt(c.x, c.y, 1.2); });
  bar.querySelector("#zoom-out").addEventListener("click", function () { const c = rect(); zoomAt(c.x, c.y, 1 / 1.2); });
  bar.querySelector("#zoom-reset").addEventListener("click", function () { view.x = 0; view.y = 0; view.k = 1; applyView(); });
}

/* ---------- status outputs shown on cards (slice 32) ---------- */

// "apiVersion|kind" -> array of status leaf paths (atProvider first), or
// undefined while loading. Loaded lazily; cards re-render when it lands.
const statusLeafCache = {};

function statusLeavesFor(meta) {
  if (!meta) return null;
  const key = meta.apiVersion + "|" + meta.kind;
  if (key in statusLeafCache) return statusLeafCache[key];
  statusLeafCache[key] = null; // in flight
  A.getKind(meta.apiVersion, meta.kind).then(function (detail) {
    const leaves = (detail && detail.status || []).map(function (f) { return f.path; });
    // atProvider outputs first — they are what other objects depend on
    leaves.sort(function (a, b) {
      const pa = a.indexOf("atProvider") === 0 ? 0 : 1;
      const pb = b.indexOf("atProvider") === 0 ? 0 : 1;
      return pa - pb || (a < b ? -1 : 1);
    });
    statusLeafCache[key] = leaves;
    render();
  }).catch(function () { statusLeafCache[key] = []; });
  return null;
}

const STATUS_ROWS_SHOWN = 4;

/* ---------- manual card size (slice 26): client-side, like positions ---- */

const cardSizes = {}; // name -> width px; absent = automatic

function applyCardSize(el, name) {
  const w = cardSizes[name];
  if (w) { el.style.width = w + "px"; el.style.maxWidth = "none"; }
}

/* ---------- context menu (slice 25) ---------- */

function closeCtxMenu() {
  const m = document.getElementById("ctx-menu");
  if (m) m.remove();
}

function openCtxMenu(x, y, resName) {
  closeCtxMenu();
  const m = document.createElement("div");
  m.id = "ctx-menu";
  m.setAttribute("role", "menu");
  m.style.cssText = "position:fixed;left:" + x + "px;top:" + y + "px;z-index:40;" +
    "background:var(--surface);border:1px solid var(--rule-2);border-radius:6px;" +
    "box-shadow:var(--shadow-lg);padding:4px;display:flex;flex-direction:column;min-width:150px";
  [
    { label: "Duplicate", act: "duplicate" },
    { label: "Rename\u2026", act: "rename" },
    { label: "Delete", act: "delete" },
  ].forEach(function (it) {
    const b = document.createElement("button");
    b.setAttribute("role", "menuitem");
    b.textContent = it.label;
    b.style.cssText = "all:unset;cursor:pointer;padding:5px 10px;font-size:11.5px;border-radius:4px;color:var(--ink)";
    b.addEventListener("mouseenter", function () { b.style.background = "var(--sunk)"; });
    b.addEventListener("mouseleave", function () { b.style.background = ""; });
    b.addEventListener("click", function () {
      closeCtxMenu();
      const d = doc();
      const res = d && (d.spec.resources || []).find(function (r) { return r.name === resName; });
      if (!res) return;
      if (it.act === "duplicate") duplicateResource(res);
      else if (it.act === "delete") removeResource(resName);
      else if (it.act === "rename") {
        const to = window.prompt('Rename "' + resName + '" to:', resName);
        if (!to || to === resName) return;
        S.renameResource(resName, to).then(function (ok) {
          if (!ok) return;
          const p = S.getPosition(resName);
          if (p) S.setPosition(to, p);
          S.select(to);
        });
      }
    });
    m.appendChild(b);
  });
  document.body.appendChild(m);
}

function onContextMenu(e) {
  const n = e.target.closest(".node");
  closeCtxMenu();
  if (!n || n.getAttribute("data-id") === XR_ID) return; // native browser menu elsewhere
  e.preventDefault();
  const name = n.getAttribute("data-id");
  S.select(name);
  openCtxMenu(e.clientX, e.clientY, name);
}

/* ---------- interactions ---------- */

function uniqueResourceName(d, kind) {
  const base = slug(kind);
  const names = {};
  (d.spec.resources || []).forEach(function (r) { names[r.name] = true; });
  if (!names[base]) return base;
  let i = 2;
  while (names[base + "-" + i]) i++;
  return base + "-" + i;
}

function uniqueParamName(d) {
  const params = d.spec.xrd && d.spec.xrd.parameters || {};
  let i = 1, n = "newField";
  while (params[n]) { i++; n = "newField" + i; }
  return n;
}

/* ---------- duplicate / remove (slice 4) ---------- */

let copiedResource = null; // internal copy buffer, not the system clipboard

function uniqueCopyName(d, name) {
  const names = {};
  (d.spec.resources || []).forEach(function (r) { names[r.name] = true; });
  const base = name.replace(/-\d+$/, "");
  let i = 2;
  while (names[base + "-" + i]) i++;
  return base + "-" + i;
}

function duplicateResource(src) {
  const d = doc();
  if (!d) return;
  const copyName = uniqueCopyName(d, src.name);
  S.replaceDoc(function (draft) {
    const dupe = JSON.parse(JSON.stringify(
      draft.spec.resources.find(function (r) { return r.name === src.name; }) || src));
    dupe.name = copyName;
    draft.spec.resources.push(dupe);
  }).then(function (ok) {
    if (!ok) return;
    const p = S.getPosition(src.name);
    if (p) S.setPosition(copyName, { x: p.x + 28, y: p.y + 28 });
    S.select(copyName);
  });
}

function removeResource(name) {
  const d = doc();
  if (!d) return;
  const res = (d.spec.resources || []).find(function (r) { return r.name === name; });
  if (!res) return;
  const wired = Object.keys(res.fields || {}).filter(function (k) { return res.fields[k] && res.fields[k].from; });
  if (wired.length &&
      !window.confirm('Remove "' + name + '"? Wired fields will be dropped: ' + wired.join(", "))) return;
  S.replaceDoc(function (draft) {
    draft.spec.resources = draft.spec.resources.filter(function (r) { return r.name !== name; });
  }).then(function (ok) { if (ok) S.select(null); });
}

function onKeyDown(e) {
  const t = e.target;
  if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
  if (String(window.getSelection && window.getSelection())) return; // real text copy wins
  const sel = S.state.selectedResource;
  if (!sel || sel === XR_ID) return;
  const d = doc();
  const res = d && (d.spec.resources || []).find(function (r) { return r.name === sel; });
  const mod = e.metaKey || e.ctrlKey;
  if (mod && e.key === "c" && res) { copiedResource = JSON.parse(JSON.stringify(res)); }
  else if (mod && e.key === "v" && copiedResource) { e.preventDefault(); duplicateResource(copiedResource); }
  else if ((e.key === "Delete" || e.key === "Backspace") && res) { e.preventDefault(); removeResource(sel); }
}

function onCanvasClick(e) {
  const act = e.target.closest("[data-act]");
  if (act) {
    const rn = act.getAttribute("data-res");
    const d = doc();
    const res = d && (d.spec.resources || []).find(function (x) { return x.name === rn; });
    if (!res) return;
    if (act.getAttribute("data-act") === "delete") removeResource(rn);
    else duplicateResource(res);
    return;
  }
  if (e.target.closest("[data-addxr]")) {
    const d = doc();
    if (!d) return;
    S.select(XR_ID);
    S.addParameter(uniqueParamName(d), { type: "string", required: false });
    return;
  }
  const n = e.target.closest(".node");
  if (n) S.select(n.getAttribute("data-id"));
}

function onResizeDown(e) {
  const grip = e.target.closest("[data-resize]");
  if (!grip || e.button !== 0) return;
  e.preventDefault();
  e.stopPropagation();
  const name = grip.getAttribute("data-res");
  const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(name) + '"]');
  if (!el) return;
  const startW = el.getBoundingClientRect().width / view.k;
  const sx = e.clientX;
  function mv(ev) {
    if (!ev.buttons) { up(); return; } // release happened while unfocused
    const w = Math.max(198, startW + (ev.clientX - sx) / view.k);
    cardSizes[name] = Math.round(w);
    el.style.width = cardSizes[name] + "px";
    el.style.maxWidth = "none";
    scheduleWires();
  }
  function up() {
    document.removeEventListener("pointermove", mv);
    document.removeEventListener("pointerup", up);
  }
  document.addEventListener("pointermove", mv);
  document.addEventListener("pointerup", up);
}

function onPointerDown(e) {
  if (e.target.closest("[data-resize]")) { onResizeDown(e); return; }
  if (e.button !== undefined && e.button !== 0) return;
  // A press on an action button is a click, never a drag: entering the drag
  // path re-selects and re-renders the card mid-press, destroying the very
  // button under the pointer (its click then never fires), and any micro-
  // movement during the press turns it into a card drag instead.
  if (e.target.closest("[data-act]") || e.target.closest("button")) return;

  const nodeEl = e.target.closest(".node");
  if (!nodeEl) return;
  const name = nodeEl.getAttribute("data-id");
  if (name) S.select(name);

  const h = e.target.closest(".node-h");
  if (!h) return;
  const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(name) + '"]');
  if (!el) return;
  const start = S.getPosition(name) || { x: el.offsetLeft, y: el.offsetTop };
  const sx = e.clientX, sy = e.clientY;
  let lx = start.x, ly = start.y;

  function mv(ev) {
    if (!ev.buttons) { up(); return; } // release happened while unfocused
    lx = Math.max(4, start.x + (ev.clientX - sx) / view.k);
    ly = Math.max(4, start.y + (ev.clientY - sy) / view.k);
    el.style.left = lx + "px";
    el.style.top = ly + "px";
    scheduleWires();
  }
  function up() {
    document.removeEventListener("pointermove", mv);
    document.removeEventListener("pointerup", up);
    document.removeEventListener("pointercancel", up);
    S.setPosition(name, { x: lx, y: ly }); // client-side only, recorded on release
    drawWires();
  }
  document.addEventListener("pointermove", mv);
  document.addEventListener("pointerup", up);
  document.addEventListener("pointercancel", up);
  e.preventDefault();
}

/* ---------- palette drop ---------- */

function parseDropPayload(dt) {
  let raw = "";
  try { raw = dt.getData("application/json") || dt.getData("text/plain") || ""; } catch (_) { }
  raw = raw.trim();
  if (!raw) return null;
  if (raw[0] === "{") {
    try {
      const o = JSON.parse(raw);
      if (o && o.kind) return { kind: o.kind, apiVersion: o.apiVersion || o.av || null, provider: o.provider || null };
    } catch (_) { /* fall through */ }
  }
  // "apiVersion/kind"? kind names have no "/", apiVersions always do.
  const i = raw.lastIndexOf("/");
  if (i > 0 && raw.indexOf(".") >= 0 && raw.indexOf(".") < i) {
    return { kind: raw.slice(i + 1), apiVersion: raw.slice(0, i), provider: null };
  }
  return { kind: raw, apiVersion: null, provider: null };
}

function resolveDropKind(pl) {
  if (!pl || !pl.kind) return null;
  if (kindsCache) {
    let matches = kindsCache.filter(function (k) { return k.kind === pl.kind; });
    if (pl.apiVersion) {
      const byAv = matches.filter(function (k) { return k.apiVersion === pl.apiVersion; });
      if (byAv.length) matches = byAv;
    }
    if (matches.length) {
      const ns = matches.filter(function (k) { return k.namespaced; });
      return ns.length ? ns[0] : matches[0];
    }
  }
  return { kind: pl.kind, provider: pl.provider || "", apiVersion: pl.apiVersion || "" };
}

function onDragOver(e) {
  const types = e.dataTransfer && e.dataTransfer.types || [];
  const ok = [].some.call(types, function (t) { return t === "text/plain" || t === "application/json"; });
  if (!ok) return;
  e.preventDefault();
  e.dataTransfer.dropEffect = "copy";
  cwEl.style.outline = "2px dashed var(--wire-xrd)";
  cwEl.style.outlineOffset = "-3px";
}

function onDragLeave(e) {
  if (e.target === cwEl) cwEl.style.outline = "";
}

function onDrop(e) {
  cwEl.style.outline = "";
  const pl = parseDropPayload(e.dataTransfer);
  if (!pl) return;
  e.preventDefault();
  const entry = resolveDropKind(pl);
  const d = doc();
  if (!entry || !d) return;
  const rect = cwEl.getBoundingClientRect();
  const pt = toCanvas(e.clientX - rect.left, e.clientY - rect.top);
  const x = Math.max(4, pt.x - 90);
  const y = Math.max(4, pt.y - 16);
  const name = uniqueResourceName(d, entry.kind);
  S.setPosition(name, { x: x, y: y });
  S.select(name);
  S.replaceDoc(function (next) {
    // sources is the dependency manifest the server loads providers from at
    // startup — a dropped kind's provider must be declared there or generate
    // cannot load its CRDs after a restart. Native kinds ("k8s") are not
    // provider packages and never appear in sources.
    if (entry.provider && entry.provider !== "k8s") {
      next.spec.sources = next.spec.sources || [];
      var declared = next.spec.sources.some(function (s) { return s.provider === entry.provider; });
      if (!declared) next.spec.sources.push({ provider: entry.provider });
    }
    next.spec.resources = next.spec.resources || [];
    next.spec.resources.push({
      name: name,
      kind: entry.kind,
      provider: entry.provider || "",
      fields: {},
    });
  }).then(function (res) {
    if (res) S.select(name);
  });
}

/* ---------- init ---------- */

export function init(rootEl, deps) {
  if (inited) return;
  inited = true;
  if (deps && deps.store) S = deps.store;
  if (deps && deps.api) A = deps.api;
  cwEl = rootEl || document.getElementById("cw");
  wiresEl = cwEl.querySelector("#wires") || document.getElementById("wires");
  canvasEl = cwEl.querySelector("#canvas") || document.getElementById("canvas");

  canvasEl.addEventListener("click", onCanvasClick);
  canvasEl.addEventListener("pointerdown", onPointerDown);
  cwEl.addEventListener("dragover", onDragOver);
  cwEl.addEventListener("dragleave", onDragLeave);
  cwEl.addEventListener("drop", onDrop);
  addEventListener("resize", scheduleWires);
  addEventListener("keydown", onKeyDown);
  cwEl.addEventListener("wheel", onWheel, { passive: false });
  cwEl.addEventListener("contextmenu", onContextMenu);
  canvasEl.addEventListener("dblclick", function (e) {
    const grip = e.target.closest("[data-resize]");
    if (!grip) return;
    const name = grip.getAttribute("data-res");
    delete cardSizes[name];
    const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(name) + '"]');
    if (el) { el.style.width = ""; el.style.maxWidth = ""; }
    scheduleWires();
  });
  document.addEventListener("click", function (e) { if (!e.target.closest("#ctx-menu")) closeCtxMenu(); });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeCtxMenu(); });
  cwEl.addEventListener("pointerdown", onPanDown);
  buildZoomControls();
  applyView();

  function reloadKindsAndRender() {
    A.getKinds().then(function (res) {
      kindsCache = res.kinds || [];
      render();
    }).catch(function () { render(); });
  }

  S.subscribe("doc", reloadKindsAndRender);
  // Selection must never rebuild the card DOM: a full innerHTML rebuild
  // destroys the element a rapid second click is about to land on ("can't
  // click anything" at human speed). Toggle classes in place instead.
  S.subscribe("selection", function () {
    const sel = S.state.selectedResource;
    let found = false;
    canvasEl.querySelectorAll(".node").forEach(function (el) {
      const is = el.getAttribute("data-id") === sel;
      el.classList.toggle("sel", is);
      if (is) found = true;
    });
    drawWires();
    // a selection naming a card that is not in the DOM yet (fresh add)
    // still needs the full render path
    if (sel && !found) render();
  });

  reloadKindsAndRender();
}

