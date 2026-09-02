import { startDrag } from "./drag.js";
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
import { esc } from "./dom.js";
import { init as initPalette } from "./regions/palette.js";
import { init as initCanvas } from "./regions/canvas.js";
import { init as initInspector } from "./regions/inspector.js";
import { init as initOutput } from "./regions/output.js";

const deps = { store, api };
window.store = store;
initPalette(document.getElementById("region-palette"), deps);
initCanvas(document.getElementById("cw"), deps);
initInspector(document.getElementById("region-inspector"), deps);
initOutput(document.getElementById("region-output"), deps);

store.loadDoc();

/* ---- empty canvas startup: offer starter examples once on first load of a blank doc ---- */
(function () {
  let checked = false;
  store.subscribe("doc", function (d) {
    if (checked || !d) return;
    checked = true;
    const res = d.spec && d.spec.resources || [];
    if (res.length === 0) {
      try {
        const offered = localStorage.getItem("cf:empty-start-offered");
        if (!offered) {
          localStorage.setItem("cf:empty-start-offered", "1");
          const exBtn = document.getElementById("examplesBtn");
          if (exBtn) setTimeout(function () { exBtn.click(); }, 100);
        }
      } catch (_) {}
    }
  });
})();


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
      startDrag(e, function (ev) {
        var d = ev.clientX - sx;
        if (side === "l") widths.l = start + d; else widths.r = start - d;
        apply(); place();
      });
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


