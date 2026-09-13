/**
 * Submodule: inspector/manifest.js
 * The Manifest view (CF-471): the selected resource's fields as nested,
 * manifest-shaped YAML from GET /api/blueprint/resources/{name}/manifest, an
 * in-place YAML editor applied through PUT (the same schema gate as the
 * field routes, so invalid YAML never lands), the persisted Manifest/Fields
 * view choice, and the schema search hits listed under the manifest.
 * Module-local state of inspector.js (render, store, box, the draft) reaches
 * this file through the `state` delegates inspector.js wires.
 */

import { esc } from "../../dom.js";
import { highlight } from "../../utils.js";
import { state } from "./state.js";
import { buildSnippets, insertSnippetIntoTextarea } from "./preview.js";

var VIEW_KEY = "cf-insp-view";

/** The persisted view: Manifest unless the user last chose Fields. Storage
 *  can be unavailable (private mode, blocked), so the default is a fallback. */
export function readView() {
  try { return localStorage.getItem(VIEW_KEY) === "fields" ? "fields" : "manifest"; }
  catch (_) { return "manifest"; }
}

export function writeView(v) {
  try { localStorage.setItem(VIEW_KEY, v); } catch (_) { /* best effort */ }
}

function draftFor(res) {
  var d = state.manifestDraft;
  return d && d.res === res.name ? d : null;
}

/** A render replaces the textarea, so what the user typed is carried into the
 *  draft first: a doc change elsewhere (an essentials edit, a wire dropped on
 *  the card) never overwrites an open editor. */
function syncDraft(draft) {
  var live = state.box && state.box.querySelector("textarea[data-manifest-editor]");
  if (live) draft.text = live.value;
}

/** The snippet catalogue as manifest wrappers: a parameter becomes
 *  {from: params.x}, a sibling's status output {from: resources.n.status.p},
 *  anything else (context, env, templates) a raw template string. */
function snippetOptionsHtml(res, params, otherResources, otherStatusMap) {
  var groups = buildSnippets(res, "", params, otherResources, otherStatusMap, state.store.state.doc);
  return groups.map(function (g) {
    var isStatus = g.label === "Sibling Resource Status";
    return '<optgroup label="' + esc(g.label) + '">' + g.items.map(function (it) {
      var param = /^\{\{\s*\$spec\.([\w.]+)\s*\}\}$/.exec(it.snippet);
      var val = param ? "{from: params." + param[1] + "}"
        : isStatus ? "{from: resources." + it.label + "}"
        : "{raw: '" + it.snippet + "'}";
      return '<option value="' + esc(val) + '">' + esc(it.label) + '</option>';
    }).join("") + '</optgroup>';
  }).join("");
}

export function manifestHtml(res, yamlText, loadErr, params, otherResources, otherStatusMap) {
  var draft = draftFor(res);
  if (draft) syncDraft(draft);
  var h = '<div class="insp-sec manifest-sec">' +
    '<div class="manifest-h"><span class="manifest-t">Manifest</span>' +
    '<span class="dg manifest-hint">' +
    (res.provider === "k8s" ? "the composed object" : "spec.forProvider of " + esc(res.kind)) + '</span>' +
    (draft || loadErr ? '' : '<button class="btn sm" data-manifest-edit title="Edit as YAML — applied through the same schema gate as PUT; invalid YAML never lands">edit</button>') +
    '</div>';
  // Nothing to edit when the load failed: an editor opened on "" would PUT an
  // empty mapping and clear every field.
  if (loadErr) return h + '<div class="warnbar">' + esc(loadErr) + '</div></div>';
  if (!draft) return h + '<pre class="manifest-view">' + highlight(yamlText || "{}") + '</pre></div>';
  return h +
    '<textarea class="manifest-editor" data-manifest-editor spellcheck="false" aria-label="Manifest YAML">' + esc(draft.text) + '</textarea>' +
    '<div class="manifest-bar">' +
    '<button class="btn sm pri" data-manifest-apply title="Apply the YAML (⌘⏎)">Apply</button>' +
    '<button class="btn sm" data-manifest-cancel title="Discard the edit (esc)">Cancel</button>' +
    '<select class="tsel" data-manifest-snippet aria-label="Insert snippet" title="Insert a wire or template at the cursor">' +
    '<option value="">+ insert wire…</option>' + snippetOptionsHtml(res, params, otherResources, otherStatusMap) + '</select>' +
    '<span class="dg">⌘⏎ apply · esc cancel</span></div>' +
    '<div class="manifest-err warnbar"' + (draft.err ? '' : ' hidden') + '>' + esc(draft.err || "") + '</div></div>';
}

export function openManifestEditor(res, yamlText) {
  state.manifestDraft = { res: res.name, text: yamlText || "", err: null, errLine: 0 };
  state.render();
}

