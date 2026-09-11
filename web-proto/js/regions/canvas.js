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
import { listWires, fanOut, envFanOut, parseFrom } from "../wires.js";
import { famOf, uniqueResourceName, COLORS, getEnvConfigName } from "../utils.js";
import { switchTab } from "./palette.js";
import {
  XR_ID,
  ENV_ID,
  computeDependencyLayout,
  dependencyLayers,
} from "./canvas/layout.js";
import {
  initDragToWire,
  onWireDragDown,
  closeWirePicker,
  isPickerJustOpened,
} from "./canvas/drag-to-wire.js";

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

function isRefFieldPath(p) {
  return /Ref(\.name)?$|Refs(\[\d+\])?(\.name)?$/i.test(p);
}

const STATUS_SEGMENT_RE = /^[a-zA-Z_][a-zA-Z0-9_]*$/;

function isValidStatusPath(p) {
  if (!p || typeof p !== "string") return false;
  const segs = p.split(".");
  for (let i = 0; i < segs.length; i++) {
    if (!STATUS_SEGMENT_RE.test(segs[i])) return false;
  }
  return true;
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
        // Ref fields (e.g. bucketRef.name) are required within their ref branch and
        // serve as canonical resource-linking inputs, so surface them on the card.
        if (f.requiredChain || (f.required && isRefFieldPath(f.path))) requiredPaths.push(f.path);
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
  // opts: {dir, dotColor, req, ty, label, title, fan, cls, warn}
  const warnHtml = opts.warn
    ? '<span class="port-warn" title="' + esc(opts.warn) + '" style="color:var(--warn);margin-left:2px;font-size:10px">\u26a0</span>'
    : '';
  return '<div class="port' + (opts.req ? " req" : "") + (opts.cls ? " " + opts.cls : "") + '"' +
    ' data-owner="' + esc(owner) + '" data-path="' + esc(path) + '"' +
    ' title="' + esc(opts.title || path) + '">' +
    '<span class="d ' + opts.dir + '" style="background:' + opts.dotColor + '"></span>' +
    '<span class="nm">' + esc(opts.label || path) + '</span>' +
    '<span class="ty">' + esc(opts.ty || "") + '</span>' +
    warnHtml +
    (opts.fan || "") + '</div>';
}

function xrCardHTML(d, sel) {
  const xrd = d.spec.xrd || {};
  const params = xrd.parameters || {};
  const pos = S.getPosition(XR_ID) || { x: 36, y: 48 };
  let h = '<div class="node' + (sel === XR_ID ? " sel" : "") + '" data-id="' + esc(XR_ID) + '"' +
    ' tabindex="0" role="region" aria-label="' + esc((xrd.kind || "XR") + " resource " + (d.metadata && d.metadata.name || "xrd")) + '"' +
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
      fan: n > 1 ? '<span class="fan" style="pointer-events:none">×' + n + '</span>' : "",
    });
  });
  h += '</div>';
  if (xrAddOpen) {
    h += '<div class="xr-add-row" id="xr-add-row">' +
      '<input class="xr-add-input" id="xr-add-input" placeholder="parameterName" autofocus aria-label="Parameter name">' +
      '<button class="btn sm pri" id="xr-add-submit" style="padding:1px 6px">Add</button>' +
      '<button class="btn sm" id="xr-add-cancel" style="padding:1px 5px">×</button>' +
      '</div>';
  } else {
    h += '<button class="node-add" data-addxr="1">+ add field</button>';
  }
  h += '</div>';
  return h;
}

