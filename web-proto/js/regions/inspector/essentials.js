/**
 * Submodule: inspector/essentials.js
 * The per-kind essentials form: profile rows (app label, typed fields with
 * expose-as-parameter, the Service selector with quick-match, the env
 * repeater) and the schema-derived fallback for kinds without a profile.
 * Field-level helpers that the field list also uses (entryOf, docMode,
 * uiMode, modeButtons, wireSelectHtml, isFieldEffectivelyRequired,
 * boundChipHtml) come in through the state delegates inspector.js wires.
 */

import { esc } from "../../dom.js";
import { profileFor } from "../../profiles.js";
import { state } from "./state.js";
import { rawEditorHtml } from "./preview.js";

/** The app label held by a selector or labels entry: a bracket entry's
 *  literal, else what a legacy whole-map raw (JSON or `app: x`) spells. */
function extractApp(f) {
  if (!f) return "";
  if (f.value) return f.value;
  if (f.raw) {
    try {
      var parsed = JSON.parse(f.raw);
      if (parsed && typeof parsed === "object" && parsed.app) return parsed.app;
    } catch (_) {}
    var m = /app[:=]\s*["']?([a-zA-Z0-9_-]+)["']?/.exec(f.raw);
    if (m) return m[1];
    return f.raw;
  }
  return "";
}

function appLabelState(res) {
  var fields = res.fields || {};
  var sel = extractApp(fields["spec.selector.matchLabels[app]"] || fields["spec.selector.matchLabels"] || fields["spec.selector.matchLabels.app"]);
  var tmpl = extractApp(fields["spec.template.metadata.labels[app]"] || fields["spec.template.metadata.labels"] || fields["spec.template.metadata.labels.app"]);
  return { label: sel || tmpl || res.name, synced: !!(sel && tmpl && sel === tmpl) };
}

function appLabelBadge(res) {
  return appLabelState(res).synced
    ? '<span class="chip-ok" style="font-size:10px">Selectors Aligned</span>'
    : '<span style="color:var(--warn);font-size:10px;font-weight:600">Sync Required</span>';
}

function appLabelRowHtml(res) {
  return '<div class="ess-row" style="margin-bottom:6px">' +
    '<div class="fld-h"><span class="lbl" style="flex:0 0 auto">App label</span></div>' +
    essPathHtml("spec.selector.matchLabels[app]", "spec.selector.matchLabels[app] and spec.template.metadata.labels[app]") +
    '<div class="frow" style="margin-bottom:0">' +
    '<input class="tin" data-wl-app="' + esc(res.name) + '" value="' + esc(appLabelState(res).label) + '" placeholder="e.g. ' + esc(res.name) + '"' + ariaLabel("App label", "spec.selector.matchLabels[app]") + ' title="Sets both spec.selector.matchLabels and spec.template.metadata.labels">' +
    '<button class="btn sm pri" data-wl-sync-app="' + esc(res.name) + '" title="Sync App Label across Selector and Template">Sync</button>' +
    '</div></div>';
}

function serviceTargetOf(res) {
  var fields = res.fields || {};
  return extractApp(fields["spec.selector[app]"] || fields["spec.selector"] || fields["spec.selector.app"]);
}

function serviceTargetBadge(res) {
  var app = serviceTargetOf(res);
  return app
    ? '<span class="chip-ok" style="font-size:10px">Target: ' + esc(app) + '</span>'
    : '<span style="color:var(--warn);font-size:10px;font-weight:600">Unset Selector</span>';
}

function serviceSelectorRowHtml(res, doc) {
  var candidates = (doc && doc.spec && doc.spec.resources || []).filter(function (r) {
    return r.name !== res.name && (r.kind === "Deployment" || r.kind === "StatefulSet" || r.kind === "DaemonSet");
  });
  var h = '<div class="ess-row" style="margin-bottom:6px">' +
    '<div class="fld-h"><span class="lbl" style="flex:0 0 auto">Target app</span></div>' +
    essPathHtml("spec.selector[app]") +
    '<div class="frow" style="margin-bottom:0">' +
    '<input class="tin" data-svc-app="' + esc(res.name) + '" value="' + esc(serviceTargetOf(res)) + '" placeholder="app label"' + ariaLabel("Target app", "spec.selector[app]") + ' title="Routes traffic to pods whose app label matches"></div>';
  if (candidates.length) {
    h += '<div style="margin-top:4px;display:flex;gap:4px;flex-wrap:wrap;align-items:center">' +
      '<span class="dg" style="font-size:10px">Quick match:</span>' +
      candidates.map(function (cw) {
        var f = cw.fields || {};
        var app = extractApp(f["spec.selector.matchLabels[app]"] || f["spec.selector.matchLabels"] || f["spec.template.metadata.labels"] || f["spec.selector.matchLabels.app"]) || cw.name;
        return '<button class="btn sm" data-svc-match-wl="' + esc(res.name) + '" data-match-app="' + esc(app) + '" style="font-size:10px;padding:1px 6px">' + esc(cw.name) + ' (' + esc(app) + ')</button>';
      }).join("") + '</div>';
  }
  return h + '</div>';
}

function ariaLabel(label, path) {
  return ' aria-label="' + esc(label + " \u2014 " + path) + '"';
}

/** The path under a row's label on its own line: it wraps rather than
 *  truncating to "spec…" the way the field list's header does. */
function essPathHtml(path, title) {
  return '<div class="ess-path" style="font-family:var(--mono);font-size:9.5px;color:var(--faint);overflow-wrap:anywhere;margin:-2px 0 3px" title="' + esc(title || path) + '">' + esc(path) + '</div>';
}

function essRowHead(label, path, type, m, extra) {
  return '<div class="fld-h"><span class="lbl" style="flex:0 0 auto">' + esc(label) + '</span>' +
    (type ? '<span class="t">' + esc(type) + '</span>' : '') +
    state.modeButtons(path, m, false) + (extra || '') + '</div>' + essPathHtml(path);
}

/** The typed control for one path in its current mode: the wired chip or
 *  wire select, the raw editor, or a literal input shaped by the row's type. */
function essValueControl(res, path, type, row, params, otherResources, otherStatusMap, env) {
  var entry = state.entryOf(res, path);
  var dm = state.docMode(entry);
  var m = state.uiMode[path] || dm;
  if (m === "w") {
    if (dm === "w" && !state.uiMode[path] && entry) return state.boundChipHtml(res, path, entry);
    return state.wireSelectHtml(path, type, params, otherResources, otherStatusMap, false, false, entry && entry.from, env);
  }
  if (m === "r") return rawEditorHtml(path, (dm === "r" && entry) ? entry.raw : "", false, res, params, otherResources, otherStatusMap);
  var val = (dm === "v" && entry) ? entry.value : "";
  var aria = ariaLabel(row.label || path.split(".").pop(), path);
  if (row.enum) {
    var opts = val && row.enum.indexOf(val) === -1 ? [val].concat(row.enum) : row.enum;
    return '<select class="val tsel" data-v="' + esc(path) + '"' + aria + '>' +
      '<option value=""' + (val === "" ? " selected" : "") + '>unset &#8212; omitted from output</option>' +
      opts.map(function (o) {
        return '<option value="' + esc(o) + '"' + (val === o ? ' selected' : '') + '>' + esc(o) + '</option>';
      }).join('') + '</select>';
  }
  if (type === "boolean") {
    var bVal = val.toLowerCase();
    return '<select class="val tsel" data-v="' + esc(path) + '"' + aria + '>' +
      '<option value=""' + (bVal === "" ? " selected" : "") + '>unset &#8212; omitted from output</option>' +
      '<option value="true"' + (bVal === "true" ? " selected" : "") + '>true</option>' +
      '<option value="false"' + (bVal === "false" ? " selected" : "") + '>false</option></select>';
  }
  var inputType = (type === "integer" || type === "number") ? 'type="number" ' : '';
  return '<input class="val" ' + inputType + 'data-v="' + esc(path) + '" value="' + esc(val) + '" placeholder="' + esc(row.placeholder || "") + '"' + aria + '>';
}

/** Expose-as-parameter: only for rows that name a parameter and hold a
 *  literal (or nothing); a wire or raw template has no literal to carry. */
function exposeButton(res, path, row) {
  if (!row.param) return '';
  var entry = state.entryOf(res, path);
  if (entry && (entry.from || entry.raw)) return '';
  return '<button class="btn sm" data-expose="' + esc(path) + '" data-expose-name="' + esc(row.param) + '" data-expose-type="' + esc(row.type || "string") + '" title="Expose as XRD parameter (default = current value) and wire this field to it">&#8599; param</button>';
}

/** One row per <prefix>[i]: NAME plus a value control that takes a literal or
 *  a wire. Add appends index n; delete removes index i and renumbers. */
function envRepeaterHtml(res, prefix, params, otherResources, otherStatusMap, env) {
  var idx = {};
  Object.keys(res.fields || {}).forEach(function (k) {
    var mm = k.indexOf(prefix + "[") === 0 && /^\[(\d+)\]\.(name|value)$/.exec(k.slice(prefix.length));
    if (mm) idx[mm[1]] = true;
  });
  var rows = Object.keys(idx).map(Number).sort(function (a, b) { return a - b; });
  var h = '<div class="ess-env" data-env-repeater="' + esc(prefix) + '">' +
    '<div class="fld-h"><span class="lbl">Environment variables</span><span class="sp"></span>' +
    '<button class="btn sm" data-env-row-add="' + esc(prefix) + '">+ add variable</button></div>';
  rows.forEach(function (i) {
    var np = prefix + "[" + i + "].name", vp = prefix + "[" + i + "].value";
    var nEntry = state.entryOf(res, np);
    h += '<div class="frow ess-env-row" data-env-row="' + i + '" style="align-items:flex-start">' +
      '<input class="val" data-v="' + esc(np) + '" value="' + esc(nEntry ? nEntry.value : "") + '" placeholder="NAME" style="flex:0 0 38%"' + ariaLabel("Environment variable name", np) + '>' +
      '<div style="flex:1;min-width:0">' + essValueControl(res, vp, "string", { label: "Environment variable value" }, params, otherResources, otherStatusMap, env) + '</div>' +
      state.modeButtons(vp, state.uiMode[vp] || state.docMode(state.entryOf(res, vp)), false) +
      '<button class="del" data-env-row-del="' + esc(prefix) + '|' + i + '" title="Remove variable">&#215;</button></div>';
  });
  if (!rows.length) h += '<div class="g" style="padding:2px 0 4px">none &#8212; values can be wired from another resource\'s status</div>';
  return h + '</div>';
}

/** Rows for a kind without a profile (every provider resource): what the
 *  Required view would flag, `region`, and whatever is already set. */
function schemaEssentials(res, flds) {
  var seen = {}, rows = [];
  ((flds && flds.fields) || []).forEach(function (f) {
    if (state.isFieldEffectivelyRequired(f, res) || f.path === "region" || state.entryOf(res, f.path)) {
      seen[f.path] = true;
      rows.push({ kind: "field", path: f.path, label: f.path.split(".").pop(), type: f.type, enum: f.enum || null });
    }
  });
  Object.keys(res.fields || {}).forEach(function (p) {
    if (!seen[p] && state.entryOf(res, p)) rows.push({ kind: "field", path: p, label: p.split(".").pop(), type: "string" });
  });
  return rows;
}

export function essentialsHtml(res, flds, doc, params, otherResources, otherStatusMap, env) {
  var profile = profileFor(res.kind, res.provider);
  var rows = profile && profile.essentials.length ? profile.essentials : schemaEssentials(res, flds);
  var isWl = res.provider === "k8s" && (res.kind === "Deployment" || res.kind === "StatefulSet" || res.kind === "DaemonSet");
  var isSvc = res.provider === "k8s" && res.kind === "Service";
  var cls = "insp-sec essentials" + (isWl ? " workload-card" : "") + (isSvc ? " service-card" : "");
  var h = '<div class="' + cls + '" style="margin:10px 0;padding:10px 12px;border:1px solid var(--wire-xrd);background:var(--surface-2);border-radius:6px">' +
    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:6px">' +
    '<span style="font-size:11px;font-weight:600;color:var(--wire-xrd);text-transform:uppercase;letter-spacing:0.5px">Essentials</span>' +
    (isWl ? appLabelBadge(res) : (isSvc ? serviceTargetBadge(res) : '')) + '</div>';
  if (!rows.length) return h + '<div class="g" style="padding:2px 0 0">nothing required &#8212; set fields below</div></div>';
  rows.forEach(function (row) {
    if (row.kind === "app-label") { h += appLabelRowHtml(res); return; }
    if (row.kind === "service-selector") { h += serviceSelectorRowHtml(res, doc); return; }
    if (row.kind === "env") { h += envRepeaterHtml(res, row.prefix, params, otherResources, otherStatusMap, env); return; }
    var m = state.uiMode[row.path] || state.docMode(state.entryOf(res, row.path));
    h += '<div class="ess-row" data-ess-row="' + esc(row.path) + '" style="margin-bottom:6px">' +
      essRowHead(row.label, row.path, row.type, m, exposeButton(res, row.path, row)) +
      essValueControl(res, row.path, row.type, row, params, otherResources, otherStatusMap, env) + '</div>';
  });
  return h + '</div>';
}
