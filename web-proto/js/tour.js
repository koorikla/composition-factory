/**
 * Interactive tour: a guided walkthrough of the canvas — providers, kinds,
 * variables, wires, maps, functions, generate — highlighting the REAL UI.
 *
 * Self-contained on purpose: this module injects its own topbar button and
 * its own styles, and drives the app only through public DOM affordances
 * (clicking the same tabs and buttons a user would), so it lives beside the
 * other regions without touching their code. Loaded from index.html with
 * one script tag.
 */

/* Each step: target (CSS selector highlighted; null = centered card),
 * title, body (plain text), and an optional prep() that puts the UI in the
 * state where the target exists — by clicking what a user would click. */
const STEPS = [
  {
    target: "#canvas",
    title: "The canvas",
    body: "Every card is one composed object; the XR card on the left is the API your users will call. Wires ARE the document — everything you see round-trips through the blueprint YAML.",
  },
  {
    target: "#rtabs button[data-r=\"src\"]",
    title: "Providers",
    body: "SOURCES lists your schema sources. Search the built-in catalogue of 476 OSS providers and add one with a click — its CRD schemas are fetched from the package's OCI layers and cached.",
    prep: function () { click("#rtabs button[data-r=\"src\"]"); },
  },
  {
    target: "#addCrdsBtn",
    title: "Any CRD-backed object",
    body: "Not just providers: upload any CRD manifest — an Argo Workflow, another composition's XR — and its kinds become droppable objects. Cluster discovery (opt-in) scans a live cluster the same way.",
  },
  {
    target: "#lrail",
    title: "Kinds",
    body: "KINDS lists every schema-backed kind, with required-field counts. Hover a row for a preview; drag it onto the canvas to compose it. Fields are validated against the CRD's real OpenAPI schema — typos fail loudly at author time.",
    prep: function () { click("#rtabs button[data-r=\"kinds\"]"); },
  },
  {
    target: "#rtabs button[data-r=\"shared\"]",
    title: "Common variables",
    body: "SHARED holds the XRD parameters — the knobs your users set. Add strings, integers, booleans, enums… and typed objects with nested members (a full schema editor lives in the inspector).",
    prep: function () { click("#rtabs button[data-r=\"shared\"]"); },
  },
  {
    target: ".port[data-owner=\"xrd\"] .d",
    title: "Wire a variable",
    body: "Drag from a parameter dot onto any card: a picker lists that kind's type-compatible fields (searchable, custom annotations included). The wire lands in the document as from: params.<name>.",
  },
  {
    target: "#insp",
    title: "Objects, maps and annotations",
    body: "Select a card and the inspector shows its fields — Required / Set / All. Map fields take per-key entries (tags[team]); annotations take wires too, e.g. an IAM role ARN into a ServiceAccount annotation. V/W/R toggles switch a field between literal value, wire and raw template.",
  },
  {
    target: "#insp",
    title: "Functions & pipeline",
    body: "With nothing selected, the inspector edits the XRD itself: parameters, and the Composition pipeline — add function steps (auto-ready, environment-configs…) before or after the templating step.",
    prep: function () {
      // clear selection so the XRD editor shows
      var c = document.getElementById("canvas");
      if (c) c.click();
    },
  },
  {
    target: "#engineSel",
    title: "Emission engines",
    body: "The same blueprint can emit through go-templating (default), KCL or Python — switch the engine here and watch composition.yaml re-render. Template source can also move from inline to files + ConfigMaps for big compositions.",
  },
  {
    target: "#generateBtn",
    title: "Generate & validate",
    body: "Generate renders composition.yaml, definition.yaml, functions.yaml and providerconfigs deterministically — byte-identical to cf gen. Validate runs a real `crossplane composition render` and reports the composed resource count.",
  },
  {
    target: "#tabs",
    title: "The output drawer",
    body: "Every generated file has a tab; the blueprint tab is editable in place (same validation gate as an import), and package.yaml shows the whole Configuration package — downloadable as an .xpkg with the Package button.",
  },
  {
    target: "#examplesBtn",
    title: "Starters, import & export",
    body: "Examples loads a curated starter (IRSA, RDS, full-stack app). Import accepts a blueprint or an exported package.yaml — the blueprint travels inside the package, so a composition is always recoverable. That's the loop. Build something!",
  },
];