function envCardHTML(d, sel) {
  const env = (d && d.spec && d.spec.environment) || {};
  const configName = getEnvConfigName(d);
  const pos = S.getPosition(ENV_ID) || { x: 40, y: 240 };
  let h = '<div class="node' + (sel === ENV_ID ? " sel" : "") + '" data-id="' + esc(ENV_ID) + '" data-kind="EnvironmentConfig" data-name="' + esc(configName) + '"' +
    ' tabindex="0" role="region" aria-label="' + esc("EnvironmentConfig resource " + configName) + '"' +
    ' style="left:' + pos.x + 'px;top:' + pos.y + 'px">' +
    '<div class="node-h" style="background:var(--shared-soft)">' +
    '<span class="sw" style="background:var(--shared)"></span>' +
    '<span class="k">EnvironmentConfig</span>' +
    '<span class="nm">' + esc(configName) + '</span></div>' +
    '<div class="node-grp">apiextensions.crossplane.io/v1beta1</div>' +
    '<div class="ports">';
  Object.keys(env).sort().forEach(function (name) {
    const k = env[name] || {};
    const n = envFanOut(d, name);
    h += portRow(ENV_ID, name, {
      dir: "out",
      dotColor: "var(--shared)",
      req: !!k.required,
      ty: k.type || "",
      label: name,
      title: name + (k.description ? " — " + k.description : ""),
      fan: n > 1 ? '<span class="fan" style="pointer-events:none">×' + n + '</span>' : "",
    });
  });
  h += '</div></div>';
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
    ' tabindex="0" role="region" aria-label="' + esc(r.kind + " resource " + r.name) + '"' +
    ' style="left:' + pos.x + 'px;top:' + pos.y + 'px">' +
    '<div class="node-h" style="background:var(--surface-2)">' +
    '<span class="sw" style="background:' + (COLORS[fam] || "var(--wire-ref)") + '"></span>' +
    '<span class="k">' + esc(r.kind) + '</span>' +
    '<span class="nm">' + esc(r.name) + '</span>' +
    '<button class="del" data-act="duplicate" data-res="' + esc(r.name) + '" title="Duplicate (\u2318C \u2318V)" aria-label="Duplicate resource ' + esc(r.name) + '">\u29c9</button>' +
    '<button class="del" data-act="delete" data-res="' + esc(r.name) + '" title="Remove (Delete)" aria-label="Delete resource ' + esc(r.name) + '">\u00d7</button></div>' +
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
  // Always include *Ref fields so resource-linking targets are immediately droppable.
  const extra = schema ? schema.requiredPaths.filter(function (p) {
    if (seen[p]) return false;
    if (isRefFieldPath(p)) return true;
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
      } else if (parsed.kind === "env") {
        dot = "var(--shared)";
      }
    }
    const isFieldReq = !!(sf && (sf.required || sf.requiredChain));
    let optWarn = false;
    if (isFieldReq && parsed && parsed.kind === "param") {
      const pObj = (d.spec.xrd && d.spec.xrd.parameters && d.spec.xrd.parameters[parsed.param]) || {};
      if (!pObj.required && !pObj.requiredChain) {
        optWarn = true;
      }
    }
    let title = p + (sf ? " \u00b7 " + sf.type + (sf.required ? " \u00b7 required" : "") : "") +
      (sf && sf.description ? "\n" + sf.description : "");
    if (parsed) {
      if (parsed.kind === "status") {
        title = parsed.resource + ".status." + parsed.statusPath + " \u2192 " + r.name + "." + p + (title ? " \u00b7 " + title : "");
      } else if (parsed.kind === "env") {
        title = "env." + parsed.key + " \u2192 " + r.name + "." + p + (title ? " \u00b7 " + title : "");
      } else if (parsed.kind === "param") {
        title = "$" + parsed.param + " \u2192 " + r.name + "." + p + (title ? " \u00b7 " + title : "");
      }
    }
    if (optWarn) {
      title += " \u00b7 \u26a0 optional parameter wired to required field: render will omit if missing";
    }
    h += portRow(r.name, p, {
      dir: "in",
      dotColor: dot,
      req: isFieldReq,
      warn: optWarn ? "Optional parameter wired to required field: render will omit if missing" : null,
      cls: optWarn ? "port-opt-warn" : "",
      ty: sf ? sf.type : "",
      label: shortPath(p),
      title: title,
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
      } else if (parsed.kind === "env") {
        dot = "var(--shared)";
      }
    }
    let title = r.name + ".envelope." + p + " (Crossplane envelope)";
    if (parsed) {
      if (parsed.kind === "status") {
        title = parsed.resource + ".status." + parsed.statusPath + " \u2192 " + r.name + ".envelope." + p;
      } else if (parsed.kind === "env") {
        title = "env." + parsed.key + " \u2192 " + r.name + ".envelope." + p;
      } else if (parsed.kind === "param") {
        title = "$" + parsed.param + " \u2192 " + r.name + ".envelope." + p;
      }
    }
    h += portRow(r.name, "envelope." + p, {
      dir: "in",
      dotColor: dot,
      req: false,
      ty: "env",
      label: "env." + shortPath(p),
      title: title,
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
      const parsed = wired ? parseFrom(f.from) : null;
      let title = k;
      if (wired) {
        if (parsed && parsed.kind === "status") {
          title = parsed.resource + ".status." + parsed.statusPath + " \u2192 " + r.name + ".annotations." + k;
        } else if (parsed && parsed.kind === "env") {
          title = "env." + parsed.key + " \u2192 " + r.name + ".annotations." + k;
        } else if (parsed && parsed.kind === "param") {
          title = "$" + parsed.param + " \u2192 " + r.name + ".annotations." + k;
        } else {
          title = k + " \u2190 " + f.from;
        }
      }
      h += portRow(r.name, "annotations." + k, {
        dir: "in",
        dotColor: wired && f.from.indexOf("resources.") === 0 ? "var(--wire-status)" : (wired && f.from.indexOf("env.") === 0 ? "var(--shared)" : "var(--wire-xrd)"),
        req: false,
        ty: wired ? "" : (f && f.raw !== undefined && f.raw !== "" ? "raw" : "value"),
        label: shortPath(k),
        title: title,
      });
    });
  }

  // Status outputs: wired paths always, plus the top atProvider leaves from
  // the schema — displayed like inputs so "object depends on object" is
  // visible before any wire exists.
  const outStatusWires = listWires(d).filter(function (w) {
    return w.kind === "status" && w.srcResource === r.name && isValidStatusPath(w.srcPath);
  });
  const seenStatus = {};
  const statusRows = [];
  outStatusWires.forEach(function (w) {
    if (seenStatus[w.srcPath]) return;
    seenStatus[w.srcPath] = true;
    statusRows.push(w.srcPath);
  });
  const schemaLeaves = (statusLeavesFor(meta) || []).filter(isValidStatusPath);
  // Ensure atProvider.id is always offered as a primary output row for resource linking
  const idPath = (schemaLeaves.length > 0 && schemaLeaves.find(function (p) { return p === "atProvider.id" || p === "id"; })) || "atProvider.id";
  if (!seenStatus[idPath] && isValidStatusPath(idPath)) {
    seenStatus[idPath] = true;
    statusRows.unshift(idPath);
  }
  for (let si = 0; si < schemaLeaves.length && statusRows.length < STATUS_ROWS_SHOWN + Object.keys(seenStatus).length; si++) {
    const p = schemaLeaves[si];
    if (seenStatus[p] || !isValidStatusPath(p)) continue;
    seenStatus[p] = true;
    statusRows.push(p);
    if (statusRows.length >= STATUS_ROWS_SHOWN && si >= STATUS_ROWS_SHOWN) break;
  }
  if (statusRows.length) {
    h += '<div class="node-grp" style="color:var(--wire-status);text-align:right">outputs</div>';
    statusRows.forEach(function (p) {
      const isId = p === "atProvider.id" || p === "id";
      const pWires = outStatusWires.filter(function (w) { return w.srcPath === p; });
      let title = r.name + ".status." + p + " (status output \u2014 other objects can wire from this)";
      if (pWires.length > 0) {
        title = pWires.map(function (w) {
          return w.srcResource + ".status." + w.srcPath + " \u2192 " + w.resource + "." + w.path;
        }).join("\n");
      }
      h += portRow(r.name, "status." + p, {
        dir: "out",
        dotColor: "var(--wire-status)",
        req: false,
        ty: "",
        cls: "status",
        // outputs read right-aligned and short: the atProvider prefix is
        // noise at a glance, the full path lives in the title
        label: isId ? "name / id" : shortPath(p.replace(/^atProvider\./, "")),
        title: title,
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
   Delegated to canvas/layout.js for standalone unit-testability. */
const autoPlaced = new Set(); // cards the layout owns until the user drags them
let lastLayoutSig = "";       // measured-size signature; re-lay only on change

function getVisibleCanvasBounds() {
  const cw = cwEl ? cwEl.getBoundingClientRect() : { left: 0, right: window.innerWidth, width: window.innerWidth, height: window.innerHeight };
  const insp = document.getElementById("region-inspector");
  let maxScreenX = cw.width;
  if (insp) {
    const inspRect = insp.getBoundingClientRect();
    if (inspRect.width > 0 && inspRect.height > 0) {
      if (inspRect.left > cw.left && inspRect.left <= cw.right) {
        maxScreenX = Math.min(maxScreenX, inspRect.left - cw.left);
      }
    }
  }
  const maxCanvasX = toCanvas(maxScreenX, 0).x;
  return {
    screenMaxX: maxScreenX,
    canvasMaxX: maxCanvasX,
  };
}

function applyDependencyLayout(onlyUnplaced) {
  const d = doc();
  if (!d) return;

  const bounds = getVisibleCanvasBounds();
  const layers = dependencyLayers(d);
  const byLayer = {};
  (d.spec.resources || []).forEach(function (r) {
    (byLayer[layers[r.name]] = byLayer[layers[r.name]] || []).push(r.name);
  });

  const layerKeys = Object.keys(byLayer).map(Number).sort(function (a, b) { return a - b; });
  const hasEnv = !!(d.spec && ((d.spec.environment && Object.keys(d.spec.environment).length > 0) || (Array.isArray(d.spec.environmentConfigs) && d.spec.environmentConfigs.length > 0)));
  const xrEl = canvasEl ? canvasEl.querySelector('.node[data-id="' + CSS.escape(XR_ID) + '"]') : null;
  let sourceW = xrEl ? xrEl.offsetWidth : 220;
  if (hasEnv) {
    const envEl = canvasEl ? canvasEl.querySelector('.node[data-id="' + CSS.escape(ENV_ID) + '"]') : null;
    sourceW = Math.max(sourceW, envEl ? envEl.offsetWidth : 220);
  }

  const colWidths = [sourceW];
  layerKeys.forEach(function (L) {
    let maxW = 0;
    byLayer[L].forEach(function (name) {
      const el = canvasEl ? canvasEl.querySelector('.node[data-id="' + CSS.escape(name) + '"]') : null;
      maxW = Math.max(maxW, el ? el.offsetWidth : 220);
    });
    colWidths.push(maxW || 220);
  });

  const numCols = colWidths.length;
  let X0 = 40;
  let GX = 60;

  if (numCols > 1) {
    const sumColWidths = colWidths.reduce(function (a, b) { return a + b; }, 0);
    const gaps = numCols - 1;
    let availableForSpacing = bounds.canvasMaxX - 24 - sumColWidths;

    const minNeededSpacing = 16 + gaps * 16;
    const totalMinCanvas = sumColWidths + minNeededSpacing + 24;

    if (totalMinCanvas > bounds.screenMaxX && (view.k === 1 && view.x === 0 || !onlyUnplaced)) {
      const targetK = Math.max(K_MIN, Math.min(1, bounds.screenMaxX / totalMinCanvas));
      if (targetK < view.k) {
        view.k = targetK;
        applyView(true);
        availableForSpacing = ((bounds.screenMaxX - 24) / view.k) - sumColWidths;
      }
    }

    if (availableForSpacing < 40 + gaps * 60) {
      X0 = Math.max(16, Math.min(40, Math.floor(availableForSpacing * 0.2)));
      GX = Math.max(16, Math.min(60, Math.floor((availableForSpacing - X0) / gaps)));
    }
  }

  computeDependencyLayout(d, {
    onlyUnplaced: onlyUnplaced,
    autoPlaced: autoPlaced,
    getPosition: function (id) { return S.getPosition(id); },
    setPosition: function (id, pos) { S.setPosition(id, pos, false); },
    getSize: function (id) {
      const el = canvasEl ? canvasEl.querySelector('.node[data-id="' + CSS.escape(id) + '"]') : null;
      return {
        width: el ? el.offsetWidth : 220,
        height: el ? el.offsetHeight : 160,
      };
    },
    XR_ID: XR_ID,
    ENV_ID: ENV_ID,
    X0: X0,
    GX: GX,
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

function triggerTabSwitch(target) {
  if (target === "sources") target = "src";
  const btn = document.querySelector('#rtabs button[data-r="' + target + '"]');
  if (btn) {
    btn.click();
  } else {
    switchTab(target);
  }
}

function render() {
  // Never rebuild the DOM under an active pointer gesture: replacing the
  // dragged element kills the drag mid-flight ("random mouse clutches").
  if (gestureActive) { pendingRender = true; return; }
  if (!canvasEl) return;
  const d = doc();
  if (!d) { canvasEl.innerHTML = ""; wiresEl.innerHTML = ""; return; }
  const sel = S.state.selectedResource;
  const hasEnv = !!(d.spec && ((d.spec.environment && Object.keys(d.spec.environment).length > 0) || (Array.isArray(d.spec.environmentConfigs) && d.spec.environmentConfigs.length > 0)));
  const desired = [{ id: XR_ID, html: xrCardHTML(d, sel) }];
  if (hasEnv) {
    desired.push({ id: ENV_ID, html: envCardHTML(d, sel) });
  }
  (d.spec.resources || []).forEach(function (r) {
    desired.push({ id: r.name, html: resourceCardHTML(d, r, sel) });
  });

  let emptyEl = canvasEl.querySelector("#canvas-empty-state");
  if ((d.spec.resources || []).length === 0) {
    if (!emptyEl) {
      emptyEl = document.createElement("div");
      emptyEl.className = "canvas-empty-state";
      emptyEl.id = "canvas-empty-state";
      emptyEl.innerHTML = '<div class="canvas-empty-title">' +
          '1. Drag kinds from <span role="button" tabindex="0" class="canvas-empty-tab-link" data-tab-switch="kinds" data-r="kinds">KINDS</span>  ' +
          '2. Add cloud providers in <span role="button" tabindex="0" class="canvas-empty-tab-link" data-tab-switch="sources" data-r="src">SOURCES</span>' +
        '</div>' +
        '<div class="canvas-empty-steps">' +
          '<div class="canvas-empty-step"><span class="step-num">1</span> Drag kinds onto canvas from <span role="button" tabindex="0" class="canvas-empty-tab-link" data-tab-switch="kinds" data-r="kinds"><strong>KINDS</strong></span> (16 native Kubernetes kinds ready without providers)</div>' +
          '<div class="canvas-empty-step"><span class="step-num">2</span> Add cloud providers in <span role="button" tabindex="0" class="canvas-empty-tab-link" data-tab-switch="sources" data-r="src"><strong>SOURCES</strong></span> for AWS, Azure, GCP</div>' +
        '</div>';
      emptyEl.addEventListener("click", function (e) {
        const tabSwitch = e.target.closest("[data-tab-switch]");
        if (tabSwitch) {
          e.stopPropagation();
          triggerTabSwitch(tabSwitch.getAttribute("data-tab-switch"));
        }
      });
      emptyEl.addEventListener("keydown", function (e) {
        const tabSwitchKey = e.target.closest("[data-tab-switch]");
        if (tabSwitchKey && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          e.stopPropagation();
          triggerTabSwitch(tabSwitchKey.getAttribute("data-tab-switch"));
        }
      });
      canvasEl.appendChild(emptyEl);
    }
  } else if (emptyEl) {
    emptyEl.remove();
  }

  const existing = new Map();
  canvasEl.querySelectorAll(".node").forEach(function (el) {
    existing.set(el.getAttribute("data-id"), el);
  });
  const desiredIds = new Set(desired.map(function (x) { return x.id; }));
  existing.forEach(function (el, id) {
    if (!desiredIds.has(id)) el.remove();
  });
  desired.forEach(function (item) {
    const prev = existing.get(item.id);
    const temp = document.createElement("div");
    temp.innerHTML = item.html;
    const nextEl = temp.firstElementChild;
    if (!nextEl) return;
    if (!prev) {
      canvasEl.appendChild(nextEl);
    } else {
      prev.className = nextEl.className;
      if (prev.innerHTML !== nextEl.innerHTML) {
        prev.innerHTML = nextEl.innerHTML;
      }
    }
  });
  // measured layout pass for cards that have no stored position
  const freshCards = (d.spec.resources || []).some(function (r) { return !S.getPosition(r.name); }) ||
    !S.getPosition(XR_ID) || (hasEnv && !S.getPosition(ENV_ID));
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

function portPos(owner, path, cwRect) {
  const el = canvasEl.querySelector(
    '.port[data-owner="' + CSS.escape(owner) + '"][data-path="' + CSS.escape(path) + '"] .d');
  if (!el) return null;
  const cw = cwRect || (cwEl ? cwEl.getBoundingClientRect() : { left: 0, top: 0 });
  const r = el.getBoundingClientRect();
  return { x: r.left - cw.left + r.width / 2, y: r.top - cw.top + r.height / 2 };
}

let xrAddOpen = false;
let selectedWire = null;

function wireKey(w) {
  if (!w) return "";
  return (w.kind || "") + ":" + (w.srcResource || "") + ":" + (w.srcPath || "") + ":" + (w.param || "") + ":" + (w.envKey || "") + ":" + (w.resource || "") + ":" + (w.path || "");
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
  const cwRect = ws.length && cwEl ? cwEl.getBoundingClientRect() : null;
  let s = "";
  let delButtons = "";
  ws.forEach(function (w, idx) {
    let a, b, cls, col, title;
    if (w.kind === "status") {
      a = portPos(w.srcResource, "status." + w.srcPath, cwRect) || portPos(w.srcResource, w.srcPath, cwRect) || portPos(w.srcResource, "status.atProvider.id", cwRect);
      b = portPos(w.resource, w.path, cwRect);
      cls = "wire-status";
      col = "var(--wire-status)";
      title = w.srcResource + ".status." + w.srcPath + " \u2192 " + w.resource + "." + w.path;
    } else if (w.kind === "env") {
      a = portPos(ENV_ID, w.envKey, cwRect);
      b = portPos(w.resource, w.path, cwRect);
      cls = "wire-shared";
      col = "var(--shared)";
      title = "env." + w.envKey + " \u2192 " + w.resource + "." + w.path;
    } else {
      a = portPos(XR_ID, w.param, cwRect);
      b = portPos(w.resource, w.path, cwRect);
      const shared = fans[w.param] > 1;
      cls = shared ? "wire-shared" : "wire-xrd";
      col = shared ? "var(--shared)" : "var(--wire-xrd)";
      title = "$" + w.param + " \u2192 " + w.resource + "." + w.path;
    }
    if (!a || !b) return;
    const isSel = selectedWire && wireKey(selectedWire) === wireKey(w);
    const dx = Math.max(34, Math.abs(b.x - a.x) * 0.42);
    const dPath = 'M' + a.x + ',' + a.y +
      ' C' + (a.x + dx) + ',' + a.y + ' ' + (b.x - dx) + ',' + b.y +
      ' ' + b.x + ',' + b.y;
    s += '<path class="wire-hit" d="' + dPath +
      '" stroke="transparent" stroke-width="14" fill="none" pointer-events="stroke" data-wire-idx="' + idx + '" title="' + esc(title) + '">' +
      '<title>' + esc(title) + '</title></path>';
    s += '<path class="wire-path ' + cls + (isSel ? " wire-selected" : "") + '" d="' + dPath +
      '" stroke="' + col + '" data-wire-idx="' + idx + '" pointer-events="stroke" tabindex="0" role="button" aria-label="' + esc(title) + '" title="' + esc(title) + '">' +
      '<title>' + esc(title) + '</title></path>';

    if (isSel) {
      const midX = (a.x + b.x) / 2;
      const midY = (a.y + b.y) / 2;
      // The outer <g> carries the position and an inner one carries the hover
      // grow. They cannot be the same element: on an SVG element the transform
      // ATTRIBUTE is the very same property as CSS `transform`, so a
      // :hover{transform:scale()} here would drop the translate and fling the
      // button off to the SVG origin — out from under the cursor hovering it.
      delButtons += '<g class="wire-del-btn" data-wire-idx="' + idx + '" transform="translate(' + midX + ',' + midY + ')" pointer-events="all">' +
        '<g class="wire-del-btn-g">' +
        '<circle r="9"></circle>' +
        '<text y="-0.5">\u00d7</text>' +
        '</g>' +
        '<title>Delete wire (Delete/Backspace)</title>' +
        '</g>';
    }
  });
  wiresEl.innerHTML = s + delButtons;

  const existingBadge = cwEl && cwEl.querySelector("#wire-badge");
  if (selectedWire) {
    let bText;
    if (selectedWire.kind === "status") {
      bText = selectedWire.srcResource + ".status." + selectedWire.srcPath + " \u2192 " + selectedWire.resource + "." + selectedWire.path;
    } else if (selectedWire.kind === "env") {
      bText = "env." + selectedWire.envKey + " \u2192 " + selectedWire.resource + "." + selectedWire.path;
    } else {
      bText = "$" + selectedWire.param + " \u2192 " + selectedWire.resource + "." + selectedWire.path;
    }
    if (!existingBadge && cwEl) {
      const bEl = document.createElement("div");
      bEl.id = "wire-badge";
      bEl.className = "wire-badge wire-hint-bar";
      bEl.setAttribute("role", "status");
      cwEl.appendChild(bEl);
    }
    const badge = cwEl.querySelector("#wire-badge");
    if (badge) {
      badge.textContent = bText;
      badge.setAttribute("title", bText);
      badge.setAttribute("aria-label", bText);
    }
  } else if (existingBadge) {
    existingBadge.remove();
  }
}

function scheduleWires() {
  if (rafWires) return;
  rafWires = requestAnimationFrame(function () { rafWires = 0; drawWires(); });
}

/* ---------- view transform: pan + zoom (slice 6) ---------- */

const view = { x: 0, y: 0, k: 1 };
const K_MIN = 0.4, K_MAX = 2.5;

function applyView(deferWires) {
  canvasEl.style.transformOrigin = "0 0";
  canvasEl.style.transform =
    "translate(" + view.x + "px," + view.y + "px) scale(" + view.k + ")";
  const pct = document.getElementById("zoom-pct");
  if (pct) pct.textContent = Math.round(view.k * 100) + "%";
  if (deferWires) {
    scheduleWires();
  } else {
    drawWires(); // synchronous: wires must never lag the transform by a frame
  }
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
  // Touch gestures are handled separately via touchstart/touchmove to avoid double-panning
  if (e.pointerType === "touch") return;
  // drag on empty canvas ground pans the view
  if (e.button !== 0) return;
  if (e.target.closest(".node") || e.target.closest("button") || e.target.closest("svg path")) return;
  const sx = e.clientX, sy = e.clientY, ox = view.x, oy = view.y;
  const abortDrag = startDrag(e, function mv(ev) {
    if (!ev.buttons) { abortDrag(); return; } // release happened while unfocused
    view.x = ox + ev.clientX - sx;
    view.y = oy + ev.clientY - sy;
    applyView();
  }, function up() {
    gestureEnd();
  });
  gestureBegin(abortDrag);
}

function fitNodesToView() {
  const nodes = Array.from(canvasEl ? canvasEl.querySelectorAll(".node") : []);
  if (!nodes.length || !cwEl) {
    view.x = 0; view.y = 0; view.k = 1;
    applyView();
    return;
  }
  const cwRect = cwEl.getBoundingClientRect();
  const vw = cwRect.width;
  const vh = cwRect.height;
  if (vw <= 0 || vh <= 0) {
    view.x = 0; view.y = 0; view.k = 1;
    applyView();
    return;
  }

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  nodes.forEach(function (el) {
    const r = el.getBoundingClientRect();
    const left = (r.left - cwRect.left - view.x) / view.k;
    const top = (r.top - cwRect.top - view.y) / view.k;
    const right = (r.right - cwRect.left - view.x) / view.k;
    const bottom = (r.bottom - cwRect.top - view.y) / view.k;
    if (left < minX) minX = left;
    if (top < minY) minY = top;
    if (right > maxX) maxX = right;
    if (bottom > maxY) maxY = bottom;
  });

  const cardsWidth = maxX - minX;
  const cardsHeight = maxY - minY;
  const padding = 40;
  const availW = Math.max(10, vw - padding * 2);
  const availH = Math.max(10, vh - padding * 2);

  const scaleX = availW / (cardsWidth || 1);
  const scaleY = availH / (cardsHeight || 1);
  const minK = Math.min(K_MIN, 0.2);
  const k = Math.min(1, Math.max(minK, Math.min(scaleX, scaleY)));

  const cx = minX + cardsWidth / 2;
  const cy = minY + cardsHeight / 2;

  view.k = k;
  view.x = (vw / 2) - cx * k;
  view.y = (vh / 2) - cy * k;
  applyView();
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
  bar.querySelector("#zoom-reset").addEventListener("click", fitNodesToView);
  bar.querySelector("#layout-btn").addEventListener("click", function () {
    if (typeof S.clearPositions === "function") {
      S.clearPositions();
    }
    autoPlaced.clear();
    lastLayoutSig = "";
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
    const rawLeaves = (detail && detail.status || []).map(function (f) { return f.path; });
    const leaves = rawLeaves.filter(isValidStatusPath);
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
      const actions = {
        duplicate: function () { duplicateResource(res); },
        delete: function () { removeResource(resName); },
        rename: function () {
          const to = window.prompt('Rename "' + resName + '" to:', resName);
          if (!to || to === resName) return;
          S.renameResource(resName, to).then(function (ok) {
            if (!ok) return;
            const p = S.getPosition(resName);
            if (p) {
              if (typeof S.renamePosition === "function") {
                S.renamePosition(resName, to);
              } else {
                S.setPosition(to, p);
              }
            }
            S.select(to);
          });
        },
      };
      if (actions[it.act]) actions[it.act]();
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
  if (!n || n.getAttribute("data-id") === XR_ID || n.getAttribute("data-id") === ENV_ID) return; // native browser menu elsewhere
  e.preventDefault();
  const name = n.getAttribute("data-id");
  S.select(name);
  openCtxMenu(e.clientX, e.clientY, name);
}

/* ---------- interactions ---------- */



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
    if (p) {
      const bounds = getVisibleCanvasBounds();
      const cardW = 220;
      const maxX = Math.max(4, bounds.canvasMaxX - cardW - 16);
      const nx = Math.max(4, Math.min(maxX, p.x + 28));
      autoPlaced.delete(copyName);
      S.setPosition(copyName, { x: nx, y: p.y + 28 }, true);
    }
    S.select(copyName);
  });
}

function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function isResourceRef(val, name) {
  if (typeof val !== "string" || !val || !name) return false;
  if (val === "resources." + name || val.startsWith("resources." + name + ".")) return true;
  const re = new RegExp("(?:^|[^a-zA-Z0-9_-])resources\\." + escapeRegex(name) + "(?:$|[^a-zA-Z0-9_-])");
  return re.test(val);
}

function isObjectReferencingResource(obj, name) {
  if (!obj || typeof obj !== "object") return false;
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    if (typeof v === "string" && isResourceRef(v, name)) return true;
    if (typeof v === "object" && isObjectReferencingResource(v, name)) return true;
  }
  return false;
}

function findDownstreamRefs(resources, name) {
  const downstream = [];
  (resources || []).forEach(function (r) {
    if (r.name === name) return;
    const depFields = [];
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        const f = r.fields[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          depFields.push(k);
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        const f = r.envelope[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          depFields.push("envelope." + k);
        }
      });
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          depFields.push("annotations." + k);
        }
      });
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string" && isResourceRef(r.connectionSecret, name)) {
        depFields.push("connectionSecret");
      } else if (typeof r.connectionSecret === "object" && isObjectReferencingResource(r.connectionSecret, name)) {
        depFields.push("connectionSecret");
      }
    }
    if (r.when && isResourceRef(r.when, name)) {
      depFields.push("when");
    }
    if (r.forEach && isResourceRef(r.forEach, name)) {
      depFields.push("forEach");
    }
    if (depFields.length > 0) {
      downstream.push({ name: r.name, fields: depFields });
    }
  });
  return downstream;
}

function cleanDownstreamRefs(resources, name) {
  (resources || []).forEach(function (r) {
    if (r.name === name) return;
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        const f = r.fields[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          delete r.fields[k];
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        const f = r.envelope[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          delete r.envelope[k];
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f && ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name)))) {
          delete r.annotations[k];
        }
      });
      if (Object.keys(r.annotations).length === 0) delete r.annotations;
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        if (isResourceRef(r.connectionSecret, name)) delete r.connectionSecret;
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys = r.connectionSecret.keys.filter(function (item) {
            if (typeof item === "string") return !isResourceRef(item, name);
            if (item && typeof item === "object") {
              if (item.from && isResourceRef(item.from, name)) return false;
              if (item.fromNode && item.fromNode === name) return false;
            }
            return true;
          });
          if (r.connectionSecret.keys.length === 0) delete r.connectionSecret;
        } else if (isObjectReferencingResource(r.connectionSecret, name)) {
          delete r.connectionSecret;
        }
      }
    }
    if (r.when && isResourceRef(r.when, name)) {
      delete r.when;
    }
    if (r.forEach && isResourceRef(r.forEach, name)) {
      delete r.forEach;
    }
  });
}

