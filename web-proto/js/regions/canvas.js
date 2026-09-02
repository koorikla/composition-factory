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
import { esc } from "../dom.js";
import { startDrag } from "../drag.js";
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

function shortPath(p) {
  const seg = String(p).split(".");
  return seg.length <= 2 ? p : "…" + seg.slice(-2).join(".");
}

/** Provider family for the prototype's color scheme. */
function famOf(provider) {
  if (!provider) return "k8s";
  const p = String(provider).toLowerCase();
  if (p.indexOf("kubernetes") >= 0 || p === "k8s") return "k8s";
  if (p.indexOf("azure") >= 0) return "azure";
  if (p.indexOf("gcp") >= 0 || p.indexOf("google") >= 0) return "gcp";
  if (p.indexOf("helm") >= 0) return "helm";
  return "aws";
}
const COLORS = {
  aws: "var(--wire-ref)",
  k8s: "var(--wire-status)",
  xrd: "var(--wire-xrd)",
  azure: "#0078d4",
  gcp: "#ea4335",
  helm: "#0f1689",
};

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
        // effective requiredness: a leaf is a must-set only when its whole
        // ancestor chain is required (requiredChain) — raw `required` floods
        // native kinds with conditional members (EnvVar.name etc.)
        if (f.requiredChain) requiredPaths.push(f.path);
      });
      (res.requiredBranches || []).forEach(function (b) {
        requiredPaths.push(b.path);
        byPath[b.path] = { path: b.path, type: b.type || "object", required: true, requiredChain: true, branch: true };
      });
      schemaCache.set(key, { byPath: byPath, requiredPaths: requiredPaths });
      render();
    }).catch(function () { /* type labels stay blank; not fatal */ });
  }
  return null;
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
      req: !!(p.requiredChain || (p.required && !p.branch)),
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
    '<span class="sw" style="background:' + (COLORS[fam] || "var(--wire-ref)") + '"></span>' +
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

  // Annotations: authored metadata entries render as rows (wire dots teal
  // when wired from status, xrd-blue from params)
  const anns = r.annotations || {};
  const annKeys = Object.keys(anns).sort();
  if (annKeys.length) {
    h += '<div class="node-grp">annotations</div>';
    annKeys.forEach(function (k) {
      const f = anns[k];
      const wired = f && typeof f.from === "string";
      h += portRow(r.name, "annotations." + k, {
        dir: "in",
        dotColor: wired && f.from.indexOf("resources.") === 0 ? "var(--wire-status)" : "var(--wire-xrd)",
        req: false,
        ty: wired ? "" : (f && f.raw !== undefined && f.raw !== "" ? "raw" : "value"),
        label: shortPath(k),
        title: k + (wired ? " \u2190 " + f.from : ""),
      });
    });
  }

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

/* ---------- dependency layout (slice 46) ----------
   Status wires are creation-order facts: a consumer of another resource's
   observed status cannot exist before its source reports, so it sits to the
   RIGHT. Layers: XR at 0; a resource's layer = 1 + max(source layers of its
   status wires). Unplaced cards get layered positions (measured widths and
   heights, no overlaps); tidy clears every stored position and re-lays. */
function dependencyLayers(d) {
  const layers = {};
  const rs = d.spec.resources || [];
  const deps = {};
  rs.forEach(function (r) { deps[r.name] = new Set(); });
  listWires(d).forEach(function (w) {
    if (w.kind === "status" && deps[w.resource] && w.srcResource !== w.resource) {
      deps[w.resource].add(w.srcResource);
    }
  });
  function layerOf(name, seen) {
    if (layers[name] !== undefined) return layers[name];
    if (seen[name]) return 1; // cycle guard: flat
    seen[name] = true;
    let l = 1;
    deps[name].forEach(function (src) { l = Math.max(l, layerOf(src, seen) + 1); });
    layers[name] = l;
    return l;
  }
  rs.forEach(function (r) { layerOf(r.name, {}); });
  return layers;
}

const autoPlaced = new Set(); // cards the layout owns until the user drags them
let lastLayoutSig = "";       // measured-size signature; re-lay only on change

function applyDependencyLayout(onlyUnplaced) {
  const d = doc();
  if (!d) return;
  const layers = dependencyLayers(d);
  const GX = 60, GY = 24, X0 = 40, Y0 = 40;
  const byLayer = {};
  (d.spec.resources || []).forEach(function (r) {
    (byLayer[layers[r.name]] = byLayer[layers[r.name]] || []).push(r.name);
  });
  // measured widths per card (fall back to 220 pre-paint)
  function width(id) {
    const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(id) + '"]');
    return el ? el.offsetWidth : 220;
  }
  function height(id) {
    const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(id) + '"]');
    return el ? el.offsetHeight : 160;
  }
  let x = X0 + width(XR_ID) + GX; // layer 1 starts right of the XR card
  const xrEl = canvasEl.querySelector('.node[data-id="' + XR_ID + '"]');
  if (!S.getPosition(XR_ID) || !onlyUnplaced) S.setPosition(XR_ID, { x: X0, y: Y0 });
  Object.keys(byLayer).map(Number).sort(function (a, b) { return a - b; }).forEach(function (L) {
    let y = Y0;
    let maxW = 0;
    byLayer[L].forEach(function (id) {
      if (onlyUnplaced && S.getPosition(id) && !autoPlaced.has(id)) {
        // user-owned card: leave it where the user put it, but RESERVE its
        // slot in this column so auto-placed siblings never stack into it
        y += height(id) + GY;
        maxW = Math.max(maxW, width(id));
        return;
      }
      autoPlaced.add(id);
      S.setPosition(id, { x: x, y: y });
      y += height(id) + GY;
      maxW = Math.max(maxW, width(id));
    });
    x += maxW + GX;
  });
}

