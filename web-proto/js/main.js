/**
 * main.js — boot. The single module entry (index.html loads only this).
 * Imports the shared store/api and explicitly initializes every region,
 * then loads the document; the initial "doc" emit triggers first renders.
 *
 * Region root elements (see index.html):
 *   palette   #region-palette
 *   canvas    #cw
 *   inspector #region-inspector
 *   output    #region-output  (also drives the topbar #region-topbar —
 *             crumb/version/valid chip/theme/validate/generate live off
 *             this region's generate cycle)
 */
import { store } from "./store.js";
import * as api from "./api.js";
import { init as initPalette } from "./regions/palette.js";
import { init as initCanvas } from "./regions/canvas.js";
import { init as initInspector } from "./regions/inspector.js";
import { init as initOutput } from "./regions/output.js";

const deps = { store, api };
initPalette(document.getElementById("region-palette"), deps);
initCanvas(document.getElementById("cw"), deps);
initInspector(document.getElementById("region-inspector"), deps);
initOutput(document.getElementById("region-output"), deps);

store.loadDoc();


/* ---- resizable side columns: drag handles, clamped, persisted ---- */
(function () {
  var cols = document.getElementById("cols");
  if (!cols) return;
  var MIN_L = 160, MAX_L = 420, MIN_R = 180, MAX_R = 520, MIN_CANVAS = 300;
  var widths = { l: 216, r: 330 };
  try {
    var saved = JSON.parse(localStorage.getItem("cf-col-widths") || "null");
    if (saved && saved.l && saved.r) widths = saved;
  } catch (_) { /* private mode */ }

  function apply() {
    var total = cols.getBoundingClientRect().width;
    var l = Math.min(MAX_L, Math.max(MIN_L, widths.l));
    var r = Math.min(MAX_R, Math.max(MIN_R, widths.r));
    if (total && total - l - r < MIN_CANVAS) {
      r = Math.max(MIN_R, total - l - MIN_CANVAS);
      l = Math.max(MIN_L, Math.min(l, total - r - MIN_CANVAS));
    }
    widths.l = l; widths.r = r;
    cols.style.gridTemplateColumns = l + "px 1fr " + r + "px";
    try { localStorage.setItem("cf-col-widths", JSON.stringify(widths)); } catch (_) { /* ok */ }
  }

  function makeHandle(id, side) {
    var el = document.createElement("div");
    el.id = id;
    el.setAttribute("role", "separator");
    el.setAttribute("aria-orientation", "vertical");
    el.title = "Drag to resize";
    el.style.cssText = "position:absolute;top:0;bottom:0;width:7px;cursor:col-resize;z-index:6";
    el.addEventListener("pointerdown", function (e) {
      e.preventDefault();
      var sx = e.clientX, start = side === "l" ? widths.l : widths.r;
      function mv(ev) {
        var d = ev.clientX - sx;
        if (side === "l") widths.l = start + d; else widths.r = start - d;
        apply(); place();
      }
      function up() {
        document.removeEventListener("pointermove", mv);
        document.removeEventListener("pointerup", up);
      }
      document.addEventListener("pointermove", mv);
      document.addEventListener("pointerup", up);
    });
    cols.style.position = "relative";
    cols.appendChild(el);
    return el;
  }

  var hl = makeHandle("col-resize-l", "l");
  var hr = makeHandle("col-resize-r", "r");
  function place() {
    hl.style.left = (widths.l - 3) + "px";
    hr.style.right = (widths.r - 3) + "px";
  }
  apply(); place();
  addEventListener("resize", function () { apply(); place(); });
})();


/* ---- undo/redo: topbar buttons + keys over the store's doc history ---- */
(function () {
  var ub = document.getElementById("undoBtn");
  var rb = document.getElementById("redoBtn");
  if (!ub || !rb) return;
  function sync() {
    ub.disabled = !store.canUndo();
    rb.disabled = !store.canRedo();
  }
  ub.addEventListener("click", function () { store.undo(); });
  rb.addEventListener("click", function () { store.redo(); });
  store.subscribe("doc", sync);
  store.subscribe("error", sync);
  addEventListener("keydown", function (e) {
    var t = e.target;
    // native undo inside text editing always wins
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
    if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "z") return;
    e.preventDefault();
    if (e.shiftKey) store.redo(); else store.undo();
  });
  sync();
})();