export function closeManifestEditor() {
  state.manifestDraft = null;
  state.render();
}

/** PUT the draft. A rejection keeps the editor open with the server's
 *  message; the line it names is selected by afterManifestRender once the
 *  (asynchronous) re-render has put the new textarea in place. A draft that
 *  was never loaded is refused (it would replace the fields with nothing),
 *  and an unchanged one just closes: no PUT, no undo step. */
export function applyManifest(box) {
  var draft = state.manifestDraft;
  var ta = box && box.querySelector("textarea[data-manifest-editor]");
  if (!draft || !ta) return null;
  draft.text = ta.value;
  if (!state.manifestLoaded) {
    draft.err = "manifest not loaded; reload the resource";
    draft.errLine = 0;
    state.render();
    return null;
  }
  if (draft.text === state.manifestYAML) {
    state.manifestDraft = null;
    state.render();
    return null;
  }
  var detail = null;
  var un = state.store.subscribe("error", function (e) {
    if (e && e.source === "setResourceManifest") detail = e;
  });
  return state.store.setResourceManifest(draft.res, draft.text).then(function (doc) {
    un();
    if (doc) { state.manifestDraft = null; state.render(); return doc; }
    draft.err = (detail && detail.message) || "apply failed";
    draft.errLine = (detail && detail.detail && detail.detail.line > 0) ? detail.detail.line : 0;
    state.render();
    return null;
  }).catch(function (e) {
    un();
    draft.err = (e && e.message) || "apply failed";
    draft.errLine = 0;
    state.render();
    return null;
  });
}

/** Called by inspector.js after the pane's HTML is replaced: select the line
 *  a rejected apply named, once. */
export function afterManifestRender(box) {
  var draft = state.manifestDraft;
  if (!draft || !draft.errLine) return;
  var ta = box.querySelector("textarea[data-manifest-editor]");
  if (!ta) return;
  var line = draft.errLine;
  draft.errLine = 0;
  var lines = ta.value.split("\n");
  var start = 0;
  for (var i = 0; i < line - 1 && i < lines.length; i++) start += lines[i].length + 1;
  ta.focus();
  ta.setSelectionRange(start, start + (lines[line - 1] || "").length);
}

/** The snippet dropdown: put the chosen wrapper at the cursor and reset the
 *  select. The document changes on Apply, not here. */
export function insertManifestSnippet(box, sel) {
  var value = sel.value;
  sel.value = "";
  if (!value) return;
  var ta = box.querySelector("textarea[data-manifest-editor]");
  if (!ta) return;
  insertSnippetIntoTextarea(ta, value);
  ta.focus();
}

/** Keys the manifest view owns; true when handled. In the editor Tab inserts
 *  two spaces, ⌘/Ctrl+Enter applies and Escape cancels; on a search hit Enter
 *  or Space acts like a click. */
export function onManifestKeydown(e, box) {
  var t = e.target;
  if (!t || !t.matches) return false;
  if (t.matches("[data-search-hit]") && (e.key === "Enter" || e.key === " ")) {
    e.preventDefault();
    t.click();
    return true;
  }
  if (!t.matches("textarea[data-manifest-editor]")) return false;
  if (e.key === "Tab") {
    e.preventDefault();
    var s = t.selectionStart, en = t.selectionEnd;
    t.value = t.value.slice(0, s) + "  " + t.value.slice(en);
    t.selectionStart = t.selectionEnd = s + 2;
    return true;
  }
  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); applyManifest(box); return true; }
  if (e.key === "Escape") { e.preventDefault(); closeManifestEditor(); return true; }
  return false;
}

/** Up to 30 schema leaves whose path or description contains the query,
 *  listed under the manifest; a click opens the Fields view on that field. */
export function searchHitsHtml(fields, q) {
  var ql = q.toLowerCase();
  var all = fields.filter(function (f) {
    return f.path.toLowerCase().indexOf(ql) !== -1 || (f.description || "").toLowerCase().indexOf(ql) !== -1;
  });
  var hits = all.slice(0, 30);
  if (!hits.length) return '<div class="empty">No schema field matches “' + esc(q) + '”.</div>';
  var count = hits.length < all.length ? "first " + hits.length + " of " + all.length : String(all.length);
  return '<div class="insp-sec search-hits"><div class="lbl">Schema matches (' + count + ')</div>' +
    hits.map(function (f) {
      return '<div class="search-hit" data-search-hit="' + esc(f.path) + '" role="button" tabindex="0" title="Open in the Fields view">' +
        '<div class="search-hit-h"><code>' + esc(f.path) + '</code><span class="t">' + esc(f.type) + '</span></div>' +
        (f.description ? '<div class="fld-d">' + esc(f.description.slice(0, 140)) + '</div>' : '') + '</div>';
    }).join("") + '</div>';
}