function click(sel) {
  var el = document.querySelector(sel);
  if (el) el.click();
}

(function init() {
  var topbar = document.getElementById("region-topbar");
  if (!topbar) return;

  var btn = document.createElement("button");
  btn.className = "btn";
  btn.id = "tourBtn";
  btn.textContent = "Tour";
  btn.title = "Interactive walkthrough: providers, kinds, variables, wires, maps, functions, generate";
  var anchor = document.getElementById("examplesBtn");
  topbar.insertBefore(btn, anchor || null);

  var overlay = document.createElement("div");
  overlay.id = "tour-overlay";
  overlay.hidden = true;
  overlay.innerHTML = '<div class="tour-hl"></div><div class="tour-card" role="dialog" aria-label="Interactive tour">' +
    '<h3></h3><div class="tour-b"></div><div class="tour-row">' +
    '<span class="tour-n"></span><span style="flex:1"></span>' +
    '<button class="btn sm" id="tour-skip">Skip</button>' +
    '<button class="btn sm" id="tour-back">Back</button>' +
    '<button class="btn sm pri" id="tour-next">Next</button></div></div>';
  document.body.appendChild(overlay);

  var hl = overlay.querySelector(".tour-hl");
  var card = overlay.querySelector(".tour-card");
  var step = 0;

  function place() {
    var s = STEPS[step];
    card.querySelector("h3").textContent = s.title;
    card.querySelector(".tour-b").textContent = s.body;
    card.querySelector(".tour-n").textContent = (step + 1) + " / " + STEPS.length;
    card.querySelector("#tour-back").disabled = step === 0;
    card.querySelector("#tour-next").textContent = step === STEPS.length - 1 ? "Done" : "Next";

    var el = s.target && document.querySelector(s.target);
    if (el) {
      var r = el.getBoundingClientRect();
      hl.hidden = false;
      hl.style.left = (r.left - 4) + "px";
      hl.style.top = (r.top - 4) + "px";
      hl.style.width = (r.width + 8) + "px";
      hl.style.height = (r.height + 8) + "px";
      // card beside the highlight: right of it if there's room, else left,
      // else below — clamped to the viewport
      var cw = 336, ch = card.offsetHeight || 160;
      var x = r.right + 14;
      if (x + cw > innerWidth) x = r.left - cw - 14;
      if (x < 8) x = Math.min(Math.max(8, r.left), innerWidth - cw - 8);
      var y = Math.min(Math.max(8, r.top), innerHeight - ch - 16);
      if (x === Math.min(Math.max(8, r.left), innerWidth - cw - 8) && r.bottom + ch + 16 < innerHeight && r.right + 14 + cw > innerWidth && r.left - cw - 14 < 8) {
        y = r.bottom + 14;
      }
      card.style.left = x + "px";
      card.style.top = y + "px";
    } else {
      hl.hidden = true;
      hl.style.width = "0";
      hl.style.height = "0";
      hl.style.left = "-10px";
      hl.style.top = "-10px";
      card.style.left = Math.max(8, (innerWidth - 336) / 2) + "px";
      card.style.top = "120px";
    }
  }

  function show(i) {
    step = Math.max(0, Math.min(STEPS.length - 1, i));
    var s = STEPS[step];
    if (s.prep) { try { s.prep(); } catch (_) { /* the target check below copes */ } }
    // prep may re-render the rail; place after a frame so rects are fresh
    requestAnimationFrame(function () { requestAnimationFrame(place); });
    overlay.hidden = false;
  }

  function close() { overlay.hidden = true; }

  btn.addEventListener("click", function () { show(0); });
  overlay.addEventListener("click", function (e) {
    if (e.target.id === "tour-skip") { close(); return; }
    if (e.target.id === "tour-back") { show(step - 1); return; }
    if (e.target.id === "tour-next") {
      if (step === STEPS.length - 1) { close(); return; }
      show(step + 1);
    }
  });
  addEventListener("keydown", function (e) {
    if (overlay.hidden) return;
    if (e.key === "Escape") close();
    if (e.key === "ArrowRight") show(step + 1);
    if (e.key === "ArrowLeft") show(step - 1);
  });
  addEventListener("resize", function () { if (!overlay.hidden) place(); });
})();