let gestureActive = false;
let pendingRender = false;
let gestureCleanup = null; // the active drag's own up(), for forced ends

// A gesture that never sees its release (app switch mid-press, pointer
// eaten elsewhere) must not leave rendering deferred forever: window blur
// and document pointercancel force the active drag's cleanup.
function forceGestureEnd() {
  if (gestureCleanup) { const fn = gestureCleanup; gestureCleanup = null; fn(); }
}
addEventListener("blur", forceGestureEnd);
document.addEventListener("pointercancel", forceGestureEnd);

function gestureBegin(cleanup) { gestureActive = true; gestureCleanup = cleanup || null; }
function gestureEnd() {
  gestureActive = false;
  gestureCleanup = null;
  if (pendingRender) { pendingRender = false; render(); }
}

function render() {
  // Never rebuild the DOM under an active pointer gesture: replacing the
  // dragged element kills the drag mid-flight ("random mouse clutches").
  if (gestureActive) { pendingRender = true; return; }
  if (!canvasEl) return;
  const d = doc();
  if (!d) { canvasEl.innerHTML = ""; wiresEl.innerHTML = ""; return; }
  const sel = S.state.selectedResource;
  let h = xrCardHTML(d, sel);
  (d.spec.resources || []).forEach(function (r) {
    h += resourceCardHTML(d, r, sel);
  });
  canvasEl.innerHTML = h;
  // measured layout pass for cards that have no stored position
  const freshCards = (d.spec.resources || []).some(function (r) { return !S.getPosition(r.name); }) ||
    !S.getPosition(XR_ID);
  let sig = "";
  if (freshCards || autoPlaced.size > 0) {
    canvasEl.querySelectorAll(".node").forEach(function (el) {
      const id = el.getAttribute("data-id");
      if (freshCards || autoPlaced.has(id)) sig += id + ":" + el.offsetWidth + "x" + el.offsetHeight + ";";
    });
  }
  const anyUnplaced = freshCards || (autoPlaced.size > 0 && sig !== lastLayoutSig);
  lastLayoutSig = sig;
  if (anyUnplaced) {
    applyDependencyLayout(true);
    canvasEl.querySelectorAll(".node").forEach(function (el) {
      const p = S.getPosition(el.getAttribute("data-id"));
      if (p) { el.style.left = p.x + "px"; el.style.top = p.y + "px"; }
    });
  }
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

let selectedWire = null;

function wireKey(w) {
  if (!w) return "";
  return (w.kind || "") + ":" + (w.srcResource || "") + ":" + (w.srcPath || "") + ":" + (w.param || "") + ":" + (w.resource || "") + ":" + (w.path || "");
}

function deleteWire(w) {
  if (!w || !w.resource) return Promise.resolve();
  return S.replaceDoc(function (d) {
    const res = (d.spec.resources || []).find(function (r) { return r.name === w.resource; });
    if (!res) return;
    if (w.isAnnotation) {
      const key = w.path.replace(/^annotations\./, "");
      if (res.annotations && res.annotations[key]) {
        delete res.annotations[key];
        if (!Object.keys(res.annotations).length) delete res.annotations;
      }
    } else if (w.isEnvelope) {
      const envPath = w.path.replace(/^envelope\./, "");
      if (res.envelope && res.envelope[envPath]) {
        delete res.envelope[envPath];
        if (!Object.keys(res.envelope).length) delete res.envelope;
      }
    } else {
      if (res.fields && res.fields[w.path]) {
        delete res.fields[w.path];
      }
    }
  }).then(function (ok) {
    if (ok) {
      selectedWire = null;
      drawWires();
    }
  });
}

function drawWires() {
  if (!wiresEl) return;
  const d = doc();
  if (!d) { wiresEl.innerHTML = ""; return; }
  const ws = listWires(d);
  const fans = {};
  ws.forEach(function (w) { if (w.kind === "param") fans[w.param] = (fans[w.param] || 0) + 1; });
  let s = "";
  let delButtons = "";
  ws.forEach(function (w, idx) {
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
    const isSel = selectedWire && wireKey(selectedWire) === wireKey(w);
    const dx = Math.max(34, Math.abs(b.x - a.x) * 0.42);
    const dPath = 'M' + a.x + ',' + a.y +
      ' C' + (a.x + dx) + ',' + a.y + ' ' + (b.x - dx) + ',' + b.y +
      ' ' + b.x + ',' + b.y;
    s += '<path class="wire-path ' + cls + (isSel ? " wire-selected" : "") + '" d="' + dPath +
      '" stroke="' + col + '" data-wire-idx="' + idx + '" pointer-events="stroke">' +
      '<title>' + title + '</title></path>';

    if (isSel) {
      const midX = (a.x + b.x) / 2;
      const midY = (a.y + b.y) / 2;
      delButtons += '<g class="wire-del-btn" data-wire-idx="' + idx + '" transform="translate(' + midX + ',' + midY + ')" pointer-events="all">' +
        '<circle r="9"></circle>' +
        '<text y="-0.5">\u00d7</text>' +
        '<title>Delete wire (Delete/Backspace)</title>' +
        '</g>';
    }
  });
  wiresEl.innerHTML = s + delButtons;
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
  const abortDrag = startDrag(e, function mv(ev) {
    if (!ev.buttons) { abortDrag(); return; } // release happened while unfocused
    moved = true;
    view.x = ox + ev.clientX - sx;
    view.y = oy + ev.clientY - sy;
    applyView();
  }, function up() {
    gestureEnd();
  });
  gestureBegin(abortDrag);
}

function buildZoomControls() {
  const bar = document.createElement("div");
  bar.id = "zoom-bar";
  bar.style.cssText = "position:absolute;right:10px;bottom:10px;display:flex;gap:4px;align-items:center;z-index:5";
  bar.innerHTML =
    '<button class="btn sm" id="zoom-out" title="Zoom out">\u2212</button>' +
    '<span id="zoom-pct" style="font-size:10px;color:var(--faint);min-width:34px;text-align:center">100%</span>' +
    '<button class="btn sm" id="zoom-in" title="Zoom in">+</button>' +
    '<button class="btn sm" id="zoom-reset" title="Reset view">\u2302</button>' +
    '<button class="btn sm" id="layout-btn" title="Tidy: lay the dependency tree left\u2192right">\u234b</button>';
  cwEl.appendChild(bar);
  const rect = function () { const r = cwEl.getBoundingClientRect(); return { x: r.width / 2, y: r.height / 2 }; };
  bar.querySelector("#zoom-in").addEventListener("click", function () { const c = rect(); zoomAt(c.x, c.y, 1.2); });
  bar.querySelector("#zoom-out").addEventListener("click", function () { const c = rect(); zoomAt(c.x, c.y, 1 / 1.2); });
  bar.querySelector("#zoom-reset").addEventListener("click", function () { view.x = 0; view.y = 0; view.k = 1; applyView(); });
  bar.querySelector("#layout-btn").addEventListener("click", function () {
    applyDependencyLayout(false);
    canvasEl.querySelectorAll(".node").forEach(function (el) {
      const p = S.getPosition(el.getAttribute("data-id"));
      if (p) { el.style.left = p.x + "px"; el.style.top = p.y + "px"; }
    });
    drawWires();
  });
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

function openWireCtxMenu(x, y, w) {
  closeCtxMenu();
  const m = document.createElement("div");
  m.id = "ctx-menu";
  m.style.cssText = "position:fixed;left:" + x + "px;top:" + y + "px;z-index:9999;" +
    "background:var(--surface);border:1px solid var(--rule);border-radius:6px;" +
    "box-shadow:var(--shadow-lg);padding:4px 0;min-width:170px;font-size:12px;";
  const label = w.kind === "status"
    ? (w.srcResource + "." + w.srcPath + " \u2192 " + w.resource + "." + w.path)
    : ("$" + w.param + " \u2192 " + w.resource + "." + w.path);
  const header = document.createElement("div");
  header.style.cssText = "padding:5px 10px;color:var(--faint);font-size:10.5px;font-family:var(--mono);border-bottom:1px solid var(--rule);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:240px;";
  header.textContent = label;
  m.appendChild(header);

  const b = document.createElement("button");
  b.type = "button";
  b.style.cssText = "display:flex;align-items:center;justify-content:space-between;width:100%;" +
    "padding:6px 12px;background:none;border:none;color:var(--warn);cursor:pointer;font-size:12px;text-align:left;";
  b.innerHTML = '<span>Delete wire</span><span style="color:var(--faint);font-size:10.5px;font-family:var(--mono)">Del</span>';
  b.addEventListener("click", function () {
    closeCtxMenu();
    deleteWire(w);
  });
  m.appendChild(b);
  document.body.appendChild(m);
}

function onContextMenu(e) {
  closeCtxMenu();
  const delBtn = e.target.closest(".wire-del-btn");
  const wireHit = e.target.closest(".wire-hit, .wire-path");
  if (delBtn || wireHit) {
    e.preventDefault();
    const target = delBtn || wireHit;
    const idx = Number(target.getAttribute("data-wire-idx"));
    const ws = listWires(doc());
    if (ws[idx]) {
      selectedWire = ws[idx];
      drawWires();
      openWireCtxMenu(e.clientX, e.clientY, ws[idx]);
    }
    return;
  }

  const n = e.target.closest(".node");
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

  if (selectedWire && (e.key === "Delete" || e.key === "Backspace")) {
    e.preventDefault();
    const toDel = selectedWire;
    selectedWire = null;
    deleteWire(toDel);
    return;
  }
  if (selectedWire && e.key === "Escape") {
    selectedWire = null;
    drawWires();
    return;
  }

  const sel = S.state.selectedResource;
  if (!sel || sel === XR_ID) return;
  const d = doc();
  const res = d && (d.spec.resources || []).find(function (r) { return r.name === sel; });
  const mod = e.metaKey || e.ctrlKey;
  if (mod && e.key === "c" && res) { copiedResource = JSON.parse(JSON.stringify(res)); }
  else if (mod && e.key === "v" && copiedResource) { e.preventDefault(); duplicateResource(copiedResource); }
  else if ((e.key === "Delete" || e.key === "Backspace") && res) { e.preventDefault(); removeResource(sel); }
}

function onCwClick(e) {
  const delBtn = e.target.closest(".wire-del-btn");
  if (delBtn) {
    const idx = Number(delBtn.getAttribute("data-wire-idx"));
    const ws = listWires(doc());
    if (ws[idx]) {
      deleteWire(ws[idx]);
    }
    e.stopPropagation();
    return;
  }
  const wireHit = e.target.closest(".wire-hit, .wire-path");
  if (wireHit) {
    const idx = Number(wireHit.getAttribute("data-wire-idx"));
    const ws = listWires(doc());
    if (ws[idx]) {
      selectedWire = ws[idx];
      S.select(null);
      drawWires();
    }
    e.stopPropagation();
    return;
  }
  if (!e.target.closest(".node") && !e.target.closest("#wire-picker") && !e.target.closest("#ctx-menu")) {
    if (selectedWire) {
      selectedWire = null;
      drawWires();
    }
  }
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
  if (n) {
    selectedWire = null;
    S.select(n.getAttribute("data-id"));
  }
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
  const abortDrag = startDrag(e, function mv(ev) {
    if (!ev.buttons) { abortDrag(); return; } // release happened while unfocused
    const w = Math.max(198, startW + (ev.clientX - sx) / view.k);
    cardSizes[name] = Math.round(w);
    el.style.width = cardSizes[name] + "px";
    el.style.maxWidth = "none";
    scheduleWires();
  }, function up() {
    gestureEnd();
  });
  gestureBegin(abortDrag);
}

/* ---------- drag-to-wire (slice: drag-to-wire) ---------- */

function applyWire(srcOwner, srcPath, targetRes, targetPath) {
  let fromExpr = "";
  if (srcOwner === XR_ID) {
    fromExpr = "params." + srcPath;
  } else {
    fromExpr = "resources." + srcOwner + ".status." + srcPath.replace(/^status\./, "");
  }
  return S.replaceDoc(function (draft) {
    const r = (draft.spec.resources || []).find(function (x) { return x.name === targetRes; });
    if (!r) return;
    if (targetPath.indexOf("envelope.") === 0) {
      r.envelope = r.envelope || {};
      r.envelope[targetPath.slice("envelope.".length)] = { from: fromExpr };
    } else if (targetPath.indexOf("annotations.") === 0) {
      r.annotations = r.annotations || {};
      r.annotations[targetPath.slice("annotations.".length)] = { from: fromExpr };
    } else {
      r.fields = r.fields || {};
      r.fields[targetPath] = { from: fromExpr };
    }
  }).then(function (ok) {
    if (ok) {
      S.select(targetRes);
      drawWires();
    }
    return ok;
  });
}

let pickerJustOpened = false;

function closeWirePicker() {
  const el = document.getElementById("wire-picker");
  if (el) el.remove();
}

async function resolveKindMetaAsync(resource) {
  if (kindsCache && kindsCache.length) {
    const m = kindMeta(resource);
    if (m) return m;
  }
  try {
    const res = await A.getKinds();
    kindsCache = res.kinds || [];
    return kindMeta(resource) || (kindsCache.find(function (k) { return k.kind === resource.kind; }) || null);
  } catch (_) {
    return null;
  }
}

function openFieldPicker(x, y, srcOwner, srcPath, targetRes) {
  closeWirePicker();
  pickerJustOpened = true;
  setTimeout(function () { pickerJustOpened = false; }, 150);
  const d = doc();
  if (!d) return;
  const res = (d.spec.resources || []).find(function (r) { return r.name === targetRes; });
  if (!res) return;

  const pop = document.createElement("div");
  pop.id = "wire-picker";
  pop.className = "wire-picker";

  const left = Math.min(x, window.innerWidth - 370);
  const top = Math.min(y, window.innerHeight - 450);
  pop.style.left = Math.max(10, left) + "px";
  pop.style.top = Math.max(10, top) + "px";

  const srcLabel = srcOwner === XR_ID ? "$" + srcPath : srcOwner + "." + srcPath.replace(/^status\./, "");
  pop.innerHTML =
    '<div class="wire-picker-h">' +
    '<span>Wire <span style="color:var(--wire-xrd)">' + esc(srcLabel) + '</span> \u2192 ' + esc(targetRes) + '</span>' +
    '<button class="del" id="wire-picker-close" style="margin-left:auto;cursor:pointer">\u00d7</button></div>' +
    '<div style="padding:6px 10px;border-bottom:1px solid var(--rule)">' +
    '<input id="wire-picker-search" class="search" style="width:100%" placeholder="Search fields, envelope, annotations\u2026" autofocus>' +
    '</div>' +
    '<div class="wire-picker-list" id="wire-picker-list"><div class="empty">Loading fields\u2026</div></div>';

  document.body.appendChild(pop);
  pop.querySelector("#wire-picker-close").addEventListener("click", closeWirePicker);
  const searchInput = pop.querySelector("#wire-picker-search");
  const listEl = pop.querySelector("#wire-picker-list");
  if (searchInput) setTimeout(function () { searchInput.focus(); }, 20);

  let selectedIndex = 0;
  let currentItems = [];

  function renderItems(items) {
    currentItems = items;
    selectedIndex = Math.min(selectedIndex, Math.max(0, items.length - 1));
    if (!items.length) {
      listEl.innerHTML = '<div class="empty">No matching fields found.</div>';
      return;
    }
    let h = "";
    let lastCat = null;
    items.forEach(function (item, idx) {
      if (item.category !== lastCat) {
        lastCat = item.category;
        h += '<div class="wire-picker-cat">' + esc(lastCat) + '</div>';
      }
      h += '<div class="wire-picker-item' + (idx === selectedIndex ? ' active' : '') + (item.suggested ? ' match' : '') + '" data-idx="' + idx + '">' +
        '<span style="font-family:var(--mono);color:' + (item.color || 'inherit') + '">' + esc(item.label || item.path) + '</span>' +
        '<span class="dg">' + esc(item.type || "") + '</span>' +
        (item.required ? '<span class="rq">req</span>' : '') +
        (item.description ? '<span class="desc" title="' + esc(item.description) + '">' + esc(item.description) + '</span>' : '') +
        '</div>';
    });
    listEl.innerHTML = h;
  }

  function buildCandidateItems(specFields, envelopeFields, filter) {
    const q = (filter || "").toLowerCase().trim();
    const items = [];
    const srcTerm = srcPath.toLowerCase().replace(/[^a-z0-9]/g, "");

    // 1. Spec / forProvider fields
    (specFields || []).forEach(function (f) {
      const p = f.path;
      const pLower = p.toLowerCase();
      const desc = f.description || "";
      const descLower = desc.toLowerCase();
      const pNorm = pLower.replace(/[^a-z0-9]/g, "");
      if (q && pLower.indexOf(q) === -1 && descLower.indexOf(q) === -1) {
        return;
      }
      const isReq = !!(f.requiredChain || f.required);
      const isMatch = srcTerm && (pNorm.indexOf(srcTerm) >= 0 || srcTerm.indexOf(pNorm) >= 0);
      let score = 20;
      if (isReq) score += 40;
      if (isMatch) score += 100;
      if (q) {
        if (pLower === q) score += 60;
        else if (pLower.startsWith(q)) score += 40;
        else if (pLower.indexOf(q) >= 0) score += 20;
      }
      items.push({
        type: f.type || "string",
        path: p,
        label: p,
        category: isMatch && !q ? "Suggested Matches" : "Spec Fields",
        required: isReq,
        suggested: isMatch,
        description: desc,
        applyType: "field",
        score: score,
      });
    });

    // 2. Envelope fields
    (envelopeFields || []).forEach(function (ef) {
      const p = ef.path;
      const pLower = p.toLowerCase();
      const desc = ef.description || "";
      if (q && pLower.indexOf(q) === -1 && desc.toLowerCase().indexOf(q) === -1) {
        return;
      }
      const isReq = !!(ef.requiredChain || ef.required);
      let score = 10;
      if (isReq) score += 30;
      if (q) {
        if (pLower === q) score += 50;
        else if (pLower.startsWith(q)) score += 30;
        else if (pLower.indexOf(q) >= 0) score += 15;
      }
      items.push({
        type: ef.type || "string",
        path: p,
        label: "envelope." + p,
        category: "Envelope",
        required: isReq,
        suggested: false,
        description: desc,
        applyType: "envelope",
        color: "var(--wire-ref)",
        score: score,
      });
    });

    // 3. Known annotations (if relevant)
    const isK8sOrIAM = (res.provider && (res.provider.indexOf("aws") >= 0 || res.provider.indexOf("k8s") >= 0)) ||
      res.kind === "ServiceAccount" || res.kind === "Role";
    if (isK8sOrIAM) {
      const knownAnns = [
        { key: "eks.amazonaws.com/role-arn", desc: "EKS IAM Role ARN to assume" },
        { key: "crossplane.io/external-name", desc: "Cloud resource name override" },
      ];
      knownAnns.forEach(function (ann) {
        const kLower = ann.key.toLowerCase();
        const dLower = ann.desc.toLowerCase();
        if (!q || kLower.indexOf(q) >= 0 || dLower.indexOf(q) >= 0) {
          const isMatch = srcTerm.indexOf("arn") >= 0 || srcTerm.indexOf("role") >= 0;
          let score = 5;
          if (isMatch) score += 80;
          if (q && kLower.indexOf(q) >= 0) score += 25;
          items.push({
            type: "string",
            path: ann.key,
            label: "annotations." + ann.key,
            category: isMatch && !q ? "Suggested Matches" : "Annotations",
            required: false,
            suggested: isMatch,
            description: ann.desc,
            applyType: "ann",
            color: "var(--wire-status)",
            score: score,
          });
        }
      });
    }

    // 4. Custom query option if typed (always lower score than real schema fields)
    if (q) {
      if (!items.some(function (it) { return it.path === q && it.applyType === "ann"; })) {
        items.push({
          type: "string",
          path: q,
          label: "annotations." + q,
          category: "Custom",
          required: false,
          suggested: false,
          description: "Set as custom annotation",
          applyType: "ann",
          color: "var(--wire-status)",
          score: -10,
        });
      }
      if (!items.some(function (it) { return it.path === q && it.applyType === "field"; })) {
        items.push({
          type: "string",
          path: q,
          label: q,
          category: "Custom",
          required: false,
          suggested: false,
          description: "Set as custom field path",
          applyType: "field",
          score: -20,
        });
      }
    }

    items.sort(function (a, b) {
      if (b.score !== a.score) return b.score - a.score;
      return a.label.localeCompare(b.label);
    });

    return items;
  }

  let cachedSpecFields = [];
  let cachedEnvelopeFields = [];

  function updateList(q) {
    const items = buildCandidateItems(cachedSpecFields, cachedEnvelopeFields, q);
    renderItems(items);
  }

  function selectItem(item) {
    if (!item) return;
    closeWirePicker();
    if (item.applyType === "ann") {
      applyWire(srcOwner, srcPath, targetRes, "annotations." + item.path);
    } else if (item.applyType === "envelope") {
      applyWire(srcOwner, srcPath, targetRes, "envelope." + item.path);
    } else {
      applyWire(srcOwner, srcPath, targetRes, item.path);
    }
  }

  resolveKindMetaAsync(res).then(function (meta) {
    if (!meta) {
      // Fallback: check if the resource already has any fields
      cachedSpecFields = Object.keys(res.fields || {}).map(function (k) {
        return { path: k, type: "string" };
      });
      updateList(searchInput ? searchInput.value : "");
      return;
    }
    Promise.all([
      A.getKindFields(meta.apiVersion, meta.kind).catch(function () { return { fields: [] }; }),
      A.getKind(meta.apiVersion, meta.kind).catch(function () { return { envelope: [] }; })
    ]).then(function (results) {
      cachedSpecFields = (results[0] && results[0].fields) || [];
      cachedEnvelopeFields = (results[1] && results[1].envelope) || [];
      updateList(searchInput ? searchInput.value : "");
    }).catch(function (err) {
      listEl.innerHTML = '<div class="empty">Failed to load fields: ' + esc(err && err.message || err) + '</div>';
    });
  });

  function updateActiveItem() {
    listEl.querySelectorAll(".wire-picker-item").forEach(function (el, idx) {
      const isSel = idx === selectedIndex;
      el.classList.toggle("active", isSel);
      if (isSel) {
        el.scrollIntoView({ block: "nearest" });
      }
    });
  }

  if (searchInput) {
    searchInput.addEventListener("input", function () {
      selectedIndex = 0;
      updateList(searchInput.value);
    });
    searchInput.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        if (currentItems.length > 0) {
          selectedIndex = (selectedIndex + 1) % currentItems.length;
          updateActiveItem();
        }
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        if (currentItems.length > 0) {
          selectedIndex = (selectedIndex - 1 + currentItems.length) % currentItems.length;
          updateActiveItem();
        }
      } else if (e.key === "Enter") {
        e.preventDefault();
        selectItem(currentItems[selectedIndex]);
      } else if (e.key === "Escape") {
        closeWirePicker();
      }
    });
  }

  listEl.addEventListener("click", function (e) {
    const itemEl = e.target.closest(".wire-picker-item");
    if (!itemEl) return;
    const idx = parseInt(itemEl.getAttribute("data-idx"), 10);
    selectItem(currentItems[idx]);
  });
}