function removeResource(name) {
  const d = doc();
  if (!d) return;
  const res = (d.spec.resources || []).find(function (r) { return r.name === name; });
  if (!res) return;

  const wired = Object.keys(res.fields || {}).filter(function (k) { return res.fields[k] && res.fields[k].from; });
  const downstream = findDownstreamRefs(d.spec.resources || [], name);

  if (wired.length > 0 || downstream.length > 0) {
    let promptMsg = 'Remove "' + name + '"?';
    if (wired.length > 0 && downstream.length > 0) {
      const downStr = downstream.map(function (dep) {
        return dep.name + " (" + dep.fields.join(", ") + ")";
      }).join(", ");
      promptMsg += ' Wired fields will be dropped: ' + wired.join(", ") + '. Downstream references in ' + downStr + ' will be unwired.';
    } else if (wired.length > 0) {
      promptMsg += ' Wired fields will be dropped: ' + wired.join(", ");
    } else {
      const downStr = downstream.map(function (dep) {
        return dep.name + " (" + dep.fields.join(", ") + ")";
      }).join(", ");
      promptMsg += ' Downstream references in ' + downStr + ' will be unwired.';
    }
    if (!window.confirm(promptMsg)) return;
  }

  S.replaceDoc(function (draft) {
    cleanDownstreamRefs(draft.spec.resources || [], name);
    draft.spec.resources = (draft.spec.resources || []).filter(function (r) { return r.name !== name; });
  }).then(function (ok) {
    if (ok) {
      if (selectedWire && (selectedWire.resource === name || selectedWire.srcResource === name)) {
        selectedWire = null;
      }
      S.select(null);
    }
  });
}

