/** Manifest-style editor for the selected resource's fields (CF-471). */
import { esc } from "../../dom.js";
import { highlight } from "../../utils.js";
import { state } from "./state.js";
import { buildSnippets } from "./preview.js";

export function manifestHtml(res, yamlText, loadErr, params, otherResources, otherStatusMap) {
  var draft = state.manifestDraft && state.manifestDraft.res === res.name ? state.manifestDraft : null;
  var h = '<div class="insp-sec manifest-sec" style="margin-top:12px;border-top:1px solid var(--rule)">' +
    '<div style="display:flex;align-items:center;gap:8px;padding:8px 12px 4px">' +
    '<span style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px">Manifest</span>' +
    '<span class="dg" style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis">' +
    (res.provider === "k8s" ? "the composed object" : "spec.forProvider of " + esc(res.kind)) + '</span>' +
    (draft ? '' : '<button class="btn sm" data-manifest-edit title="Edit as YAML — applied through the same schema gate as PUT; invalid YAML never lands">edit</button>') +
    '</div>';
  if (loadErr) return h + '<div class="warnbar">' + esc(loadErr) + '</div></div>';
  if (!draft) {
    return h + '<pre class="manifest-view" style="margin:0;padding:6px 12px 10px;font-family:var(--mono);font-size:11px;line-height:1.45;white-space:pre;overflow-x:auto">' +
      highlight(yamlText || "{}") + '</pre></div>';
  }
  var groups = buildSnippets(res, "", params, otherResources, otherStatusMap, state.store.state.doc);
  h += '<textarea class="manifest-editor" data-manifest-editor spellcheck="false" style="display:block;width:100%;box-sizing:border-box;min-height:220px;resize:vertical;font-family:var(--mono);font-size:11px;line-height:1.45;padding:6px 12px;border:0;border-top:1px solid var(--rule);background:var(--sunk);color:inherit;outline:none">' + esc(draft.text) + '</textarea>' +
    '<div class="manifest-bar" style="display:flex;gap:6px;align-items:center;padding:6px 12px;border-top:1px solid var(--rule)">' +
    '<button class="btn sm pri" data-manifest-apply>Apply</button>' +
    '<button class="btn sm" data-manifest-cancel>Cancel</button>' +
    '<select class="tsel" data-manifest-snippet aria-label="Insert snippet" style="flex:1;min-width:0"><option value="">+ insert wire…</option>';
  groups.forEach(function (g) {
    h += '<optgroup label="' + esc(g.label) + '">';
    g.items.forEach(function (it) {
      var from = /^\{\{\s*\$spec\.([\w.]+)\s*\}\}$/.exec(it.snippet);
      var val = from ? "{from: params." + from[1] + "}" : (it.label.indexOf("resources.") === 0 ? "{from: " + it.label + "}" : "{raw: '" + it.snippet + "'}");
      h += '<option value="' + esc(val) + '">' + esc(it.label) + '</option>';
    });
    h += '</optgroup>';
  });
  h += '</select><span class="dg">⌘⏎ apply · esc cancel</span></div>' +
    '<div class="manifest-err warnbar"' + (draft.err ? '' : ' hidden') + '>' + esc(draft.err || "") + '</div></div>';
  return h;
}

export function openManifestEditor(res, yamlText) {
  state.manifestDraft = { res: res.name, text: yamlText || "", err: null };
  state.render();
}

export function closeManifestEditor() {
  state.manifestDraft = null;
  state.render();
}

export function applyManifest(box) {
  var draft = state.manifestDraft;
  var ta = box.querySelector("textarea[data-manifest-editor]");
  if (!draft || !ta) return;
  draft.text = ta.value;
  var detail = null;
  var un = state.store.subscribe("error", function (e) { detail = e; });
  state.store.setResourceManifest(draft.res, draft.text).then(function (doc) {
    un();
    if (doc) { state.manifestDraft = null; state.render(); return; }
    var msg = (detail && detail.message) || "apply failed";
    draft.err = msg;
    state.render();
    var line = detail && detail.detail && detail.detail.line;
    var ta2 = box.querySelector("textarea[data-manifest-editor]");
    if (ta2 && line) selectLine(ta2, line);
  });
}

function selectLine(ta, line) {
  var lines = ta.value.split("\n");
  var start = 0;
  for (var i = 0; i < line - 1 && i < lines.length; i++) start += lines[i].length + 1;
  var end = start + ((lines[line - 1] || "").length);
  ta.focus();
  ta.setSelectionRange(start, end);
}

/** Tab inserts two spaces; ⌘/Ctrl+Enter applies; Escape cancels. */
export function onManifestKeydown(e, box) {
  var t = e.target;
  if (!t || !t.matches || !t.matches("textarea[data-manifest-editor]")) return false;
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