function onWireDragDown(e, portEl) {
  const owner = portEl.getAttribute("data-owner");
  const path = portEl.getAttribute("data-path");
  const isOut = portEl.querySelector(".d.out") !== null || (e.target.classList && e.target.classList.contains("out"));
  const dir = isOut ? "out" : "in";

  const startPt = portPos(owner, path);
  if (!startPt) return;

  const startClientX = e.clientX, startClientY = e.clientY;
  let hasMoved = false;

  let previewPath = null;
  const isStatus = path.indexOf("status.") === 0;
  const strokeColor = owner === XR_ID ? COLORS.xrd : (isStatus ? "var(--wire-status)" : COLORS.xrd);

  let lastHoverNode = null;
  let lastHoverPort = null;

  function clearHovers() {
    if (lastHoverNode) { lastHoverNode.classList.remove("wire-target-hover"); lastHoverNode = null; }
    if (lastHoverPort) { lastHoverPort.classList.remove("wire-target-hover"); lastHoverPort = null; }
  }

  function mv(ev) {
    if (!ev.buttons) { up(ev); return; }
    if (!hasMoved) {
      const dist = Math.hypot(ev.clientX - startClientX, ev.clientY - startClientY);
      if (dist < 4) return;
      hasMoved = true;
      previewPath = document.createElementNS("http://www.w3.org/2000/svg", "path");
      previewPath.id = "wire-drag-preview";
      previewPath.setAttribute("stroke", strokeColor);
      previewPath.setAttribute("fill", "none");
      previewPath.setAttribute("stroke-width", "2.5");
      wiresEl.appendChild(previewPath);
    }

    const cw = cwEl.getBoundingClientRect();
    const mousePt = { x: ev.clientX - cw.left, y: ev.clientY - cw.top };

    let a = startPt, b = mousePt;
    if (dir === "in") { a = mousePt; b = startPt; }
    const dx = Math.max(30, Math.abs(b.x - a.x) * 0.45);
    if (previewPath) {
      previewPath.setAttribute("d", "M" + a.x + "," + a.y + " C" + (a.x + dx) + "," + a.y + " " + (b.x - dx) + "," + b.y + " " + b.x + "," + b.y);
    }

    clearHovers();
    const elements = document.elementsFromPoint(ev.clientX, ev.clientY) || [];
    let p = null, n = null;
    for (let i = 0; i < elements.length; i++) {
      if (!p) p = elements[i].closest(".port");
      if (!n) n = elements[i].closest(".node");
    }
    if (p && p.getAttribute("data-owner") !== owner) {
      lastHoverPort = p;
      p.classList.add("wire-target-hover");
    } else if (n && n.getAttribute("data-id") !== owner) {
      lastHoverNode = n;
      n.classList.add("wire-target-hover");
    }
  }

  function up(ev) {
    document.removeEventListener("pointermove", mv);
    document.removeEventListener("pointerup", up);
    document.removeEventListener("pointercancel", up);
    if (previewPath) { previewPath.remove(); previewPath = null; }
    clearHovers();
    gestureEnd();

    if (!ev || !hasMoved) {
      // Just a click on the port: select the node
      if (owner) S.select(owner);
      return;
    }

    const elements = document.elementsFromPoint(ev.clientX, ev.clientY) || [];
    let targetPort = null, targetNode = null;
    for (let i = 0; i < elements.length; i++) {
      if (!targetPort) targetPort = elements[i].closest(".port");
      if (!targetNode) targetNode = elements[i].closest(".node");
    }

    if (targetPort) {
      const tOwner = targetPort.getAttribute("data-owner");
      const tPath = targetPort.getAttribute("data-path");
      if (tOwner && tOwner !== owner) {
        if (dir === "out" && tOwner !== XR_ID) {
          applyWire(owner, path, tOwner, tPath);
        } else if (dir === "in" && (tOwner === XR_ID || tPath.indexOf("status.") === 0)) {
          applyWire(tOwner, tPath, owner, path);
        }
        return;
      }
    }

    if (targetNode) {
      const tId = targetNode.getAttribute("data-id");
      if (tId && tId !== owner) {
        if (dir === "out" && tId !== XR_ID) {
          openFieldPicker(ev.clientX, ev.clientY, owner, path, tId);
        }
      }
    }
  }

  const abortDrag = startDrag(e, mv, up);
  gestureBegin(function () {
    abortDrag();
    if (previewPath) { previewPath.remove(); previewPath = null; }
    clearHovers();
  });
}

