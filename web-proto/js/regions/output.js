/**
 * Region: OUTPUT + TOPBAR behavior.
 *
 * export init(rootEl, {store, api}) — the integrator calls it once with
 * rootEl = #region-output. This module also drives the topbar controls
 * (#crumb #ver #valid #themeBtn #validateBtn #generateBtn) because the
 * topbar's live data (blueprint name, generate status) is owned by this
 * region's generate cycle.
 *
 * Behavior (live, replacing the prototype's fake render()):
 *  - tabs: composition.yaml | definition.yaml | <name>.cf.yaml
 *      comp/xrd bodies come from POST /api/generate {write:false} outputs,
 *      matched by path (/compositions/ vs /xrds/, with a kind: sniff
 *      fallback); the third tab renders store.state.doc as YAML via a tiny
 *      JSON-to-YAML indent walk (no library).
 *  - meta caption: "<N> lines · deterministic" (prototype copy).
 *  - warnbar: the prototype's exact raw-template copy, count = fields in the
 *    doc whose form is {raw} (server pads absent keys with "" — "" = absent).
 *  - regenerate: debounced 300ms on every "doc" emit. The store already PUTs;
 *    this region ONLY generates.
 *  - topbar: crumb = blueprints/<name>.cf.yaml from doc metadata, #ver from
 *    doc apiVersion, #valid chip = "ok · N files" / "error" (verbatim server
 *    message in the title), Theme cycles system → light → dark.
 *  - splitter: draggable horizontal divider straddling the drawer's top
 *    border (pointer events, min heights both sides, double-click collapses
 *    to the header). Positioned absolutely so the approved layout is
 *    untouched.
 */

var DEBOUNCE_MS = 300;
var MIN_DRAWER = 64;   // px — keep the header + a few code lines reachable
var MIN_CANVAS = 140;  // px — never let the drawer swallow the canvas

