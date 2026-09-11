/**
 * Submodule: inspector/preview.js
 * CEL & Go-template expression preview, snippet catalogue, live debouncing.
 */

import { esc } from "../../dom.js";
import { state } from "./state.js";
import { commitValue, commitEnvelopeValue } from "./events.js";

export function buildSnippets(res, fieldPath, params, otherResources, otherStatusMap, doc) {
  doc = doc || (state.store && state.store.state && state.store.state.doc);
  var groups = [];

  // 1. XRD Parameters
  var paramItems = [];
  Object.keys(params || {}).sort().forEach(function (pn) {
    var p = params[pn] || {};
    if (p.type === "object") {
      paramItems.push({ label: "$spec." + pn + " (toYaml)", snippet: "{{ toYaml $spec." + pn + " | nindent 6 }}" });
      if (p.properties) {
        Object.keys(p.properties).sort().forEach(function (mn) {
          paramItems.push({ label: "$spec." + pn + "." + mn, snippet: "{{ $spec." + pn + "." + mn + " }}" });
        });
      }
    } else {
      paramItems.push({ label: "$spec." + pn, snippet: "{{ $spec." + pn + " }}" });
      paramItems.push({ label: "default($spec." + pn + ")", snippet: '{{ default "' + (p.default || "value") + '" $spec.' + pn + " }}" });
    }
  });
  if (paramItems.length > 0) {
    groups.push({ label: "XRD Parameters", items: paramItems });
  }

  // 2. Composite ($xr) & Loop ($i) Context
  var ctxItems = [
    { label: "$xr (Composite Name)", snippet: "{{ $xr }}" },
    { label: "$xrMeta.namespace", snippet: "{{ $xrMeta.namespace }}" }
  ];
  if (res && res.forEach) {
    ctxItems.push({ label: "$i (Loop Index)", snippet: "{{ $i }}" });
    ctxItems.push({ label: 'printf "%s-%d" $xr $i', snippet: '{{ printf "%s-%d" $xr $i }}' });
  }
  groups.push({ label: "Context & Loops", items: ctxItems });

  // 3. Sibling Resource Status
  if (otherResources && otherResources.length > 0) {
    var statusItems = [];
    otherResources.forEach(function (or) {
      var sfs = (otherStatusMap && otherStatusMap[or.name]) || [
        { path: "atProvider.id", type: "string" },
        { path: "atProvider.arn", type: "string" },
        { path: "atProvider.url", type: "string" }
      ];
      sfs.forEach(function (sf) {
        statusItems.push({
          label: or.name + ".status." + sf.path,
          snippet: '{{ (index $.observed.resources "' + or.name + '").resource.status.' + sf.path + " }}"
        });
      });
    });
    if (statusItems.length > 0) {
      groups.push({ label: "Sibling Resource Status", items: statusItems });
    }
  }

  // 4. Environment
  var envItems = [
    { label: "$env.env", snippet: "{{ $env.env }}" },
    { label: "$env.region", snippet: "{{ $env.region }}" },
    { label: "$env.account", snippet: "{{ $env.account }}" }
  ];
  groups.push({ label: "Environment ($env)", items: envItems });

  // 5. Templates
  var tmpls = (doc && doc.spec && doc.spec.templates) || {};
  var tmplNames = Object.keys(tmpls).sort();
  if (tmplNames.length > 0) {
    var tmplItems = tmplNames.map(function (tn) {
      return { label: 'include "' + tn + '"', snippet: '{{ include "' + tn + '" . }}' };
    });
    groups.push({ label: "Templates", items: tmplItems });
  }

  return groups;
}

