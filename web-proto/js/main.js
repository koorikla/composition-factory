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
import { mapResourceCoordinates } from "./utils.js";
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


/* ---- global error toast for rejected store actions (CF-011, CF-136) ---- */
let toastTimer = null;
export function showErrorToast(msg) {
  if (!msg) return;
  let t = document.getElementById("canvas-error-toast");
  if (!t) {
    t = document.createElement("div");
    t.id = "canvas-error-toast";
    t.className = "toast-bar";
    t.style.borderColor = "var(--err)";
    document.body.appendChild(t);
  }
  t.innerHTML = '<span style="color:var(--err)">⚠️</span> <span class="toast-msg" style="flex:1">' + esc(msg) + '</span> <button class="toast-close" style="background:none;border:none;color:var(--dim);cursor:pointer;font-size:14px;padding:0 4px">&times;</button>';
  t.querySelector(".toast-close").onclick = function () {
    clearErrorToast();
  };
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(function () {
    clearErrorToast();
  }, 6000);
}

export function clearErrorToast() {
  if (toastTimer) {
    clearTimeout(toastTimer);
    toastTimer = null;
  }
  let t = document.getElementById("canvas-error-toast");
  if (t) {
    t.remove();
  }
}
window.clearErrorToast = clearErrorToast;

store.subscribe("error", function (err) {
  if (err && err.message) {
    showErrorToast(mapResourceCoordinates(err.message));
  }
});

// The toast must not outlive the next successful action (CF-136, CF-147)
store.subscribe("doc", function () {
  clearErrorToast();
});

store.subscribe("generate", function () {
  clearErrorToast();
});

store.subscribe("selection", function () {
  clearErrorToast();
});