function onPointerDown(e) {
  if (e.target.closest("[data-resize]")) { onResizeDown(e); return; }
  if (e.button !== undefined && e.button !== 0) return;
  if (e.target.closest("[data-act]") || e.target.closest("button")) return;

  const portEl = e.target.closest(".port");
  if (portEl) {
    onWireDragDown(e, portEl);
    return;
  }

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
  const abortDrag = startDrag(e, mv, function up() {
    S.setPosition(name, { x: lx, y: ly }); // client-side only, recorded on release
    if (Math.abs(lx - start.x) > 3 || Math.abs(ly - start.y) > 3) {
      autoPlaced.delete(name);             // a real drag: the user owns it now
    }
    drawWires();
    gestureEnd();
  });
  gestureBegin(abortDrag);
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
      // a .yaml/.yml provider is a scanned crds: source (a CRD manifest
      // file), declared under crds:, never as a provider package
      var isCrds = /\.ya?ml$/.test(entry.provider);
      var declared = next.spec.sources.some(function (s) {
        return isCrds ? s.crds === entry.provider : s.provider === entry.provider;
      });
      if (!declared) next.spec.sources.push(isCrds ? { crds: entry.provider } : { provider: entry.provider });
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

  cwEl.addEventListener("click", onCwClick);
  if (wiresEl) {
    wiresEl.addEventListener("click", onCwClick);
    wiresEl.addEventListener("contextmenu", onContextMenu);
  }
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
  document.addEventListener("click", function (e) {
    if (!e.target.closest("#ctx-menu")) closeCtxMenu();
    if (!pickerJustOpened && !e.target.closest("#wire-picker")) closeWirePicker();
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { closeCtxMenu(); closeWirePicker(); }
  });
  cwEl.addEventListener("pointerdown", onPanDown);

  // Touch gestures for mobile: 1-finger pan & 2-finger pinch zoom
  let touchStartDist = 0;
  let touchStartCenter = { x: 0, y: 0 };
  let touchStartView = { x: 0, y: 0, k: 1 };
  let isTouching = false;

  cwEl.addEventListener("touchstart", function (e) {
    if (e.target.closest(".node") || e.target.closest("button") || e.target.closest("#zoom-bar") || e.target.closest("#wire-picker")) return;
    if (e.touches.length === 1) {
      isTouching = true;
      touchStartCenter = { x: e.touches[0].clientX, y: e.touches[0].clientY };
      touchStartView = { x: view.x, y: view.y, k: view.k };
    } else if (e.touches.length === 2) {
      isTouching = true;
      const dx = e.touches[1].clientX - e.touches[0].clientX;
      const dy = e.touches[1].clientY - e.touches[0].clientY;
      touchStartDist = Math.hypot(dx, dy) || 1;
      const rect = cwEl.getBoundingClientRect();
      touchStartCenter = {
        x: (e.touches[0].clientX + e.touches[1].clientX) / 2 - rect.left,
        y: (e.touches[0].clientY + e.touches[1].clientY) / 2 - rect.top,
      };
      touchStartView = { x: view.x, y: view.y, k: view.k };
    }
  }, { passive: false });

  cwEl.addEventListener("touchmove", function (e) {
    if (!isTouching) return;
    if (e.touches.length === 1) {
      e.preventDefault();
      const dx = e.touches[0].clientX - touchStartCenter.x;
      const dy = e.touches[0].clientY - touchStartCenter.y;
      view.x = touchStartView.x + dx;
      view.y = touchStartView.y + dy;
      applyView();
    } else if (e.touches.length === 2) {
      e.preventDefault();
      const dx = e.touches[1].clientX - e.touches[0].clientX;
      const dy = e.touches[1].clientY - e.touches[0].clientY;
      const dist = Math.hypot(dx, dy) || 1;
      const scale = dist / touchStartDist;
      const k = Math.min(K_MAX, Math.max(K_MIN, touchStartView.k * scale));
      view.x = touchStartCenter.x - (k / touchStartView.k) * (touchStartCenter.x - touchStartView.x);
      view.y = touchStartCenter.y - (k / touchStartView.k) * (touchStartCenter.y - touchStartView.y);
      view.k = k;
      applyView();
    }
  }, { passive: false });

  cwEl.addEventListener("touchend", function (e) {
    if (e.touches.length === 0) isTouching = false;
  });

  buildZoomControls();
  applyView();

  function reloadKindsAndRender() {
    A.getKinds().then(function (res) {
      kindsCache = res.kinds || [];
      render();
    }).catch(function () { render(); });
  }

  // The kinds list only changes when the doc's SOURCES change (a provider
  // added/removed). Refetching on every doc emit made each field edit spawn
  // a cascade of async renders — full DOM rebuilds that ate the user's next
  // clicks once several providers were loaded.
  let lastSourcesSig = "";
  S.subscribe("doc", function () {
    const d = doc();
    if (selectedWire) {
      const ws = listWires(d);
      const exists = ws.some(function (w) { return wireKey(w) === wireKey(selectedWire); });
      if (!exists) selectedWire = null;
    }
    const sig = ((d && d.spec && d.spec.sources) || [])
      .map(function (s) { return s.provider; }).join("|");
    if (sig !== lastSourcesSig) {
      lastSourcesSig = sig;
      reloadKindsAndRender();
    } else {
      render();
    }
  });
  // Selection must never rebuild the card DOM: a full innerHTML rebuild
  // destroys the element a rapid second click is about to land on ("can't
  // click anything" at human speed). Toggle classes in place instead.
  S.subscribe("selection", function () {
    const sel = S.state.selectedResource;
    if (sel && selectedWire) selectedWire = null;
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