export function rawEditorHtml(path, val, isEnv, res, params, otherResources, otherStatusMap) {
  var doc = state.store && state.store.state && state.store.state.doc;
  var groups = buildSnippets(res, path, params, otherResources, otherStatusMap, doc);
  var rawAttr = isEnv ? 'data-env-raw="' : 'data-raw="';
  var snippetAttr = isEnv ? 'data-env-insert-snippet="' : 'data-insert-snippet="';
  var previewFor = isEnv ? ("env:" + path) : path;

  var h = '<div class="raw-editor-wrap">' +
    '<textarea class="val raw" ' + rawAttr + esc(path) + '" rows="2" placeholder="{{ }}">' + esc(val || "") + '</textarea>' +
    '<div class="snippets-bar">' +
    '<span class="snippets-lbl">Snippets:</span>' +
    '<select class="tsel snippet-select" ' + snippetAttr + esc(path) + '" aria-label="Insert snippet">' +
    '<option value="">+ Insert snippet…</option>';

  groups.forEach(function (g) {
    h += '<optgroup label="' + esc(g.label) + '">';
    g.items.forEach(function (item) {
      h += '<option value="' + esc(item.snippet) + '">' + esc(item.label) + '</option>';
    });
    h += '</optgroup>';
  });

  h += '</select>';

  var quickList = [];
  quickList.push({ label: "$xr", snippet: "{{ $xr }}" });
  if (res && res.forEach) {
    quickList.push({ label: "$i", snippet: "{{ $i }}" });
  }
  var pnames = Object.keys(params || {});
  if (pnames.length > 0) {
    quickList.push({ label: "$" + pnames[0], snippet: "{{ $spec." + pnames[0] + " }}" });
  }
  quickList.forEach(function (qc) {
    var chipAttr = isEnv ? 'data-env-quick-snippet="' : 'data-quick-snippet="';
    h += '<button type="button" class="snippet-chip" ' + chipAttr + esc(path) + '" data-snippet-val="' + esc(qc.snippet) + '" title="Insert ' + esc(qc.snippet) + '">' + esc(qc.label) + '</button>';
  });

  h += '</div>' +
    '<div class="expr-preview" data-preview-for="' + esc(previewFor) + '" style="display:none">' +
    '<div class="expr-preview-h">' +
    '<span class="expr-preview-title">Live Preview</span>' +
    '<span class="expr-preview-badge"></span>' +
    '</div>' +
    '<div class="expr-preview-body"></div>' +
    '</div>' +
    '</div>';

  return h;
}

var previewTimers = {};

export function triggerExpressionPreview(textarea, isEnv, path) {
  var box = state.box || (textarea && textarea.closest ? textarea.closest("#insp") : null) || document.querySelector("#insp");
  if (!box) return;

  var val = textarea.value.trim();
  var previewKey = isEnv ? ("env:" + path) : path;
  var previewEl = box.querySelector('.expr-preview[data-preview-for="' + CSS.escape(previewKey) + '"]');
  if (!previewEl) return;

  if (!val) {
    previewEl.style.display = "none";
    previewEl.className = "expr-preview";
    return;
  }

  if (previewTimers[previewKey]) {
    clearTimeout(previewTimers[previewKey]);
  }

  previewTimers[previewKey] = setTimeout(function () {
    var selRes = state.selectedResource ? state.selectedResource() : null;
    var resName = selRes ? selRes.name : "";
    state.api.previewExpression(val, resName).then(function (resp) {
      if (!previewEl) return;
      previewEl.style.display = "block";
      var badge = previewEl.querySelector(".expr-preview-badge");
      var body = previewEl.querySelector(".expr-preview-body");
      if (resp && resp.error) {
        previewEl.className = "expr-preview err";
        if (badge) badge.textContent = "error";
        if (body) body.textContent = resp.error;
      } else {
        previewEl.className = "expr-preview ok";
        if (badge) badge.textContent = "ok";
        if (body) body.textContent = (resp && resp.rendered) || "(empty output)";
      }
    }).catch(function (err) {
      if (!previewEl) return;
      previewEl.style.display = "block";
      previewEl.className = "expr-preview err";
      var badge = previewEl.querySelector(".expr-preview-badge");
      var body = previewEl.querySelector(".expr-preview-body");
      if (badge) badge.textContent = "error";
      if (body) body.textContent = err.message || String(err);
    });
  }, 100);
}

export function updateAllPreviews() {
  var box = state.box || document.querySelector("#insp");
  if (!box) return;
  var rawTextareas = box.querySelectorAll("textarea.raw");
  Array.prototype.forEach.call(rawTextareas, function (ta) {
    var isEnv = ta.hasAttribute("data-env-raw");
    var path = ta.getAttribute(isEnv ? "data-env-raw" : "data-raw");
    if (ta.value.trim()) {
      triggerExpressionPreview(ta, isEnv, path);
    }
  });
}

export function insertSnippetIntoTextarea(textarea, snippet) {
  var start = textarea.selectionStart;
  var end = textarea.selectionEnd;
  var text = textarea.value;
  if (start !== null && end !== null && start !== undefined) {
    textarea.value = text.substring(0, start) + snippet + text.substring(end);
    textarea.selectionStart = textarea.selectionEnd = start + snippet.length;
  } else {
    textarea.value = (text ? text + " " : "") + snippet;
  }
  var isEnv = textarea.hasAttribute("data-env-raw");
  var path = textarea.getAttribute(isEnv ? "data-env-raw" : "data-raw");
  triggerExpressionPreview(textarea, isEnv, path);
  if (isEnv) {
    commitEnvelopeValue(path, "raw", textarea.value);
  } else {
    commitValue(path, "raw", textarea.value);
  }
}