/* ---- floating & movable panels (Inspector & Code Drawer) ---- */
(function () {
  var insp = document.getElementById("region-inspector");
  var drawer = document.getElementById("region-output");
  var floatInspBtn = document.getElementById("pane-float-r");
  var floatDrawerBtn = document.getElementById("drawer-float-btn");
  var minDrawerBtn = document.getElementById("drawer-min-btn");
  var cols = document.getElementById("cols");
  var hr = document.getElementById("col-resize-r");

  var state = {
    inspector: { floated: false, x: 0, y: 0, w: 340, h: 560 },
    drawer: { floated: false, x: 0, y: 0, w: 720, h: 360, min: false }
  };

  try {
    var saved = JSON.parse(localStorage.getItem("cf-panel-float") || "null");
    if (saved) {
      if (saved.inspector) state.inspector = Object.assign(state.inspector, saved.inspector);
      if (saved.drawer) state.drawer = Object.assign(state.drawer, saved.drawer);
    }
  } catch (_) {}

  function save() {
    try { localStorage.setItem("cf-panel-float", JSON.stringify(state)); } catch (_) {}
  }

  function makeDraggable(el, handleSelector, onMove) {
    var handle = el.querySelector(handleSelector) || el;
    handle.addEventListener("pointerdown", function (e) {
      if (e.target.closest("button") || e.target.closest("input") || e.target.closest("select") || e.target.closest(".seg") || e.target.closest(".tabs")) return;
      if (!el.classList.contains("floated-panel")) return;
      e.preventDefault();
      el.classList.add("dragging");
      var rect = el.getBoundingClientRect();
      var offX = e.clientX - rect.left;
      var offY = e.clientY - rect.top;

      startDrag(e, function (ev) {
        var x = Math.max(10, Math.min(window.innerWidth - el.offsetWidth - 10, ev.clientX - offX));
        var y = Math.max(48, Math.min(window.innerHeight - 50, ev.clientY - offY));
        el.style.left = x + "px";
        el.style.top = y + "px";
        el.style.right = "auto";
        el.style.bottom = "auto";
        if (onMove) onMove(x, y);
      }, function () {
        el.classList.remove("dragging");
        save();
      });
    });
  }

  function applyInspector() {
    if (!insp || !floatInspBtn) return;
    if (state.inspector.floated) {
      insp.classList.add("floated-panel");
      floatInspBtn.textContent = "🔒";
      floatInspBtn.title = "Dock inspector (Lock in place)";
      var x = state.inspector.x || (window.innerWidth - 370);
      var y = state.inspector.y || 60;
      x = Math.max(10, Math.min(window.innerWidth - 200, x));
      y = Math.max(48, Math.min(window.innerHeight - 100, y));
      insp.style.left = x + "px";
      insp.style.top = y + "px";
      insp.style.right = "auto";
      insp.style.bottom = "auto";
      if (state.inspector.w) insp.style.width = state.inspector.w + "px";
      if (state.inspector.h) insp.style.height = state.inspector.h + "px";
      if (hr) hr.style.display = "none";
      if (cols) cols.style.gridTemplateColumns = (cols.style.gridTemplateColumns.split(" ")[0] || "216px") + " 1fr 0px";
    } else {
      insp.classList.remove("floated-panel");
      floatInspBtn.textContent = "⛶";
      floatInspBtn.title = "Float inspector window (Move freely)";
      insp.style.left = "";
      insp.style.top = "";
      insp.style.right = "";
      insp.style.bottom = "";
      insp.style.width = "";
      insp.style.height = "";
      if (hr) hr.style.display = "";
      // re-trigger column width apply
      window.dispatchEvent(new Event("resize"));
    }
  }

  function applyDrawer() {
    if (!drawer || !floatDrawerBtn) return;
    if (state.drawer.floated) {
      drawer.classList.add("floated-panel");
      drawer.classList.toggle("minimized", !!state.drawer.min);
      floatDrawerBtn.textContent = "🔒";
      floatDrawerBtn.title = "Dock editor (Lock at bottom)";
      if (minDrawerBtn) minDrawerBtn.textContent = state.drawer.min ? "▴" : "▾";
      var x = state.drawer.x || 230;
      var y = state.drawer.y || (window.innerHeight - (state.drawer.min ? 60 : 380));
      x = Math.max(10, Math.min(window.innerWidth - 200, x));
      y = Math.max(48, Math.min(window.innerHeight - 40, y));
      drawer.style.left = x + "px";
      drawer.style.top = y + "px";
      drawer.style.right = "auto";
      drawer.style.bottom = "auto";
      if (state.drawer.w) drawer.style.width = state.drawer.w + "px";
      if (state.drawer.h && !state.drawer.min) drawer.style.height = state.drawer.h + "px";
    } else {
      drawer.classList.remove("floated-panel");
      drawer.classList.remove("minimized");
      floatDrawerBtn.textContent = "⛶";
      floatDrawerBtn.title = "Float editor window (Move freely)";
      if (minDrawerBtn) minDrawerBtn.textContent = "▾";
      drawer.style.left = "";
      drawer.style.top = "";
      drawer.style.right = "";
      drawer.style.bottom = "";
      drawer.style.width = "";
      drawer.style.height = "212px";
    }
  }

  if (insp) {
    makeDraggable(insp, ".pane-h", function (x, y) {
      state.inspector.x = x; state.inspector.y = y;
    });
    if (floatInspBtn) floatInspBtn.addEventListener("click", function () {
      state.inspector.floated = !state.inspector.floated;
      save();
      applyInspector();
    });
    insp.addEventListener("mouseup", function () {
      if (insp.classList.contains("floated-panel")) {
        state.inspector.w = insp.offsetWidth;
        state.inspector.h = insp.offsetHeight;
        save();
      }
    });
  }

  if (drawer) {
    makeDraggable(drawer, ".drawer-h", function (x, y) {
      state.drawer.x = x; state.drawer.y = y;
    });
    if (floatDrawerBtn) floatDrawerBtn.addEventListener("click", function () {
      state.drawer.floated = !state.drawer.floated;
      save();
      applyDrawer();
    });
    if (minDrawerBtn) minDrawerBtn.addEventListener("click", function () {
      if (state.drawer.floated) {
        state.drawer.min = !state.drawer.min;
        save();
        applyDrawer();
      } else {
        // when docked, minimize collapses/expands height
        var h = drawer.style.height;
        if (h === "38px") drawer.style.height = "212px";
        else drawer.style.height = "38px";
      }
    });
    drawer.addEventListener("mouseup", function () {
      if (drawer.classList.contains("floated-panel") && !state.drawer.min) {
        state.drawer.w = drawer.offsetWidth;
        state.drawer.h = drawer.offsetHeight;
        save();
      }
    });
  }

  // Only float on desktop viewports by default; on narrow screens keep overlay mode
  if (window.innerWidth > 900) {
    applyInspector();
    applyDrawer();
  }
})();