function onKeyDown(e) {
  const t = e.target;
  const tabSwitchKey = t && t.closest && t.closest("[data-tab-switch]");
  if (tabSwitchKey && (e.key === "Enter" || e.key === " ")) {
    e.preventDefault();
    triggerTabSwitch(tabSwitchKey.getAttribute("data-tab-switch"));
    return;
  }
  if (t && t.id === "xr-add-input") {
    if (e.key === "Enter") {
      const val = (t.value || "").trim();
      if (val) {
        xrAddOpen = false;
        S.addParameter(val, { type: "string", required: false }).then(function () {
          S.select(XR_ID);
        });
      }
    } else if (e.key === "Escape") {
      xrAddOpen = false;
      render();
    }
    return;
  }
  if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;

  // Keyboard selection on focused card (.node)
  const nodeEl = t && t.closest && t.closest(".node");
  if (nodeEl && !t.closest("button, input, textarea, a, select, [role='button']")) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      selectedWire = null;
      const id = nodeEl.getAttribute("data-id");
      if (id) S.select(id);
      return;
    }
  }

  // Keyboard navigation on focused wire path
  if (t && t.classList && t.classList.contains("wire-path")) {
    const idx = Number(t.getAttribute("data-wire-idx"));
    const ws = listWires(doc());
    if (ws[idx]) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        selectedWire = ws[idx];
        drawWires();
        const p = wiresEl && wiresEl.querySelector('.wire-path[data-wire-idx="' + idx + '"]');
        if (p && typeof p.focus === "function") {
          p.focus();
        }
        return;
      }
      if (e.key === "Delete" || e.key === "Backspace") {
        e.preventDefault();
        const toDel = ws[idx];
        selectedWire = null;
        deleteWire(toDel);
        return;
      }
    }
  }

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

  if (e.key === "Escape" && S.state.selectedResource) {
    S.select(null);
    return;
  }

  const sel = S.state.selectedResource;
  if (!sel || sel === XR_ID || sel === ENV_ID) return;
  const d = doc();
  const res = d && (d.spec.resources || []).find(function (r) { return r.name === sel; });
  const mod = e.metaKey || e.ctrlKey;
  if (mod && e.key === "c") {
    if (String(window.getSelection && window.getSelection())) return; // real text copy wins
    if (res) { copiedResource = JSON.parse(JSON.stringify(res)); }
  } else if (mod && e.key === "v" && copiedResource) { e.preventDefault(); duplicateResource(copiedResource); }
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
      if (document.activeElement && typeof document.activeElement.blur === "function") {
        document.activeElement.blur();
      }
      S.select(null);
      drawWires();
      const p = wiresEl && wiresEl.querySelector('.wire-path[data-wire-idx="' + idx + '"]');
      if (p && typeof p.focus === "function") {
        p.focus();
      }
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
  const tabSwitch = e.target.closest("[data-tab-switch]");
  if (tabSwitch) {
    triggerTabSwitch(tabSwitch.getAttribute("data-tab-switch"));
    return;
  }
  const act = e.target.closest("[data-act]");
  if (act) {
    const rn = act.getAttribute("data-res");
    const d = doc();
    const res = d && (d.spec.resources || []).find(function (x) { return x.name === rn; });
    if (!res) return;
    const actions = {
      delete: function () { removeResource(rn); },
      duplicate: function () { duplicateResource(res); },
    };
    const actionName = act.getAttribute("data-act");
    if (actions[actionName]) actions[actionName]();
    return;
  }
  if (e.target.closest("[data-addxr]")) {
    const d = doc();
    if (!d) return;
    xrAddOpen = true;
    S.select(XR_ID);
    render();
    const inp = canvasEl.querySelector("#xr-add-input");
    if (inp) inp.focus();
    return;
  }
  if (e.target.closest("#xr-add-cancel")) {
    xrAddOpen = false;
    render();
    return;
  }
  if (e.target.closest("#xr-add-submit")) {
    const inp = canvasEl.querySelector("#xr-add-input");
    const val = (inp && inp.value || "").trim();
    if (val) {
      xrAddOpen = false;
      S.addParameter(val, { type: "string", required: false }).then(function () {
        S.select(XR_ID);
      });
    }
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

/* ---------- drag-to-wire (slice: drag-to-wire) ----------
   Delegated to canvas/drag-to-wire.js state machine. */

function onPointerDown(e) {
  if (e.target.closest("[data-resize]")) { onResizeDown(e); return; }
  if (e.button !== undefined && e.button !== 0) return;
  if (e.target.closest("[data-act]") || e.target.closest("button")) return;

  const nodeEl = e.target.closest(".node");
  if (nodeEl) {
    const name = nodeEl.getAttribute("data-id");
    if (name) S.select(name);
  }

  const portEl = e.target.closest(".port");
  if (portEl) {
    onWireDragDown(e, portEl);
    return;
  }

  if (!nodeEl) return;
  const name = nodeEl.getAttribute("data-id");

  const h = e.target.closest(".node-h");
  if (!h) return;
  const el = canvasEl.querySelector('.node[data-id="' + CSS.escape(name) + '"]');
  if (!el) return;
  const start = S.getPosition(name) || { x: el.offsetLeft, y: el.offsetTop };
  const sx = e.clientX, sy = e.clientY;
  let lx = start.x, ly = start.y;

  function onUp() {
    const isDrag = Math.abs(lx - start.x) > 3 || Math.abs(ly - start.y) > 3;
    if (isDrag) {
      autoPlaced.delete(name);             // a real drag: the user owns it now
      S.setPosition(name, { x: lx, y: ly }, true);
    } else {
      S.setPosition(name, { x: lx, y: ly }, !autoPlaced.has(name));
    }
    drawWires();
    gestureEnd();
  }
  function mv(ev) {
    if (!ev.buttons) { onUp(); return; } // release happened while unfocused
    const bounds = getVisibleCanvasBounds();
    const cardW = el.offsetWidth || 220;
    const maxX = Math.max(4, bounds.canvasMaxX - cardW - 16);
    lx = Math.max(4, Math.min(maxX, start.x + (ev.clientX - sx) / view.k));
    ly = Math.max(4, start.y + (ev.clientY - sy) / view.k);
    el.style.left = lx + "px";
    el.style.top = ly + "px";
    scheduleWires();
  }
  const abortDrag = startDrag(e, mv, onUp);
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
  const preview = document.getElementById("kind-preview");
  if (preview) { preview.hidden = true; preview.remove(); }
  cwEl.style.outline = "";
  const pl = parseDropPayload(e.dataTransfer);
  if (!pl) return;
  e.preventDefault();
  const entry = resolveDropKind(pl);
  const d = doc();
  if (!entry || !d) return;
  const rect = cwEl.getBoundingClientRect();
  const pt = toCanvas(e.clientX - rect.left, e.clientY - rect.top);
  const bounds = getVisibleCanvasBounds();
  const cardW = 220;
  const maxX = Math.max(4, bounds.canvasMaxX - cardW - 16);
  const x = Math.max(4, Math.min(maxX, pt.x - 90));
  const y = Math.max(4, pt.y - 16);
  const name = uniqueResourceName(d, entry.kind);
  autoPlaced.delete(name);
  S.setPosition(name, { x: x, y: y }, true);
  S.select(name);
  S.replaceDoc(function (next) {
    // sources is the dependency manifest the server loads providers from at
    // startup — a dropped kind's provider must be declared there or generate
    // cannot load its CRDs after a restart. Native kinds ("k8s") and live cluster
    // kinds ("cluster") are not provider packages and never appear in sources.
    if (entry.provider && entry.provider !== "k8s" && entry.provider !== "cluster") {
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

  initDragToWire({
    store: S,
    api: A,
    getCwEl: function () { return cwEl; },
    getWiresEl: function () { return wiresEl; },
    portPos: function (owner, path) { return portPos(owner, path); },
    drawWires: function () { drawWires(); },
    gestureBegin: function (fn) { gestureBegin(fn); },
    gestureEnd: function () { gestureEnd(); },
    kindMeta: function (res) { return kindMeta(res); },
    getKindsCache: function () { return kindsCache; },
    setKindsCache: function (k) { kindsCache = k; },
    XR_ID: XR_ID,
    ENV_ID: ENV_ID,
  });

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
    if (!isPickerJustOpened() && !e.target.closest("#wire-picker")) closeWirePicker();
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
      applyView(true);
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
      applyView(true);
    }
  }, { passive: false });

  cwEl.addEventListener("touchend", function (e) {
    if (e.touches.length === 0) {
      isTouching = false;
      drawWires();
    }
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