/* ---- resizable side columns: drag handles, clamped, persisted ---- */
(function () {
  var cols = document.getElementById("cols");
  if (!cols) return;
  var MIN_L = 180, MAX_L = 420, MIN_R = 260, MAX_R = 520, MIN_CANVAS = 300;
  var widths = { l: 216, r: 330 };
  try {
    var saved = JSON.parse(localStorage.getItem("cf-col-widths") || "null");
    if (saved && saved.l && saved.r) widths = saved;
  } catch (_) { /* private mode */ }

  function save() {
    try { localStorage.setItem("cf-col-widths", JSON.stringify(widths)); } catch (_) { /* ok */ }
  }

  function apply(persist) {
    var total = cols.getBoundingClientRect().width;
    var l = Math.min(MAX_L, Math.max(MIN_L, widths.l));
    var r = Math.min(MAX_R, Math.max(MIN_R, widths.r));
    if (total && total - l - r < MIN_CANVAS) {
      r = Math.max(MIN_R, total - l - MIN_CANVAS);
      l = Math.max(MIN_L, Math.min(l, total - r - MIN_CANVAS));
    }
    widths.l = l; widths.r = r;
    cols.style.gridTemplateColumns = l + "px 1fr " + r + "px";
    if (persist) {
      save();
    }
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
        apply(false); place();
      }, function () {
        save();
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
  apply(false); place();
  addEventListener("resize", function () { apply(true); place(); });
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
      drawer.style.height = "200px";
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
        var isCollapsed = h === "38px" || drawer.hasAttribute("data-collapsed") || drawer.classList.contains("collapsed");
        if (isCollapsed) {
          drawer.removeAttribute("data-collapsed");
          drawer.classList.remove("collapsed");
          drawer.style.height = "200px";
          if (minDrawerBtn) minDrawerBtn.textContent = "▾";
        } else {
          drawer.setAttribute("data-collapsed", "");
          drawer.classList.add("collapsed");
          drawer.style.height = "38px";
          if (minDrawerBtn) minDrawerBtn.textContent = "▴";
        }
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
var cachedBlueprintName = "";

function getTargetBlueprintName() {
  if (cachedBlueprintName) return cachedBlueprintName;
  var doc = store && store.state && store.state.doc;
  var name = (doc && doc.metadata && doc.metadata.name) || "blueprint";
  return name + ".cf.yaml";
}

function ensureBp() {
  if (cachedBlueprintName) return Promise.resolve(cachedBlueprintName);
  return api.getVersion().then(function (r) {
    if (r && r.blueprint) {
      var bp = r.blueprint;
      var slashIdx = Math.max(bp.lastIndexOf("/"), bp.lastIndexOf("\\"));
      cachedBlueprintName = slashIdx >= 0 ? bp.slice(slashIdx + 1) : bp;
    }
    return cachedBlueprintName;
  }).catch(function () { return ""; });
}

(function () {
  var btn = document.getElementById("importBtn");
  var file = document.getElementById("importFile");
  if (!btn || !file) return;

  function updateImportTooltip() {
    var targetBp = cachedBlueprintName || getTargetBlueprintName();
    btn.title = "Import a blueprint .yaml, or adopt Crossplane Composition & XRD (combine or select both files for lossless adoption; replaces " + targetBp + " \u00b7 undoable)";
  }

  ensureBp().then(function () {
    updateImportTooltip();
  });
  store.subscribe("doc", updateImportTooltip);
  updateImportTooltip();

  btn.addEventListener("click", function () { file.click(); });
  store.subscribe("error", function (e) {
    if (!e || e.source !== "importBlueprint") return;
    notice("import failed: " + e.message, true);
  });
  store.subscribe("error", function (e) {
    if (!e || e.source !== "adoptComposition") return;
    var msg = (e.message || "").replace(/^adopt failed:\s*/i, "");
    notice("adopt failed: " + msg, true);
  });

  file.addEventListener("change", function () {
    var files = file.files ? Array.from(file.files) : [];
    file.value = "";
    if (files.length === 0) return;

    var targetBp = cachedBlueprintName || getTargetBlueprintName();
    var ok = window.confirm("Import will replace " + targetBp + " (undoable).\n\nProceed?");
    if (!ok) return;

    var prevDoc = store.state.doc ? JSON.parse(JSON.stringify(store.state.doc)) : null;

    var readPromises = files.map(function (f) {
      return new Promise(function (resolve, reject) {
        var reader = new FileReader();
        reader.onload = function () { resolve(String(reader.result)); };
        reader.onerror = function (err) { reject(err); };
        reader.readAsText(f);
      });
    });

    Promise.all(readPromises).then(function (texts) {
      var text = texts.join("\n---\n");
      // One Import button, two source formats: cf's own blueprint goes through
      // the import gate unchanged, and a real Crossplane Composition is adopted
      // into one. Routing on the manifest's own `kind:` means the user does not
      // have to know which of cf's two front doors their file belongs to.
      var isComp = isCompositionManifest(text);
      var op = isComp
        ? store.adoptComposition(text)
        : store.importBlueprint(text);
      op.then(function (doc) {
        if (!doc) return; // failures surface through the store's error topic
        store.select(null);
        showImportToast(prevDoc, doc, isComp);
        if (isComp) {
          reportAdoptLoss();
        } else {
          clearNotice();
        }
      });
    }).catch(function (err) {
      notice("failed to read file: " + (err && err.message ? err.message : err), true);
    });
  });

  function summarizeChanges(prevDoc, nextDoc) {
    var changes = [];
    if (!prevDoc || !nextDoc) return changes;

    // 1. metadata.name
    var prevName = (prevDoc.metadata && prevDoc.metadata.name) || "untitled";
    var nextName = (nextDoc.metadata && nextDoc.metadata.name) || "untitled";
    if (prevName !== nextName) {
      changes.push("name (" + prevName + " \u2192 " + nextName + ")");
    }

    // 2. spec.pipeline
    var prevPipe = (prevDoc.spec && prevDoc.spec.pipeline) || [];
    var nextPipe = (nextDoc.spec && nextDoc.spec.pipeline) || [];
    if (prevPipe.length === 0 && nextPipe.length > 0) {
      var stepNames = nextPipe.map(function (s) { return s.name || s.step; }).filter(Boolean).join(", ");
      changes.push("pipeline materialized (" + (stepNames || "custom step") + ")");
    } else if (prevPipe.length > 0 && nextPipe.length === 0) {
      changes.push("pipeline reset to default");
    } else if (JSON.stringify(prevPipe) !== JSON.stringify(nextPipe)) {
      changes.push("pipeline updated");
    }

    // 3. spec.xrd.parameters
    var prevParams = (prevDoc.spec && prevDoc.spec.xrd && prevDoc.spec.xrd.parameters) || {};
    var nextParams = (nextDoc.spec && nextDoc.spec.xrd && nextDoc.spec.xrd.parameters) || {};
    var allParamKeys = {};
    Object.keys(prevParams).forEach(function (k) { allParamKeys[k] = true; });
    Object.keys(nextParams).forEach(function (k) { allParamKeys[k] = true; });

    Object.keys(allParamKeys).forEach(function (p) {
      if (prevParams[p] && !nextParams[p]) {
        changes.push("parameter $" + p + " removed");
      } else if (!prevParams[p] && nextParams[p]) {
        changes.push("parameter $" + p + " added");
      } else if (prevParams[p] && nextParams[p]) {
        var pp = prevParams[p];
        var np = nextParams[p];
        if ((pp.description || "") !== (np.description || "")) {
          changes.push("parameter $" + p + " description rewritten");
        }
        if (pp.type !== np.type) {
          changes.push("parameter $" + p + " type changed (" + pp.type + " \u2192 " + np.type + ")");
        }
        if (Boolean(pp.required) !== Boolean(np.required)) {
          var oldFlag = pp.required ? "required" : "optional";
          var newFlag = np.required ? "required" : "optional";
          changes.push("parameter $" + p + " required changed (" + oldFlag + " \u2192 " + newFlag + ")");
        }
      }
    });

    // 4. spec.resources
    var prevRes = (prevDoc.spec && prevDoc.spec.resources) || [];
    var nextRes = (nextDoc.spec && nextDoc.spec.resources) || [];
    var prevResMap = {};
    var nextResMap = {};
    prevRes.forEach(function (r) { if (r && r.name) prevResMap[r.name] = r; });
    nextRes.forEach(function (r) { if (r && r.name) nextResMap[r.name] = r; });

    var addedRes = [];
    var removedRes = [];
    var modifiedRes = [];

    Object.keys(nextResMap).forEach(function (n) {
      if (!prevResMap[n]) {
        addedRes.push(n);
      } else {
        if (JSON.stringify(prevResMap[n]) !== JSON.stringify(nextResMap[n])) {
          modifiedRes.push(n);
        }
      }
    });
    Object.keys(prevResMap).forEach(function (n) {
      if (!nextResMap[n]) {
        removedRes.push(n);
      }
    });

    if (addedRes.length) changes.push("resources added (" + addedRes.join(", ") + ")");
    if (removedRes.length) changes.push("resources removed (" + removedRes.join(", ") + ")");
    if (modifiedRes.length) changes.push("resources modified (" + modifiedRes.join(", ") + ")");

    return changes;
  }

  var importToastTimer = null;

  function showImportToast(prevDoc, nextDoc, isComp) {
    var old = document.getElementById("import-toast");
    if (old) old.remove();
    if (importToastTimer) {
      clearTimeout(importToastTimer);
      importToastTimer = null;
    }

    var docName = (nextDoc && nextDoc.metadata && nextDoc.metadata.name) || "blueprint";
    var changes = summarizeChanges(prevDoc, nextDoc);

    var r = store.state.lastAdoptReport;
    var dropItems = (r && r.drops && r.drops.length) ? r.drops : [];

    var parts = [];
    if (changes.length > 0) {
      parts.push("changed: " + changes.join("; "));
    }
    if (dropItems.length > 0) {
      parts.push("could not carry " + dropItems.length + " dropped item" + (dropItems.length === 1 ? "" : "s") + ": " + dropItems.map(function (d) { return d.path + " (" + d.reason + ")"; }).join(", "));
    }

    var msg = (isComp ? "Adopted " : "Imported ") + docName;
    if (parts.length > 0) {
      msg += " \u2014 " + parts.join(" | ");
    }

    var toast = document.createElement("div");
    toast.id = "import-toast";
    toast.className = "toast-bar";
    toast.setAttribute("role", "status");

    var iconSpan = document.createElement("span");
    iconSpan.textContent = "\ud83d\udce5";
    toast.appendChild(iconSpan);

    var msgSpan = document.createElement("span");
    msgSpan.className = "toast-msg";
    msgSpan.style.flex = "1";
    msgSpan.textContent = msg;
    toast.appendChild(msgSpan);

    var undoBtn = document.createElement("button");
    undoBtn.type = "button";
    undoBtn.className = "toast-link";
    undoBtn.style.background = "none";
    undoBtn.style.border = "none";
    undoBtn.style.padding = "0";
    undoBtn.style.font = "inherit";
    undoBtn.textContent = "Undo (\u21a9)";
    undoBtn.onclick = function () {
      store.undo();
      if (toast.parentNode) toast.remove();
    };
    toast.appendChild(undoBtn);

    var closeBtn = document.createElement("button");
    closeBtn.type = "button";
    closeBtn.className = "del modal-close";
    closeBtn.style.marginLeft = "6px";
    closeBtn.setAttribute("aria-label", "Dismiss");
    closeBtn.textContent = "\u00d7";
    closeBtn.onclick = function () {
      if (toast.parentNode) toast.remove();
    };
    toast.appendChild(closeBtn);

    document.body.appendChild(toast);
    importToastTimer = setTimeout(function () {
      if (toast.parentNode) toast.remove();
      importToastTimer = null;
    }, 12000);
  }

  /**
   * True when the YAML stream is a Crossplane Composition to be adopted rather
   * than something the import gate can take losslessly. Scans every document's
   * `kind:`, so a multi-document stream routes on what it contains, not on
   * whichever document happens to come first.
   *
   * Two things outrank the Composition in a stream, because for both of them
   * import recovers the original blueprint exactly and adopt would only
   * approximate it: cf's own Blueprint, and a Configuration package that
   * carries the embedded blueprint annotation `cf package` writes. A
   * third-party Configuration has no such annotation, so its Composition is
   * still adopted — which is the whole point of the feature.
   */
  function isCompositionManifest(text) {
    var s = String(text);
    if (s.indexOf("factory.crossplane.io/blueprint") !== -1) return false;
    var kinds = [];
    s.split(/^---[ \t]*$/m).forEach(function (doc) {
      var m = /^kind:[ \t]*["']?([A-Za-z0-9_.-]+)/m.exec(doc);
      if (m) kinds.push(m[1]);
    });
    if (kinds.indexOf("Blueprint") !== -1) return false;
    return kinds.indexOf("Composition") !== -1 ||
      kinds.indexOf("CompositeResourceDefinition") !== -1;
  }

  /**
   * Adoption cannot carry everything a hand-written Composition expresses, and
   * silently keeping a partial import is the one outcome that would waste the
   * user's time. Say what was dropped, and where.
   */
  function reportAdoptLoss() {
    var r = store.state.lastAdoptReport;
    if (!r || !r.drops || !r.drops.length) {
      clearNotice();
      return;
    }
    var head = "adopted with " + r.drops.length + " dropped item" +
      (r.drops.length === 1 ? "" : "s") + ": ";
    notice(head + r.drops.map(function (d) { return d.path + " (" + d.reason + ")"; }).join("; "), false, true);
  }
})();

var noticeTimer = null;

function clearNotice() {
  if (noticeTimer) {
    clearTimeout(noticeTimer);
    noticeTimer = null;
  }
  var bar = document.getElementById("import-warn");
  if (bar) {
    bar.hidden = true;
  }
}

/** Shared warn bar under the topbar; isError picks the alert role; persistent prevents auto-hiding. */
function notice(text, isError, persistent) {
  if (noticeTimer) {
    clearTimeout(noticeTimer);
    noticeTimer = null;
  }
  var bar = document.getElementById("import-warn");
  if (!bar) {
    bar = document.createElement("div");
    bar.id = "import-warn";
    bar.className = "warnbar";
    var host = document.getElementById("region-topbar") || document.body;
    host.parentNode.insertBefore(bar, host.nextSibling);
  }
  bar.setAttribute("role", isError ? "alert" : "status");
  bar.hidden = false;
  bar.removeAttribute("hidden");
  bar.innerHTML = "";

  var textSpan = document.createElement("span");
  textSpan.style.flex = "1";
  textSpan.textContent = text;
  bar.appendChild(textSpan);

  var dismissBtn = document.createElement("button");
  dismissBtn.type = "button";
  dismissBtn.className = "del modal-close warnbar-dismiss";
  dismissBtn.setAttribute("aria-label", "Dismiss");
  dismissBtn.setAttribute("title", "Dismiss");
  dismissBtn.textContent = "\u00d7";
  dismissBtn.onclick = function () {
    bar.hidden = true;
    if (noticeTimer) {
      clearTimeout(noticeTimer);
      noticeTimer = null;
    }
  };
  bar.appendChild(dismissBtn);

  if (!persistent) {
    noticeTimer = setTimeout(function () {
      bar.hidden = true;
      noticeTimer = null;
    }, isError ? 8000 : 12000);
  }
}


/* ---- package: one-click Configuration .xpkg download ---- */
(function () {
  var btn = document.getElementById("packageBtn");
  if (!btn) return;
  btn.addEventListener("click", function () {
    fetch("/api/package")
      .then(function (res) {
        if (!res.ok) {
          return res.json().catch(function () { return {}; }).then(function (data) {
            notice((data && data.error) || ("package build failed (" + res.status + ")"), true);
          });
        }
        var filename = "package.xpkg";
        var disp = res.headers.get("Content-Disposition");
        if (disp) {
          var m = /filename\*=(?:UTF-8'')?([^;\r\n]+)/i.exec(disp) ||
                  /filename="?([^";\r\n]+)"?/i.exec(disp);
          if (m && m[1]) {
            filename = decodeURIComponent(m[1].replace(/['"]/g, "").trim());
          }
        }
        return res.blob().then(function (blob) {
          var url = URL.createObjectURL(blob);
          var a = document.createElement("a");
          a.href = url;
          a.download = filename;
          document.body.appendChild(a);
          a.click();
          a.remove();
          setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
        });
      })
      .catch(function (err) {
        notice("package download failed: " + ((err && err.message) || String(err)), true);
      });
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
    var targetBp = esc(cachedBlueprintName || getTargetBlueprintName());
    var html = "";
    list.forEach(function (ex) {
      var ic = ex.icon || { label: "EX", color: "var(--wire-xrd)" };
      var tagsHtml = (ex.tags || []).map(function (t) {
        return '<span class="example-tag">' + esc(t) + '</span>';
      }).join("");
      var resLabel = ex.resourceCount ? (ex.resourceCount + " resources") : "";
      // Loading an example syncs its provider schemas, so a card whose
      // providers are not cached costs a download and cannot work offline.
      // Say so on the card rather than letting the user find out from a
      // failed load — this chooser opens itself on a blank first run.
      var n = (ex.missingSources || []).length;
      var readyHtml = ex.sourcesReady
        ? '<span class="example-tag example-ready" title="' +
            ((ex.sources || []).length
              ? "Provider schemas are cached — loads offline"
              : "No providers needed — native Kubernetes kinds only") +
            '">\u25cf ready</span>'
        : '<span class="example-tag example-fetch" title="' +
            esc("Downloads on load: " + (ex.missingSources || []).join(", ")) +
            '">\u2193 ' + n + ' provider' + (n === 1 ? "" : "s") + '</span>';

      html += '<div class="example-card" data-id="' + esc(ex.id) + '">' +
        '<div class="example-card-h">' +
        '<span class="example-icon" style="background:' + ic.color + '">' + ic.label + '</span>' +
        '<div style="min-width:0;flex:1">' +
        '<div class="example-title">' + esc(ex.name) + '</div>' +
        '</div>' +
        '</div>' +
        '<div class="example-desc">' + esc(ex.description) + '</div>' +
        '<div class="example-tags">' +
        readyHtml +
        (resLabel ? '<span class="example-tag" style="background:var(--wire-xrd-soft);color:var(--wire-xrd)">' + esc(resLabel) + '</span>' : '') +
        tagsHtml +
        '</div>' +
        '<button class="btn pri sm example-btn" data-load-id="' + esc(ex.id) + '">Load Blueprint</button>' +
        '<div class="example-note" style="font-size:10px;color:var(--faint);text-align:center;margin-top:3px">(replaces ' + targetBp + ' \u00b7 undoable)</div>' +
        '</div>';
    });
    grid.innerHTML = html;
  }

  function loadExamples() {
    if (cachedExamples) {
      ensureBp().then(function () {
        renderExamples(cachedExamples);
      });
      return;
    }
    grid.innerHTML = '<div class="empty">Loading examples…</div>';
    Promise.all([
      api.getExamples(),
      ensureBp(),
    ]).then(function (res) {
      var data = res[0];
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
    overlay.removeAttribute("hidden");
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

  function showCardError(card, msg) {
    if (!card) return;
    card.classList.add("has-error");
    card.style.borderColor = "var(--err)";
    var errEl = card.querySelector(".example-card-error");
    if (!errEl) {
      errEl = document.createElement("div");
      errEl.className = "example-card-error";
      errEl.setAttribute("role", "alert");
      var btn = card.querySelector(".example-btn");
      if (btn && btn.nextSibling) {
        card.insertBefore(errEl, btn.nextSibling);
      } else {
        card.appendChild(errEl);
      }
    }
    errEl.textContent = msg;
  }

  grid.addEventListener("click", function (e) {
    var loadBtn = e.target.closest("[data-load-id]");
    if (!loadBtn) return;
    var id = loadBtn.getAttribute("data-load-id");
    var ex = (cachedExamples || []).find(function (item) { return item.id === id; });
    if (!ex) return;

    var card = loadBtn.closest(".example-card");
    if (card) {
      card.classList.remove("has-error");
      card.style.borderColor = "";
      var prevErr = card.querySelector(".example-card-error");
      if (prevErr) prevErr.remove();
    }

    loadBtn.disabled = true;
    loadBtn.textContent = "Loading…";
    var p = (store.loadExample && typeof store.loadExample === "function")
      ? store.loadExample(id)
      : store.importBlueprint(ex.yaml);
    p.then(function (doc) {
      if (!doc || doc.error) {
        showCardError(card, (doc && doc.error) || "Failed to load example: empty blueprint returned");
        return;
      }
      store.select(null);
      closeModal();
    }).catch(function (err) {
      var msg = (err && err.message) || String(err || "Failed to load example");
      showCardError(card, msg);
    }).finally(function () {
      loadBtn.disabled = false;
      loadBtn.textContent = "Load Blueprint";
    });
  });
})();