/* ---- narrow-screen drawers: panes slide over instead of vanishing ---- */
(function () {
  var l = document.querySelector(".pane.l");
  var r = document.querySelector(".pane.r");
  var d = document.getElementById("region-output");
  var backdrop = document.getElementById("drawerBackdrop");
  var toggleL = document.getElementById("pane-toggle-l");
  var toggleR = document.getElementById("pane-toggle-r");
  var toggleD = document.getElementById("pane-toggle-drawer");
  var closeR = document.getElementById("pane-close-r");

  function syncBackdrop() {
    if (!backdrop) return;
    var isOpen = (l && l.classList.contains("drawer-open")) ||
                 (r && r.classList.contains("drawer-open")) ||
                 (d && d.classList.contains("drawer-open"));
    backdrop.hidden = !isOpen || !window.matchMedia("(max-width:900px)").matches;
  }

  function closeAll() {
    if (l) l.classList.remove("drawer-open");
    if (r) r.classList.remove("drawer-open");
    if (d) d.classList.remove("drawer-open");
    syncBackdrop();
  }

  if (backdrop) backdrop.addEventListener("click", closeAll);

  if (toggleL && l) toggleL.addEventListener("click", function () {
    l.classList.toggle("drawer-open");
    if (r) r.classList.remove("drawer-open");
    if (d) d.classList.remove("drawer-open");
    syncBackdrop();
  });

  if (toggleR && r) toggleR.addEventListener("click", function () {
    r.classList.toggle("drawer-open");
    if (l) l.classList.remove("drawer-open");
    if (d) d.classList.remove("drawer-open");
    syncBackdrop();
  });

  if (toggleD && d) toggleD.addEventListener("click", function () {
    d.classList.toggle("drawer-open");
    if (l) l.classList.remove("drawer-open");
    if (r) r.classList.remove("drawer-open");
    syncBackdrop();
  });

  if (closeR && r) closeR.addEventListener("click", function () {
    r.classList.remove("drawer-open");
    syncBackdrop();
  });

  // selecting something opens the inspector drawer on narrow screens
  store.subscribe("selection", function (sel) {
    if (!r || !window.matchMedia("(max-width:900px)").matches) return;
    if (sel) {
      r.classList.add("drawer-open");
      if (l) l.classList.remove("drawer-open");
      if (d) d.classList.remove("drawer-open");
      syncBackdrop();
    }
  });

  var mq = window.matchMedia("(max-width:900px)");
  (mq.addEventListener ? mq.addEventListener.bind(mq, "change") : mq.addListener.bind(mq))(function (e) {
    if (!r) return;
    if (e.matches) {
      if (store.state.selectedResource) r.classList.add("drawer-open");
    } else {
      closeAll();
    }
    syncBackdrop();
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

  

  function renderExamples(list) {
    if (!list || !list.length) {
      grid.innerHTML = '<div class="empty">No starter examples available.</div>';
      return;
    }
    var html = "";
    list.forEach(function (ex) {
      var ic = ex.icon || { label: "EX", color: "var(--wire-xrd)" };
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

  var lastFocusedElement = null;

  function openModal() {
    lastFocusedElement = document.activeElement;
    overlay.hidden = false;
    loadExamples();
    setTimeout(function () {
      if (closeBtn) closeBtn.focus();
    }, 30);
  }

  function closeModal() {
    overlay.hidden = true;
    if (lastFocusedElement && typeof lastFocusedElement.focus === "function") {
      lastFocusedElement.focus();
    }
  }

  btn.addEventListener("click", openModal);
  if (closeBtn) closeBtn.addEventListener("click", closeModal);
  overlay.addEventListener("click", function (e) {
    if (e.target === overlay) closeModal();
  });

  addEventListener("keydown", function (e) {
    if (overlay.hidden) return;
    if (e.key === "Escape") {
      closeModal();
      return;
    }
    if (e.key === "Tab") {
      var focusables = overlay.querySelectorAll('button:not([disabled]), [tabindex]:not([tabindex="-1"]), input:not([disabled]), select:not([disabled])');
      if (!focusables || !focusables.length) return;
      var first = focusables[0];
      var last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  });

  grid.addEventListener("click", function (e) {
    var loadBtn = e.target.closest("[data-load-id]");
    if (!loadBtn) return;
    var id = loadBtn.getAttribute("data-load-id");
    var ex = (cachedExamples || []).find(function (item) { return item.id === id; });
    if (!ex) return;

    loadBtn.disabled = true;
    loadBtn.textContent = "Loading…";
    var p = (store.loadExample && typeof store.loadExample === "function")
      ? store.loadExample(id)
      : store.importBlueprint(ex.yaml);
    p.then(function (doc) {
      if (doc) store.select(null);
      closeModal();
    }).catch(function (err) {
      alert("Failed to load example: " + (err && err.message || err));
    }).finally(function () {
      loadBtn.disabled = false;
      loadBtn.textContent = "Load Blueprint";
    });
  });
})();