export function init(rootEl, deps) {
  var store = deps.store;
  var api = deps.api;

  var el = {
    root: rootEl,
    tabs: rootEl.querySelector("#tabs") || document.getElementById("tabs"),
    meta: rootEl.querySelector("#meta") || document.getElementById("meta"),
    warn: rootEl.querySelector("#warn") || document.getElementById("warn"),
    code: rootEl.querySelector("#code") || document.getElementById("code"),
    crumb: document.getElementById("crumb"),
    ver: document.getElementById("ver"),
    valid: document.getElementById("valid"),
    themeBtn: document.getElementById("themeBtn"),
    validateBtn: document.getElementById("validateBtn"),
    generateBtn: document.getElementById("generateBtn"),
  };

  var tab = "comp";          // "comp" | "xrd" | "bp"
  var genTimer = null;

  /* ---------- tabs (prototype markup, built live) ---------- */

  function buildTabs() {
    var bpLabel = bpTabLabel(store.state.doc);
    var h =
      '<button data-t="comp" aria-pressed="' + (tab === "comp") + '">composition.yaml</button>' +
      '<button data-t="xrd" aria-pressed="' + (tab === "xrd") + '">definition.yaml</button>' +
      '<button data-t="fns" aria-pressed="' + (tab === "fns") + '">functions.yaml</button>' +
      '<button data-t="bp" aria-pressed="' + (tab === "bp") + '">' + esc(bpLabel) + '</button>';
    h += '<button data-t="pkg" aria-pressed="' + (tab === "pkg") + '">package.yaml</button>';
    // one tab per generated providerconfig family (outputs carry the bodies)
    var g = store.state.lastGenerate;
    (g && g.outputs || []).forEach(function (o) {
      var m = /providerconfigs[\/\\]([^\/\\]+)\.yaml$/.exec(o.path);
      if (!m) return;
      var key = "pc:" + m[1];
      h += '<button data-t="' + key + '" aria-pressed="' + (tab === key) + '">providerconfigs/' + m[1] + ".yaml</button>";
    });
    h += '<button data-t="rbac" aria-pressed="' + (tab === "rbac") + '">rbac</button>';
    el.tabs.innerHTML = h;
  }

  function bpTabLabel(doc) {
    var name = doc && doc.metadata && doc.metadata.name || "blueprint";
    return name + ".cf.yaml";
  }

  el.tabs.addEventListener("click", function (e) {
    var b = e.target.closest("button");
    if (!b || !el.tabs.contains(b)) return;
    tab = b.getAttribute("data-t");
    [].forEach.call(el.tabs.children, function (c) {
      c.setAttribute("aria-pressed", String(c === b));
    });
    render();
  });

  /* ---------- output matching ---------- */

  function matchOutput(which) {
    var g = store.state.lastGenerate;
    var outputs = g && g.outputs || [];
    if (which === "fns") {
      for (var k = 0; k < outputs.length; k++) {
        if (/functions\.yaml$/.test(outputs[k].path)) return outputs[k];
      }
      return null;
    }
    var dirRe = which === "comp" ? /[\\/]compositions[\\/]/ : /[\\/]xrds[\\/]/;
    var kindRe = which === "comp"
      ? /^kind:\s*Composition\s*$/m
      : /^kind:\s*CompositeResourceDefinition\s*$/m;
    for (var i = 0; i < outputs.length; i++) {
      if (dirRe.test(outputs[i].path)) return outputs[i];
    }
    for (var j = 0; j < outputs.length; j++) {
      if (kindRe.test(outputs[j].body || "")) return outputs[j];
    }
    return null;
  }

  /* ---------- rendering ---------- */

  function esc(s) {
    return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  /** Prototype-style YAML highlighting over plain text. */
  function highlight(text) {
    return text.split("\n").map(function (line) {
      if (/^\s*#/.test(line)) return '<span class="cm">' + esc(line) + "</span>";
      var m = line.match(/^(\s*(?:-\s+)?)([\w.$\/"'\-]+):(\s*)(.*)$/);
      if (m) {
        var val = m[4];
        var h = esc(m[1]) + '<span class="kk">' + esc(m[2]) + "</span>:" + m[3];
        if (val) {
          var cls = /\{\{/.test(val) ? "tm" : "st";
          h += '<span class="' + cls + '">' + esc(val) + "</span>";
        }
        return h;
      }
      if (/\{\{/.test(line)) return '<span class="tm">' + esc(line) + "</span>";
      return esc(line);
    }).join("\n");
  }

  var rbacCache = null; // invalidated on every doc emit
  var pkgCache = null;  // same lifecycle: the package renders the live doc

  function currentText() {
    if (tab === "bp") {
      var doc = store.state.doc;
      return doc ? toYaml(doc) : "";
    }
    if (tab.indexOf("pc:") === 0) {
      var fam = tab.slice(3);
      var g = store.state.lastGenerate;
      var pcs = (g && g.outputs || []).filter(function (o) {
        return new RegExp("providerconfigs[/\\\\]" + fam + "\\.yaml$").test(o.path);
      });
      return pcs.length ? pcs[0].body : "";
    }
    if (tab === "pkg") {
      if (pkgCache) return pkgCache;
      api.getPackageYAML().then(function (text) {
        pkgCache = text;
        if (tab === "pkg") render();
      }).catch(function (e) { pkgCache = "# package unavailable: " + (e && e.message || e); if (tab === "pkg") render(); });
      return "# building package\u2026";
    }
    if (tab === "rbac") {
      if (rbacCache) return rbacCache;
      api.getRBAC().then(function (r) {
        rbacCache = (r.rules || []).map(function (rule) {
          return "- apiGroups: [" + rule.apiGroups.join(", ") + "]\n" +
            "  resources: [" + rule.resources.join(", ") + "]\n" +
            "  verbs: [" + rule.verbs.join(", ") + "]\n" +
            "  # scope: " + rule.scope;
        }).join("\n");
        if (tab === "rbac") render();
      }).catch(function (e) { rbacCache = "# rbac unavailable: " + (e && e.message || e); if (tab === "rbac") render(); });
      return "# loading rbac\u2026";
    }
    var out = matchOutput(tab);
    return out ? out.body : "";
  }

  function pickerNote() {
    // annotate the family scaffold with the client-side kind picker state
    var fam = tab.slice(3);
    var hidden = {};
    try { hidden = JSON.parse(localStorage.getItem("cf-hidden-kinds")) || {}; } catch (_) { /* none */ }
    var doc = store.state.doc;
    var lines = [];
    (doc && doc.spec && doc.spec.sources || []).forEach(function (src) {
      var ref = src.provider || "";
      var m = /provider-([a-z0-9]+)-/.exec(ref);
      var srcFam = m ? m[1] : ref.split("/").pop().replace(/^provider-/, "").split(":")[0];
      if (srcFam !== fam) return;
      var n = (hidden[ref] || []).length;
      var name = ref.split("/").pop();
      lines.push(name + (n ? " \u2014 " + n + " kind" + (n > 1 ? "s" : "") + " hidden in the palette" : " \u2014 all kinds enabled"));
    });
    return lines.join(" \u00b7 ");
  }

  function render() {
    var text = currentText();
    var note = document.getElementById("pc-note");
    if (tab.indexOf("pc:") === 0) {
      if (!note) {
        note = document.createElement("div");
        note.id = "pc-note";
        note.className = "dg";
        note.style.cssText = "padding:4px 12px;border-bottom:1px solid var(--rule)";
        el.code.parentNode.insertBefore(note, el.code);
      }
      note.hidden = false;
      note.textContent = pickerNote();
    } else if (note) {
      note.hidden = true;
    }
    el.code.innerHTML = highlight(text);
    var lines = text ? text.split("\n").filter(function (l, i, a) {
      return !(i === a.length - 1 && l === "");
    }).length : 0;
    el.meta.textContent = lines + " lines · deterministic";
  }

  /* ---------- render-failure bar (separate from the raw-count warnbar,
     which is re-rendered on every doc change) ---------- */
  function showWarn(message) {
    var bar = document.getElementById("render-warn");
    if (!bar) {
      bar = document.createElement("div");
      bar.id = "render-warn";
      bar.className = "warnbar";
      bar.setAttribute("role", "alert");
      el.warn.parentNode.insertBefore(bar, el.warn.nextSibling);
    }
    bar.hidden = !message;
    bar.textContent = message || "";
  }

  /* ---------- raw-template warnbar (prototype's exact copy) ---------- */

  function countRaws(doc) {
    var n = 0;
    var resources = doc && doc.spec && doc.spec.resources || [];
    resources.forEach(function (r) {
      var fields = r.fields || {};
      Object.keys(fields).forEach(function (k) {
        var f = fields[k];
        if (f && typeof f.raw === "string" && f.raw !== "") n++;
      });
    });
    return n;
  }

  function drawWarn(doc) {
    var raws = countRaws(doc);
    if (raws) {
      el.warn.hidden = false;
      el.warn.innerHTML = "<span>▲</span><span>" + raws + " field" + (raws > 1 ? "s" : "") +
        ' use a raw template — the canvas can show them but not validate them. <code>missingkey=error</code> still guards them.</span>';
    } else {
      el.warn.hidden = true;
    }
  }

  /* ---------- topbar ---------- */

  function drawTopbar(doc) {
    var name = doc && doc.metadata && doc.metadata.name || "blueprint";
    el.crumb.innerHTML = "blueprints/<b>" + esc(name) + ".cf.yaml</b>";
    // the wordmark shows the BUILD version (the doc's schema version was
    // read as "I'm on an old app" — it lives with the blueprint name now)
    var av = doc && doc.apiVersion || "";
    var schemaV = av.indexOf("/") >= 0 ? av.split("/").pop() : av;
    el.crumb.innerHTML += schemaV ? ' <span class="dg">' + esc(schemaV) + "</span>" : "";
    if (!el.ver.dataset.build) {
      el.ver.dataset.build = "1";
      api.getVersion().then(function (r) {
        el.ver.textContent = r.version;
        el.ver.title = "compositionfactory build " + r.version;
      }).catch(function () { el.ver.textContent = ""; });
    }
    var bp = el.tabs.querySelector('[data-t="bp"]');
    if (bp) bp.textContent = bpTabLabel(doc);
  }

  function chipOk(n) {
    el.valid.textContent = "ok · " + n + " file" + (n === 1 ? "" : "s");
    el.valid.title = "";
    el.valid.style.color = "";
    el.valid.classList.remove("err");
  }

  function chipErr(message) {
    el.valid.textContent = "error";
    el.valid.title = message; // server's message, verbatim
    el.valid.style.color = "var(--err)";
  }

  /* ---------- theme: system → light → dark → system ---------- */

  var THEME_CYCLE = ["system", "light", "dark"];

  function applyTheme(mode) {
    var r = document.documentElement;
    if (mode === "light" || mode === "dark") r.setAttribute("data-theme", mode);
    else r.removeAttribute("data-theme");
    el.themeBtn.title = "Theme: " + mode;
    try { localStorage.setItem("cf-theme", mode); } catch (_) { /* private mode */ }
  }

  el.themeBtn.addEventListener("click", function () {
    var cur = document.documentElement.getAttribute("data-theme") || "system";
    var next = THEME_CYCLE[(THEME_CYCLE.indexOf(cur) + 1) % THEME_CYCLE.length];
    applyTheme(next);
  });

  var savedTheme = null;
  try { savedTheme = localStorage.getItem("cf-theme"); } catch (_) { /* ignore */ }
  if (savedTheme === "light" || savedTheme === "dark") applyTheme(savedTheme);
  else el.themeBtn.title = "Theme: system";

  /* ---------- generate cycle ---------- */

  function generateNow() {
    if (genTimer) { clearTimeout(genTimer); genTimer = null; }
    store.generate(false);
  }

  function scheduleGenerate() {
    if (genTimer) clearTimeout(genTimer);
    genTimer = setTimeout(function () { genTimer = null; store.generate(false); }, DEBOUNCE_MS);
  }

  el.generateBtn.addEventListener("click", generateNow);
  el.generateBtn.disabled = false;   // wired: markup ships them disabled so an
  el.validateBtn.disabled = false;   // early click can't hit a dead button
  el.validateBtn.addEventListener("click", function () {
    el.validateBtn.disabled = true;
    el.valid.textContent = "rendering\u2026";
    el.valid.style.color = "";
    api.renderCheck().then(function (r) {
      if (r.ok) {
        showWarn("");
        el.valid.textContent = "render ok \u00b7 " + r.resources + " resource" + (r.resources === 1 ? "" : "s");
        el.valid.title = "";
        el.valid.style.color = "";
      } else if (r.unavailable) {
        el.valid.textContent = "render check unavailable";
        el.valid.title = r.unavailable;
        el.valid.style.color = "var(--warn)";
      } else {
        el.valid.textContent = "render error";
        el.valid.title = r.error;
        el.valid.style.color = "var(--err)";
        showWarn(r.error);   // verbatim engine failure where the user can read it
      }
    }).catch(function (err) {
      el.valid.textContent = "render error";
      el.valid.style.color = "var(--err)";
      showWarn(err && err.message || String(err));
    }).finally(function () { el.validateBtn.disabled = false; });
  });

  store.subscribe("doc", function (doc) {
    drawTopbar(doc);
    drawWarn(doc);
    if (tab === "bp") render();
    scheduleGenerate();
  });

  store.subscribe("doc", function () { rbacCache = null; pkgCache = null; });
  store.subscribe("generate", function (result) {
    chipOk(result && result.outputs ? result.outputs.length : 0);
    buildTabs(); // providerconfig families can appear/vanish with sources
    if (tab !== "bp") render();
  });

  store.subscribe("error", function (err) {
    if (err && (err.source === "generate" || err.source === "loadDoc")) {
      chipErr(err.message);
    }
  });

  /* ---------- selection → scroll the output to that resource ---------- */

  function anchorLine(text, name) {
    var lines = text.split("\n");
    var marks = ['setResourceNameAnnotation "' + name + '"', "- name: " + name];
    for (var m = 0; m < marks.length; m++) {
      for (var i = 0; i < lines.length; i++) {
        if (lines[i].indexOf(marks[m]) !== -1) return i;
      }
    }
    return -1;
  }

  store.subscribe("selection", function (name) {
    if (!name || (tab !== "comp" && tab !== "bp")) return;
    var idx = anchorLine(currentText(), name);
    if (idx < 0) return;
    var lh = parseFloat(getComputedStyle(el.code).lineHeight) || 16;
    el.code.scrollTo({ top: Math.max(0, idx * lh - 40), behavior: "smooth" });
  });

  /* ---------- blueprint tab editor (yaml back through the import gate) ---------- */

  var editBtn = document.createElement("button");
  editBtn.id = "code-edit";
  editBtn.className = "btn";
  editBtn.textContent = "edit";
  editBtn.title = "Edit the blueprint YAML in place — applied through the same parse+validate gate as an import (one undo step)";
  editBtn.hidden = true;
  el.meta.parentNode.insertBefore(editBtn, el.meta);

  var editor = document.createElement("textarea");
  editor.id = "code-editor";
  editor.spellcheck = false;
  editor.hidden = true;
  editor.style.cssText = "flex:1;min-height:0;width:100%;box-sizing:border-box;resize:none;border:0;outline:none;" +
    "background:transparent;color:inherit;font:inherit;padding:10px 12px;";
  el.code.parentNode.insertBefore(editor, el.code.nextSibling);

  var editBar = document.createElement("div");
  editBar.id = "code-editbar";
  editBar.hidden = true;
  editBar.style.cssText = "display:flex;gap:8px;padding:6px 12px;border-top:1px solid var(--rule)";
  editBar.innerHTML = '<button class="btn" id="code-apply">Apply</button>' +
    '<button class="btn" id="code-cancel">Cancel</button>' +
    '<span class="dg" style="align-self:center">applied through the same gate as an import — invalid YAML never lands</span>';
  editor.parentNode.insertBefore(editBar, editor.nextSibling);

  function hideEditor() {
    editor.hidden = true;
    editBar.hidden = true;
    el.code.hidden = false;
  }

  editBtn.addEventListener("click", function () {
    editor.value = currentText();
    // the pre owns the drawer's free space via CSS; the textarea inherits its box
    editor.style.height = Math.max(80, el.code.clientHeight - 36) + "px";
    el.code.hidden = true;
    editor.hidden = false;
    editBar.hidden = false;
    editor.focus();
  });
  editBar.addEventListener("click", function (e) {
    if (e.target.id === "code-cancel") { hideEditor(); return; }
    if (e.target.id !== "code-apply") return;
    store.importBlueprint(editor.value).then(function (doc) {
      if (doc) hideEditor(); // a rejected edit stays open so it can be fixed
    });
  });
  el.tabs.addEventListener("click", function () {
    editBtn.hidden = tab !== "bp";
    hideEditor(); // switching tabs abandons an in-progress edit
  });

  /* ---------- splitter (canvas ↕ output) ---------- */

  initSplitter(rootEl);

  /* ---------- first paint ---------- */

  buildTabs();
  if (store.state.doc) {
    drawTopbar(store.state.doc);
    drawWarn(store.state.doc);
    scheduleGenerate();
  }
  render();
}

/* ======================================================================
 * Splitter: an absolutely-positioned grab strip straddling the drawer's
 * top border. Dragging resizes the drawer (the .cols row is 1fr, so the
 * canvas absorbs the difference). Double-click collapses to the header.
 * ==================================================================== */

function initSplitter(rootEl) {
  var style = document.createElement("style");
  style.id = "of-split-style";
  style.textContent =
    "#region-output{position:relative}" +
    ".of-split{position:absolute;top:-4px;left:0;right:0;height:9px;cursor:row-resize;" +
    "z-index:20;touch-action:none;background:transparent}" +
    ".of-split::after{content:\"\";position:absolute;left:0;right:0;top:3px;height:2px;" +
    "background:transparent;transition:background .12s}" +
    ".of-split:hover::after,.of-split.drag::after{background:var(--wire-xrd)}" +
    "#region-output[data-collapsed] .warnbar,#region-output[data-collapsed] .code{display:none!important}";
  document.head.appendChild(style);

  var split = document.createElement("div");
  split.className = "of-split";
  split.setAttribute("role", "separator");
  split.setAttribute("aria-orientation", "horizontal");
  split.setAttribute("aria-label", "Resize output drawer");
  split.title = "Drag to resize · double-click to collapse";
  rootEl.insertBefore(split, rootEl.firstChild);

  var lastExpanded = rootEl.offsetHeight || 212;
  var drag = null;
  var expandedByDown = false; // pointerdown on a collapsed drawer expands it;
                              // the dblclick that follows must not re-collapse

  function maxHeight() {
    var app = rootEl.closest(".app") || document.body;
    var bar = document.getElementById("region-topbar");
    var barH = bar ? bar.offsetHeight : 46;
    return Math.max(MIN_DRAWER, app.clientHeight - barH - MIN_CANVAS);
  }

  function collapsed() { return rootEl.hasAttribute("data-collapsed"); }

  function setHeight(h) {
    h = Math.min(Math.max(h, MIN_DRAWER), maxHeight());
    rootEl.style.height = h + "px";
    return h;
  }

  function collapse() {
    lastExpanded = rootEl.offsetHeight || lastExpanded;
    rootEl.setAttribute("data-collapsed", "");
    rootEl.style.height = "auto"; // header (+ splitter) only
  }

  function expand(h) {
    rootEl.removeAttribute("data-collapsed");
    setHeight(h || lastExpanded);
  }

  split.addEventListener("pointerdown", function (e) {
    if (e.button !== 0) return;
    if (collapsed()) { expand(Math.max(rootEl.offsetHeight, MIN_DRAWER)); expandedByDown = true; }
    else expandedByDown = false;
    drag = { y: e.clientY, h: rootEl.offsetHeight, moved: false };
    split.classList.add("drag");
    split.setPointerCapture(e.pointerId);
    e.preventDefault();
  });

  split.addEventListener("pointermove", function (e) {
    if (!drag) return;
    var dy = drag.y - e.clientY; // up = bigger drawer
    if (Math.abs(dy) > 2) drag.moved = true;
    setHeight(drag.h + dy);
  });

  function endDrag(e) {
    if (!drag) return;
    split.classList.remove("drag");
    try { split.releasePointerCapture(e.pointerId); } catch (_) { /* already released */ }
    if (!collapsed()) lastExpanded = rootEl.offsetHeight;
    drag = null;
  }
  split.addEventListener("pointerup", endDrag);
  split.addEventListener("pointercancel", endDrag);

  split.addEventListener("dblclick", function () {
    if (expandedByDown) { expandedByDown = false; return; }
    if (collapsed()) expand();
    else collapse();
  });
}

/* ======================================================================
 * Tiny JSON-to-YAML formatter — an indent walk, no library.
 * Faithful enough for the blueprint doc: objects, arrays, scalars,
 * multi-line strings as block literals, insertion order preserved.
 * ==================================================================== */

var PLAIN_KEY = /^[A-Za-z0-9_][A-Za-z0-9_.\-]*$/;
var NEEDS_QUOTE = /^$|^[\s>|&*!%@`"'#{}[\],]|[:#]\s|:$|\s$|^\s|^(true|false|null|yes|no|on|off|~)$|^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/i;

function yamlKey(k) {
  return PLAIN_KEY.test(k) ? k : JSON.stringify(k);
}

function yamlScalar(v) {
  if (v === null || v === undefined) return "null";
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  var s = String(v);
  if (NEEDS_QUOTE.test(s) || /[\n\t]/.test(s)) return JSON.stringify(s);
  return s;
}

function yamlLines(v, indent, out) {
  var pad = new Array(indent + 1).join(" ");
  if (Array.isArray(v)) {
    v.forEach(function (item) {
      if (item !== null && typeof item === "object") {
        var sub = [];
        yamlLines(item, indent + 2, sub);
        if (!sub.length) { out.push(pad + "- {}"); return; }
        out.push(pad + "- " + sub[0].slice(indent + 2));
        for (var i = 1; i < sub.length; i++) out.push(sub[i]);
      } else {
        out.push(pad + "- " + yamlScalar(item));
      }
    });
    return;
  }
  Object.keys(v).forEach(function (k) {
    var val = v[k];
    var key = pad + yamlKey(k) + ":";
    if (val !== null && typeof val === "object") {
      var empty = Array.isArray(val) ? val.length === 0 : Object.keys(val).length === 0;
      if (empty) { out.push(key + " " + (Array.isArray(val) ? "[]" : "{}")); return; }
      out.push(key);
      yamlLines(val, indent + 2, out);
    } else if (typeof val === "string" && val.indexOf("\n") >= 0) {
      out.push(key + " |" + (/\n$/.test(val) ? "" : "-"));
      val.replace(/\n$/, "").split("\n").forEach(function (l) {
        out.push(pad + "  " + l);
      });
    } else {
      out.push(key + " " + yamlScalar(val));
    }
  });
}

/** @param {Object} doc @returns {string} YAML text */
export function toYaml(doc) {
  var out = [];
  yamlLines(doc, 0, out);
  return out.join("\n") + "\n";
}
