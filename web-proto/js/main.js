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


/* ---- narrow-screen drawers: panes slide over instead of vanishing ---- */
(function () {
  var l = document.querySelector(".pane.l");
  var r = document.querySelector(".pane.r");
  var toggleL = document.getElementById("pane-toggle-l");
  var closeR = document.getElementById("pane-close-r");
  if (toggleL && l) toggleL.addEventListener("click", function () {
    l.classList.toggle("drawer-open");
    if (r) r.classList.remove("drawer-open");
  });
  var toggleR = document.getElementById("pane-toggle-r");
  if (toggleR && r) toggleR.addEventListener("click", function () {
    r.classList.toggle("drawer-open");
    if (l) l.classList.remove("drawer-open");
  });
  if (closeR && r) closeR.addEventListener("click", function () {
    r.classList.remove("drawer-open");
  });
  // selecting something opens the inspector drawer on narrow screens
  store.subscribe("selection", function (sel) {
    if (!r || !window.matchMedia("(max-width:900px)").matches) return;
    if (sel) { r.classList.add("drawer-open"); if (l) l.classList.remove("drawer-open"); }
  });
  // crossing the breakpoint must never strand a selected inspector: entering
  // narrow auto-opens the drawer for the current selection, leaving narrow
  // clears drawer state so the desktop columns render normally.
  var mq = window.matchMedia("(max-width:900px)");
  (mq.addEventListener ? mq.addEventListener.bind(mq, "change") : mq.addListener.bind(mq))(function (e) {
    if (!r) return;
    if (e.matches) {
      if (store.state.selectedResource) r.classList.add("drawer-open");
    } else {
      r.classList.remove("drawer-open");
      if (l) l.classList.remove("drawer-open");
    }
  });
})();


/* ---- import dsl.yaml: file picker -> server YAML gate -> doc replaced ---- */
(function () {
  var btn = document.getElementById("importBtn");
  var file = document.getElementById("importFile");
  if (!btn || !file) return;
  btn.addEventListener("click", function () { file.click(); });
  store.subscribe("error", function (e) {
    if (!e || e.source !== "importBlueprint") return;
    var bar = document.getElementById("import-warn");
    if (!bar) {
      bar = document.createElement("div");
      bar.id = "import-warn";
      bar.className = "warnbar";
      bar.setAttribute("role", "alert");
      var host = document.getElementById("region-topbar") || document.body;
      host.parentNode.insertBefore(bar, host.nextSibling);
    }
    bar.hidden = false;
    bar.textContent = "import failed: " + e.message;
    setTimeout(function () { bar.hidden = true; }, 8000);
  });
  file.addEventListener("change", function () {
    var f = file.files && file.files[0];
    file.value = "";
    if (!f) return;
    var reader = new FileReader();
    reader.onload = function () {
      store.importBlueprint(String(reader.result)).then(function (doc) {
        if (doc) store.select(null);
        // failures surface through the store's error topic (verbatim 400)
      });
    };
    reader.readAsText(f);
  });
})();


/* ---- package: one-click Configuration .xpkg download ---- */
(function () {
  var btn = document.getElementById("packageBtn");
  if (!btn) return;
  btn.addEventListener("click", function () {
    var a = document.createElement("a");
    a.href = "/api/package";
    a.download = ""; // filename comes from Content-Disposition
    document.body.appendChild(a);
    a.click();
    a.remove();
  });
})();


/* ---- startup example chooser: modal + direct blueprint loader ---- */
(function () {
  var btn = document.getElementById("examplesBtn");
  var overlay = document.getElementById("examplesOverlay");
  var closeBtn = document.getElementById("examplesCloseBtn");
  var grid = document.getElementById("examplesGrid");
  if (!btn || !overlay || !grid) return;

  var cachedExamples = null;

  function iconOf(id) {
    if (id === "irsa") return { label: "IAM", color: "var(--wire-ref)" };
    if (id === "rds-postgres") return { label: "RDS", color: "#d97706" };
    if (id === "k8s-app") return { label: "K8S", color: "var(--wire-status)" };
    return { label: "EX", color: "var(--wire-xrd)" };
  }

  function esc(s) {
    return String(s || "").replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function renderExamples(list) {
    if (!list || !list.length) {
      grid.innerHTML = '<div class="empty">No starter examples available.</div>';
      return;
    }
    var html = "";
    list.forEach(function (ex) {
      var ic = iconOf(ex.id);
      var tagsHtml = (ex.tags || []).map(function (t) {
        return '<span class="example-tag">' + esc(t) + '</span>';
      }).join("");
      var resLabel = ex.resourceCount ? (ex.resourceCount + " resources") : "";

      html += '<div class="example-card" data-id="' + esc(ex.id) + '">' +
        '<div class="example-card-h">' +
          '<span class="example-icon" style="background:' + ic.color + '">' + ic.label + '</span>' +
          '<div style="min-width:0;flex:1">' +
            '<div class="example-title">' + esc(ex.name) + '</div>' +
          '</div>' +
        '</div>' +
        '<div class="example-desc">' + esc(ex.description) + '</div>' +
        '<div class="example-tags">' +
          (resLabel ? '<span class="example-tag" style="background:var(--wire-xrd-soft);color:var(--wire-xrd)">' + esc(resLabel) + '</span>' : '') +
          tagsHtml +
        '</div>' +
        '<button class="btn pri sm example-btn" data-load-id="' + esc(ex.id) + '">Load Blueprint</button>' +
      '</div>';
    });
    grid.innerHTML = html;
  }

  function loadExamples() {
    if (cachedExamples) {
      renderExamples(cachedExamples);
      return;
    }
    grid.innerHTML = '<div class="empty">Loading examples…</div>';
    api.getExamples().then(function (data) {
      cachedExamples = data && data.examples || [];
      renderExamples(cachedExamples);
    }).catch(function (err) {
      grid.innerHTML = '<div class="empty">Failed to load examples: ' + esc(err.message) + '</div>';
    });
  }

  function openModal() {
    overlay.hidden = false;
    loadExamples();
  }

  function closeModal() {
    overlay.hidden = true;
  }

  btn.addEventListener("click", openModal);
  if (closeBtn) closeBtn.addEventListener("click", closeModal);
  overlay.addEventListener("click", function (e) {
    if (e.target === overlay) closeModal();
  });

  addEventListener("keydown", function (e) {
    if (e.key === "Escape" && !overlay.hidden) {
      closeModal();
    }
  });

  grid.addEventListener("click", function (e) {
    var loadBtn = e.target.closest("[data-load-id]");
    if (!loadBtn) return;
    var id = loadBtn.getAttribute("data-load-id");
    var ex = (cachedExamples || []).find(function (item) { return item.id === id; });
    if (!ex || !ex.yaml) return;

    loadBtn.disabled = true;
    loadBtn.textContent = "Loading…";
    store.importBlueprint(ex.yaml).then(function (doc) {
      if (doc) store.select(null);
      closeModal();
    }).finally(function () {
      loadBtn.disabled = false;
      loadBtn.textContent = "Load Blueprint";
    });
  });
})();

