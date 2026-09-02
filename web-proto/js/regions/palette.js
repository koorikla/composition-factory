/**
 * Region: PALETTE (left rail). Root element: #region-palette (see index.html).
 *
 * Tabs (prototype-exact):
 *   KINDS   — live /api/kinds grouped by group; server-side search (?q=);
 *             per-kind required-count badge; rows draggable with a
 *             dataTransfer JSON payload {kind, apiVersion, provider}.
 *   SHARED  — XRD parameters from doc.spec.xrd.parameters, rendered with the
 *             prototype's Vars card look; badge = fan-out count (wires.js).
 *   SOURCES — doc.spec.sources refs as src-rows; add flow disabled for now.
 *
 * Exported init(rootEl, {store, api}) is the single entry point — main.js
 * calls it once with the region root and the shared store/api.
 */

import { store as defaultStore } from "../store.js";
import * as defaultApi from "../api.js";
import { esc } from "../dom.js";
import { fanOut } from "../wires.js";

/* Node color families, exactly as the prototype's COLORS map. */
const COLORS = { aws: "var(--wire-ref)", k8s: "var(--wire-status)", cluster: "#06b6d4" };

const HINT_KINDS =
  'Drag a kind onto the canvas. Schemas load per-kind — <span class="mono">4.5 KB</span> median.';
const HINT_SHARED =
  'Wires are explicit in the doc — <span class="mono">from: params.X</span> on a resource field. Badge = fan-out.';
const HINT_SRC =
  'Pinned by digest in <span class="mono">.cf.lock</span> or discovered live from your cluster.';

let booted = false;

/** Color family for a live kind row (heuristic: provider/group naming). */
function famOf(k) {
  if (k && k.provider === "cluster") return "cluster";
  const g = (k && k.group || "") + " " + (k && k.provider || "");
  return /(^|[^a-z])aws|upbound\.io/.test(g) ? "aws" : "k8s";
}

/**
 * init — wire the palette region into its root element.
 * @param {HTMLElement} rootEl #region-palette
 * @param {{store: Object, api: Object}} deps
 */
export function init(rootEl, deps) {
  if (booted) return;
  booted = true;

  const store = deps && deps.store || defaultStore;
  const api = deps && deps.api || defaultApi;

  const tabsEl = rootEl.querySelector("#rtabs");
  const searchWrapEl = rootEl.querySelector("#lsearch");
  const searchEl = rootEl.querySelector("#psearch");
  const railEl = rootEl.querySelector("#lrail");
  const hintEl = rootEl.querySelector("#lhint");

  let rail = "kinds";          // "kinds" | "shared" | "src"
  let kinds = [];              // last /api/kinds result rows
  // per-provider kind visibility, persisted: { "<providerRef>": ["Kind",...] }
  // lists the HIDDEN kinds; a provider absent from the map hides nothing.
  let hiddenKinds = {};
  try { hiddenKinds = JSON.parse(localStorage.getItem("cf-hidden-kinds")) || {}; } catch (_) { /* fresh */ }
  function saveHidden() {
    try { localStorage.setItem("cf-hidden-kinds", JSON.stringify(hiddenKinds)); } catch (_) { /* private mode */ }
  }
  // identity is kind|apiVersion — Namespaced and Cluster variants share the
  // kind name under one provider (the classic kind-alone collision).
  function isHidden(provider, kind, apiVersion) {
    const l = hiddenKinds[provider];
    return !!l && l.indexOf(kind + "|" + (apiVersion || "")) >= 0;
  }

  let kindsError = null;       // verbatim server message, or null
  let kindsLoaded = false;
  let searchSeq = 0;
  let debounceTimer = null;

  /* ---- kinds fetch (server-side search via api.getKinds(q)) ---- */
  function loadKinds() {
    const q = (searchEl && searchEl.value || "").trim();
    const seq = ++searchSeq;
    api.getKinds(q).then(function (d) {
      if (seq !== searchSeq) return; // stale response
      kinds = d && d.kinds || [];
      kindsError = null;
      kindsLoaded = true;
      if (rail === "kinds") drawRail();
    }, function (e) {
      if (seq !== searchSeq) return;
      kindsError = e.message;
      kindsLoaded = true;
      if (rail === "kinds") drawRail();
    });
  }

  /* ---- render ---------------------------------------------------------- */
  function drawKinds() {
    if (kindsError) return '<div class="empty">' + esc(kindsError) + "</div>";
    if (!kindsLoaded) return '<div class="empty">Loading kinds…</div>';
    if (!kinds.length) return '<div class="empty">No kinds match.</div>';
    // Group by `group`, first-appearance order (header rows like the prototype).
    const order = [];
    const byGroup = {};
    kinds.forEach(function (k) {
      if (isHidden(k.provider, k.kind, k.apiVersion)) return;
      const g = k.group || k.apiVersion || "";
      if (!byGroup[g]) { byGroup[g] = []; order.push(g); }
      byGroup[g].push(k);
    });
    let h = "";
    order.forEach(function (g) {
      const items = byGroup[g];
      h += '<div class="grp"><span class="lbl">' + esc(g) + '</span><span class="n">' + items.length + "</span></div>";
      items.forEach(function (k) {
        const fam = famOf(k);
        const clusterTag = k.provider === "cluster"
          ? '<span class="pill" style="font-size:9.5px;padding:1px 4px;background:rgba(6,182,212,0.12);color:#06b6d4;border-radius:3px;margin-right:4px">cluster</span>'
          : "";
        h += '<div class="kind" draggable="true"' +
          ' data-kind="' + esc(k.kind) + '"' +
          ' data-av="' + esc(k.apiVersion) + '"' +
          ' data-provider="' + esc(k.provider || "") + '"' +
          ' data-fam="' + esc(fam) + '">' +
          '<span class="sw" style="background:' + COLORS[fam] + '"></span>' +
          '<span class="nm" style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="' + esc(k.kind) + '">' + esc(k.kind) + '</span>' +
          clusterTag +
          '<span class="req">' + (k.required | 0) + " req</span></div>";
      });
    });
    return h;
  }

  function paramLines(p) {
    const lines = ["type " + (p.type || "string") + (p.required ? ", required" : "")];
    if (p.enum && p.enum.length) lines.push("enum " + p.enum.join(" | "));
    if (p.default !== undefined && p.default !== "") lines.push("default " + p.default);
    if (p.description) lines.push(p.description);
    return lines.map(esc).join("\n");
  }

  function memberSummary(props, depth) {
    if (!props) return "";
    return Object.keys(props).sort().map(function (mn) {
      const mp = props[mn];
      let row = '<div class="dg" style="padding-left:' + (depth * 8) + 'px">.' + esc(mn) + " \u00b7 " + esc(mp.type) +
        (mp.required ? " req" : "") + (mp.default ? " = " + esc(mp.default) : "") + "</div>";
      if (mp.type === "object") row += memberSummary(mp.properties, depth + 1);
      return row;
    }).join("");
  }

  function drawShared() {
    const doc = store.state.doc;
    if (!doc) return '<div class="empty">No document loaded.</div>';
    const params = doc.spec && doc.spec.xrd && doc.spec.xrd.parameters || {};
    const names = Object.keys(params);
    let h = '<div class="grp"><span class="lbl">Parameters</span><span class="n">' + names.length + "</span></div>";
    if (!names.length) h += '<div class="empty">No parameters declared.</div>';
    names.forEach(function (n) {
      h += '<div class="card"><div class="card-h">' +
        '<span class="nm" style="color:var(--shared)">$' + esc(n) + "</span>" +
        '<span class="sp"></span><span class="bind">' + fanOut(doc, n) + " bound</span>" +
        '<button class="del" data-param-del="' + esc(n) + '" title="Delete parameter">\u00d7</button></div>' +
        '<div class="card-b">' + paramLines(params[n]) +
        memberSummary(params[n].properties, 1) + "</div></div>";
    });
    if (!paramFormOpen) {
      h += '<div style="padding:8px 10px"><button class="btn sm" id="param-add-btn">+ Add parameter</button></div>';
    } else {
      h += '<div class="card" id="param-add-form" style="padding:8px 10px;display:flex;flex-direction:column;gap:6px">' +
        '<input id="param-add-name" class="search" placeholder="parameterName" aria-label="Parameter name">' +
        '<div style="display:flex;gap:6px;align-items:center">' +
        '<select id="param-add-type" class="search" style="flex:1" aria-label="Type">' +
        ["string","integer","number","boolean","object"].map(function (t) {
          return '<option value="' + t + '"' + (t === paramType ? " selected" : "") + ">" + t + "</option>";
        }).join("") + "</select>" +
        '<label style="display:flex;gap:4px;align-items:center;font-size:10.5px;color:var(--faint)">' +
        '<input type="checkbox" id="param-add-req">required</label></div>' +
        '<input id="param-add-default" class="search" placeholder="default (optional)"' +
        (paramType === "object" ? " hidden" : "") + ' aria-label="Default value">' +
        '<input id="param-add-enum" class="search" placeholder="enum values, comma-separated"' +
        (paramType === "string" ? "" : " hidden") + ' aria-label="Enum values">' +
        (paramType === "object"
          ? '<div class="dg">no members \u2192 a free-form string map (like tags); declare members for a typed object</div>' +
            paramMembers.map(function (m, mi) {
              return '<div style="display:flex;gap:4px;align-items:center">' +
                '<input class="search" data-member-name data-mi="' + mi + '" placeholder="memberName" value="' + esc(m.name) + '" style="flex:1;min-width:0">' +
                '<select class="search" data-member-type data-mi="' + mi + '" style="flex:0 0 auto">' +
                ["string","integer","number","boolean","object"].map(function (t) {
                  return '<option' + (m.type === t ? " selected" : "") + ">" + t + "</option>";
                }).join("") + "</select>" +
                '<input class="search" data-member-default data-mi="' + mi + '" placeholder="default" value="' + esc(m.default || "") + '" style="flex:0 0 70px">' +
                '<button class="del" data-member-del="' + mi + '" title="Remove member">\u00d7</button></div>';
            }).join("") +
            '<button class="btn sm" id="param-add-member">+ member</button>'
          : "") +
        '<div style="display:flex;gap:6px">' +
        '<button class="btn sm pri" id="param-add-submit">Add</button>' +
        '<button class="btn sm" id="param-add-cancel">Cancel</button></div></div>';
    }
    if (paramErr) h += '<div class="warnbar" role="alert" style="margin:0 10px">' + esc(paramErr) + "</div>";
    return h;
  }

  let paramFormOpen = false;
  let paramErr = null;
  let paramType = "string";    // add-form type; controls which inputs render
  let paramMembers = [];       // typed-object member rows in the add form

  let providers = null;        // server-side cached providers, null = not loaded
  let providersErr = null;     // verbatim server error from the last add/list
  let catRows = null;          // catalogue search results, null = untouched
  let catTimer = null;
  let fnRows = null;           // functions catalogue search results, null = untouched
  let fnTimer = null;
  let srcSubTab = "prov";      // "prov" | "fn" | "cls"
  let expandedProvider = null; // ref whose detail row is open
  let providerKinds = null;    // kinds of the expanded provider, null = loading
  let clusterInfo = null;      // live cluster connection status
  let clusterLoading = false;
  let clusterErr = null;

  function loadCluster() {
    api.getCluster().then(function (info) {
      clusterInfo = info;
      if (rail === "src") drawRail();
    }).catch(function (e) {
      clusterInfo = { connected: false, error: e && e.message || String(e) };
      if (rail === "src") drawRail();
    });
  }

  function loadProviders() {
    api.getProviders().then(function (r) {
      providers = r.providers || [];
      if (rail === "src") drawRail();
    }).catch(function () {
      providers = null;        // endpoint absent or down: fall back to doc sources
      if (rail === "src") drawRail();
    });
  }

  function loadFunctions(q) {
    api.getCatalogue(q || "", "function").then(function (r) {
      fnRows = r.providers || [];
      if (rail === "src") drawRail();
    }).catch(function () {
      fnRows = [];
      if (rail === "src") drawRail();
    });
  }

  function drawSources() {
    const doc = store.state.doc;
    if (!doc) return '<div class="empty">No document loaded.</div>';

    // Subtabs switcher
    let h = '<div style="padding:6px 10px 4px">' +
      '<div class="rtabs" id="src-subtabs">' +
      '<button data-src-sub="prov" aria-pressed="' + (srcSubTab === "prov") + '">Providers</button>' +
      '<button data-src-sub="fn" aria-pressed="' + (srcSubTab === "fn") + '">Functions</button>' +
      '<button data-src-sub="cls" aria-pressed="' + (srcSubTab === "cls") + '">Cluster</button>' +
      '</div></div>';

    if (srcSubTab === "fn") {
      h += '<div class="grp"><span class="lbl">Crossplane Functions</span></div>' +
        '<div style="padding:0 10px 6px"><input id="fn-search" class="search" placeholder="Search functions (kcl, ready, extra\u2026)" aria-label="Search functions"></div>';
      if (fnRows === null) {
        loadFunctions("");
        h += '<div class="empty">Loading functions catalogue\u2026</div>';
      } else if (!fnRows.length) {
        h += '<div class="empty">No function matches found.</div>';
      } else {
        h += '<div style="padding:0 10px 4px;font-size:10.5px;color:var(--faint)">Add steps to <code>spec.pipeline</code>:</div>';
        fnRows.forEach(function (c) {
          const isAdded = (doc.spec && doc.spec.pipeline || []).some(function (p) {
            return p.functionRef === c.name || (p.package && p.package.indexOf(c.name) !== -1);
          });
          h += '<div class="cat-row src-row" style="cursor:default;align-items:flex-start;padding:6px 10px;gap:6px" title="' + esc(c.description || c.name) + '">' +
            '<span class="sw" style="width:5px;height:24px;border-radius:1.5px;background:var(--wire-xrd);flex:0 0 auto;margin-top:2px"></span>' +
            '<span style="min-width:0;flex:1"><span class="nm" style="display:block;font-weight:600">' + esc(c.name) + "</span>" +
            '<span class="dg" style="display:block;font-size:10px;line-height:1.3;margin-top:1px;color:var(--muted)">' + esc(c.description || "") + '</span>' +
            '<span class="dg" style="display:block;font-size:9.5px;margin-top:2px;color:var(--faint);word-break:break-all">' + esc(c.ref || "") + '</span></span>' +
            (isAdded
              ? '<span class="pill" style="font-size:9.5px;background:var(--wire-status-soft);color:var(--wire-status);align-self:center;flex:0 0 auto">Active</span>'
              : '<button class="btn sm fn-add-pipe" data-add-fn-pipe="' + esc(c.name) + '|' + esc(c.ref || "") + '" style="align-self:center;flex:0 0 auto" title="Add step to pipeline">+ Pipe</button>') +
            "</div>";
        });
      }
      return h;
    }

    if (srcSubTab === "cls") {
      h += '<div class="grp"><span class="lbl">Live Cluster</span></div>';
      if (clusterLoading) {
        h += '<div class="empty">Syncing CRDs with cluster\u2026</div>';
      } else if (clusterInfo && clusterInfo.connected) {
        h += '<div class="src-row" style="cursor:default;padding:8px 10px">' +
          '<span class="sw" style="width:5px;height:22px;border-radius:1.5px;background:#06b6d4"></span>' +
          '<span style="min-width:0;flex:1"><span class="nm" style="display:block">' + esc(clusterInfo.context || "connected") + '</span>' +
          '<span class="dg">' + esc(clusterInfo.server || "") + ' \u00b7 ' + (clusterInfo.crdCount || 0) + ' CRDs</span></span>' +
          '<button class="btn sm" id="cluster-sync-btn" title="Sync CRDs from cluster">Sync</button></div>';
      } else {
        h += '<div style="padding:8px 10px;display:flex;flex-direction:column;gap:6px">' +
          '<div class="dg" style="color:var(--faint);font-size:10.5px">' + (clusterInfo && clusterInfo.error ? esc(clusterInfo.error) : "Not connected to a live Kubernetes cluster.") + '</div>' +
          '<button class="btn sm pri" id="cluster-sync-btn" style="align-self:flex-start">Connect &amp; Sync CRDs</button>' +
          '</div>';
      }
      if (clusterErr) h += '<div class="warnbar" role="alert" style="margin:0 10px">' + esc(clusterErr) + "</div>";
      return h;
    }

    // Providers tab
    let sources = providers !== null
      ? providers.map(function (p) { return { provider: p.ref, digest: p.digest, kinds: p.kinds }; })
      : (doc.spec && doc.spec.sources || []).slice();
    const nativeCount = kinds.filter(function (k) { return k.provider === "k8s"; }).length;
    if (nativeCount) sources = sources.concat([{ provider: "k8s", digest: "", kinds: nativeCount, native: true }]);
    h += '<div class="grp"><span class="lbl">Installed Providers</span><span class="n">' + sources.length + "</span></div>";
    h += '<div style="padding:2px 10px 8px"><button class="btn" id="addCrdsBtn" ' +
      'title="Add any CRD-backed kind (an Argo Workflow, another composition\u2019s XR\u2026) from a CRD manifest file">+ Add CRDs from file</button>' +
      '<input type="file" id="addCrdsFile" accept=".yaml,.yml" hidden></div>';
    if (!sources.length) h += '<div class="empty">No sources declared.</div>';
    sources.forEach(function (s) {
      const ref = s && s.provider || "";
      const fam = /aws/.test(ref) ? "aws" : "k8s";
      const meta = (s.digest ? s.digest.slice(0, 19) : "") + (s.kinds ? " \u00b7 " + s.kinds + " kinds" : "");
      h += '<div class="src-row" data-ref="' + esc(ref) + '" style="cursor:pointer" title="Click for details" aria-expanded="' + (expandedProvider === ref) + '">' +
        '<span class="sw" style="width:5px;height:22px;border-radius:1.5px;background:' + COLORS[fam] + '"></span>' +
        '<span style="min-width:0"><span class="nm" style="display:block">' + esc(ref.split("/").pop()) + "</span>" +
        '<span class="dg">' + esc(meta || ref) + '</span></span><span class="sp"></span></div>';
      if (expandedProvider === ref) {
        const hiddenList = hiddenKinds[ref] || [];
        const kindsHtml = providerKinds === null
          ? '<div class="g">loading kinds\u2026</div>'
          : '<label style="display:flex;gap:6px;align-items:center;font-size:10.5px;margin:2px 0">' +
            '<input type="checkbox" data-pick-all data-ref="' + esc(ref) + '"' +
            (hiddenList.length === 0 ? " checked" : "") + ">show all kinds</label>" +
            providerKinds.map(function (k) {
              return '<label style="display:flex;gap:6px;align-items:center;font-size:10.5px">' +
                '<input type="checkbox" data-pick-kind="' + esc(k.kind) + '" data-av="' + esc(k.apiVersion) + '" data-ref="' + esc(ref) + '"' +
                (isHidden(ref, k.kind, k.apiVersion) ? "" : " checked") + ">" +
                '<span style="font-family:var(--mono);font-size:11px">' + esc(k.kind) + "</span>" +
                '<span class="dg" style="margin-left:auto">' + esc(k.scope) + "</span></label>";
            }).join("");
        h += '<div class="src-detail" style="padding:4px 12px 10px 22px;display:flex;flex-direction:column;gap:3px">' +
          '<span class="dg" style="word-break:break-all" title="Full registry reference">' + esc(ref) + "</span>" +
          (s.digest ? '<span class="dg" style="word-break:break-all">' + esc(s.digest) + "</span>" : "") +
          kindsHtml +
          (s.native ? "" :
            '<button class="btn sm" id="src-remove-btn" style="align-self:flex-start;margin-top:4px" ' +
            'title="Remove this provider from the cache">Remove provider</button>') + "</div>";
      }
    });
    h += '<div style="padding:8px 10px;display:flex;gap:6px">' +
      '<input id="src-add-ref" class="search" style="flex:1;min-width:0" placeholder="ghcr.io/\u2026/provider-x:vN" aria-label="Provider ref">' +
      '<button class="btn sm" id="src-add-btn">Add</button></div>';
    // add/remove failures surface here, verbatim (the refactor that added
    // the functions rail dropped this render and the add handler with it \u2014
    // both are load-bearing: without them the Add button is silently dead)
    if (providersErr) h += '<div class="warnbar" role="alert" style="margin:0 10px">' + esc(providersErr) + "</div>";
    h += '<div class="grp"><span class="lbl">Providers Catalogue</span></div>' +
      '<div style="padding:0 10px 6px"><input id="cat-search" class="search" placeholder="Search OSS providers\u2026" aria-label="Search catalogue"></div>';
    if (catRows === null) {
      h += '<div class="empty">Type to search the catalogue.</div>';
    } else if (!catRows.length) {
      h += '<div class="empty">No catalogue matches.</div>';
    } else {
      catRows.slice(0, 20).forEach(function (c) {
        h += '<div class="cat-row src-row" style="cursor:default" title="' + esc(c.description || c.name) + '">' +
          '<span style="min-width:0;flex:1"><span class="nm" style="display:block">' + esc(c.name) + "</span>" +
          '<span class="dg">' + esc(c.ref || "no published image \u2014 publishes elsewhere") + "</span></span>" +
          (c.ref ? '<button class="btn sm cat-add" data-cat-ref="' + esc(c.ref) + '">Add</button>' : "") +
          "</div>";
      });
    }
    // Live Kubernetes Cluster section
    h += '<div class="grp"><span class="lbl">Live Cluster</span></div>';
    if (clusterLoading) {
      h += '<div class="empty">Syncing CRDs with cluster\u2026</div>';
    } else if (clusterInfo && clusterInfo.connected) {
      h += '<div class="src-row" style="cursor:default;padding:8px 10px">' +
        '<span class="sw" style="width:5px;height:22px;border-radius:1.5px;background:#06b6d4"></span>' +
        '<span style="min-width:0;flex:1"><span class="nm" style="display:block">' + esc(clusterInfo.context || "connected") + '</span>' +
        '<span class="dg">' + esc(clusterInfo.server || "") + ' \u00b7 ' + (clusterInfo.crdCount || 0) + ' CRDs</span></span>' +
        '<button class="btn sm" id="cluster-sync-btn" title="Sync CRDs from cluster">Sync</button></div>';
    } else {
      h += '<div style="padding:8px 10px;display:flex;flex-direction:column;gap:6px">' +
        '<div class="dg" style="color:var(--faint);font-size:10.5px">' + (clusterInfo && clusterInfo.error ? esc(clusterInfo.error) : "Not connected to a live Kubernetes cluster.") + '</div>' +
        '<button class="btn sm pri" id="cluster-sync-btn" style="align-self:flex-start">Connect &amp; Sync CRDs</button>' +
        '</div>';
    }
    if (clusterErr) h += '<div class="warnbar" role="alert" style="margin:0 10px">' + esc(clusterErr) + "</div>";
    return h;
  }

  function drawGuide() {
    function sec(t, body) {
      return '<div class="grp"><span class="lbl">' + t + '</span></div>' +
        '<div style="padding:4px 12px 10px;font-size:11px;line-height:1.55;color:var(--ink-2)">' + body + "</div>";
    }
    function kbd(k) { return '<b style="font-family:var(--mono);font-size:10px">' + k + "</b>"; }
    const mod = /Mac/.test(navigator.platform) ? "\u2318" : "Ctrl";
    return sec("Starter Blueprints",
        "Explore canonical compositions (click to load):" +
        '<div style="margin-top:6px;display:flex;flex-direction:column;gap:5px">' +
          '<button class="btn sm" data-guide-example="irsa" style="justify-content:flex-start">⚡ AWS IRSA (Role + ServiceAccount)</button>' +
          '<button class="btn sm" data-guide-example="rds-postgres" style="justify-content:flex-start">🗄️ AWS RDS PostgreSQL</button>' +
          '<button class="btn sm" data-guide-example="k8s-app" style="justify-content:flex-start">📦 Full-Stack Microservice (App + SQS + IRSA + RDS)</button>' +
          '<button class="btn sm" data-guide-example="k8s-workload" style="justify-content:flex-start">🌐 Cloud-Agnostic Web Workload</button>' +
          '<button class="btn sm" data-guide-example="k8s-cronjob" style="justify-content:flex-start">⏱️ Cloud-Agnostic Scheduled CronJob</button>' +
          '<button class="btn sm" data-guide-example="s3-bucket" style="justify-content:flex-start">🪣 AWS S3 Secure Storage Bucket</button>' +
          '<button class="btn sm" data-guide-example="sqs-queue" style="justify-content:flex-start">📬 AWS SQS Queue with DLQ</button>' +
        '</div>') +
      sec("The loop",
        "Drag a kind from KINDS onto the canvas, wire XR parameters to resource fields, " +
        "edit values in the inspector \u2014 the generated YAML below updates live and is " +
        "written by <b>cf gen</b> byte-for-byte the same.") +
      sec("Wires",
        '<span style="color:var(--wire-xrd)">\u2500\u2500</span> XRD spec \u00b7 ' +
        '<span style="color:var(--shared)">\u2500\u2500</span> shared (one parameter feeding ' +
        "several fields) \u00b7 status and native-ref wires arrive with the engine work.") +
      sec("Keyboard",
        kbd(mod + "C") + " / " + kbd(mod + "V") + " copy &amp; paste to duplicate a resource \u00b7 " +
        kbd("Delete") + " remove (confirms when wires would drop) \u00b7 " +
        "wheel zooms to the cursor, " + kbd("Shift+wheel") + " pans (or drag the empty ground), " + kbd("\u2302") + " resets.") +
      sec("Validate",
        "Runs a real <b>crossplane composition render</b> against a sample XR synthesized " +
        "from your XRD \u2014 the chip reports the composed resource count or the engine's " +
        "error verbatim.") +
      sec("Files",
        "Generate writes compositions/, xrds/ and functions.yaml to the output directory " +
        "cf serve was started with; the blueprint file is the single source of truth.");
  }

  function drawRail() {
    if (searchWrapEl) searchWrapEl.style.display = rail === "kinds" ? "" : "none";
    let h, hint;
    if (rail === "kinds") { h = drawKinds(); hint = HINT_KINDS; }
    else if (rail === "shared") { h = drawShared(); hint = HINT_SHARED; }
    else if (rail === "guide") { h = drawGuide(); hint = ""; }
    else { h = drawSources(); hint = HINT_SRC; }
    // A re-render (e.g. the providers list arriving) must not eat what the
    // user is typing into the add-provider field.
    var keepIds = ["src-add-ref", "cat-search", "fn-search", "param-add-name", "param-add-type", "param-add-req"];
    var kept = {};
    keepIds.forEach(function (id) {
      var el = railEl.querySelector("#" + id);
      if (el) kept[id] = {
        v: el.type === "checkbox" ? el.checked : el.value,
        focus: document.activeElement === el,
      };
    });
    railEl.innerHTML = h;
    keepIds.forEach(function (id) {
      var st = kept[id], el = railEl.querySelector("#" + id);
      if (!st || !el) return;
      if (el.type === "checkbox") el.checked = st.v; else el.value = st.v;
      if (st.focus) el.focus();
    });
    if (hintEl) hintEl.innerHTML = hint;
  }

  /* ---- events ---------------------------------------------------------- */
  if (tabsEl) tabsEl.addEventListener("click", function (e) {
    const b = e.target.closest("button");
    if (!b) return;
    rail = b.getAttribute("data-r");
    if (rail === "src") {
      if (providers === null) loadProviders();
      loadCluster();
    }
    [].forEach.call(tabsEl.children, function (c) {
      c.setAttribute("aria-pressed", String(c === b));
    });
    drawRail();
  });

  if (searchEl) searchEl.addEventListener("input", function () {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(loadKinds, 150);
  });

  function syncMemberRows() {
    railEl.querySelectorAll("[data-member-name]").forEach(function (el) {
      const mi = Number(el.getAttribute("data-mi"));
      if (paramMembers[mi]) paramMembers[mi].name = el.value;
    });
    railEl.querySelectorAll("[data-member-type]").forEach(function (el) {
      const mi = Number(el.getAttribute("data-mi"));
      if (paramMembers[mi]) paramMembers[mi].type = el.value;
    });
    railEl.querySelectorAll("[data-member-default]").forEach(function (el) {
      const mi = Number(el.getAttribute("data-mi"));
      if (paramMembers[mi]) paramMembers[mi].default = el.value;
    });
  }

  railEl.addEventListener("change", function (e) {
    if (e.target.id === "addCrdsFile") {
      var f = e.target.files && e.target.files[0];
      e.target.value = "";
      if (!f) return;
      var reader = new FileReader();
      reader.onload = function () {
        var name = f.name.replace(/\.(yaml|yml)$/, "").toLowerCase().replace(/[^a-z0-9-]/g, "-");
        api.addCRDSource(name, String(reader.result)).then(function () {
          providers = null;                    // provider rows may change
          return store.loadDoc();              // pull the doc the server just extended
        }).then(function () {
          loadKinds();
          if (rail === "src") { loadProviders(); drawRail(); }
        }).catch(function (err) {
          alert("add CRDs failed: " + (err && err.message || err));
        });
      };
      reader.readAsText(f);
      return;
    }
    if (e.target.id === "param-add-type") { syncMemberRows(); paramType = e.target.value; drawRail(); return; }
    if (e.target.closest("[data-member-name],[data-member-type],[data-member-default]")) { syncMemberRows(); return; }
    const pickAll = e.target.closest("input[data-pick-all]");
    if (pickAll) {
      const ref = pickAll.getAttribute("data-ref");
      if (pickAll.checked) delete hiddenKinds[ref];
      else hiddenKinds[ref] = (providerKinds || [])
        .map(function (k) { return k.kind + "|" + k.apiVersion; });
      saveHidden(); drawRail(); return;
    }
    const pick = e.target.closest("input[data-pick-kind]");
    if (pick) {
      const ref = pick.getAttribute("data-ref");
      const key = pick.getAttribute("data-pick-kind") + "|" + (pick.getAttribute("data-av") || "");
      const l = (hiddenKinds[ref] || []).filter(function (k) { return k !== key; });
      if (!pick.checked) l.push(key);
      if (l.length) hiddenKinds[ref] = l; else delete hiddenKinds[ref];
      saveHidden(); drawRail(); return;
    }
  });

  railEl.addEventListener("click", function (e) {
    if (e.target.id === "addCrdsBtn") {
      var fi = document.getElementById("addCrdsFile");
      if (fi) fi.click();
      return;
    }
    if (e.target.closest("#cluster-sync-btn")) {
      clusterLoading = true;
      clusterErr = null;
      drawRail();
      api.syncCluster().then(function (info) {
        clusterLoading = false;
        clusterInfo = info;
        loadKinds();
        loadProviders();
      }).catch(function (err) {
        clusterLoading = false;
        clusterErr = err && err.message || String(err);
        drawRail();
      });
      return;
    }
    if (e.target.closest("#src-remove-btn") && expandedProvider) {
      const ref = expandedProvider;
      if (!window.confirm("Remove " + ref + " from the cache?")) return;
      providersErr = null;
      api.removeProvider(ref).then(function () {
        expandedProvider = null; providerKinds = null;
        loadProviders(); loadKinds();
      }).catch(function (err) {
        providersErr = err && err.message || String(err);
        drawRail();
      });
      return;
    }
    const guideExBtn = e.target.closest("button[data-guide-example]");
    if (guideExBtn) {
      const exId = guideExBtn.getAttribute("data-guide-example");
      guideExBtn.disabled = true;
      var loadP = (store.loadExample && typeof store.loadExample === "function")
        ? store.loadExample(exId)
        : api.getExample(exId).then(function (res) {
            if (res && res.example && res.example.yaml) return store.importBlueprint(res.example.yaml);
          });
      loadP.then(function (doc) {
        if (doc) store.select(null);
      }).catch(function (err) {
        if (hintEl) hintEl.innerHTML = '<span style="color:var(--err)">Failed to load example: ' + esc(err.message) + '</span>';
      }).finally(function () {
        guideExBtn.disabled = false;
      });
      return;
    }
    if (e.target.closest("#src-add-btn")) {
      const addInput = railEl.querySelector("#src-add-ref");
      const addRef = addInput && addInput.value.trim();
      if (!addRef) return;
      e.target.disabled = true;
      providersErr = null;
      api.addProvider(addRef).then(function () {
        loadProviders();
        loadKinds();             // new kinds must appear in the KINDS tab
      }).catch(function (err) {
        providersErr = err && err.message || String(err);
        drawRail();
      });
      return;
    }
    const catBtn = e.target.closest("button.cat-add");
    if (catBtn) {
      catBtn.disabled = true;
      providersErr = null;
      api.addProvider(catBtn.getAttribute("data-cat-ref")).then(function () {
        loadProviders(); loadKinds();
      }).catch(function (err) {
        providersErr = err && err.message || String(err);
        drawRail();
      });
      return;
    }
    if (e.target.closest(".src-detail")) return; // the change listener owns it
    const srcRow = e.target.closest(".src-row[data-ref]");
    if (srcRow) {
      const ref = srcRow.getAttribute("data-ref");
      if (expandedProvider === ref) { expandedProvider = null; providerKinds = null; drawRail(); return; }
      expandedProvider = ref;
      providerKinds = null;
      drawRail();
      api.getKinds().then(function (r) {
        if (expandedProvider !== ref) return;
        providerKinds = (r.kinds || []).filter(function (k) { return k.provider === ref; });
        drawRail();
      }).catch(function () { providerKinds = []; if (expandedProvider === ref) drawRail(); });
      return;
    }
    const pdel = e.target.closest("[data-param-del]");
    if (pdel) {
      const n = pdel.getAttribute("data-param-del");
      if (!window.confirm("Delete parameter $" + n + "?")) return;
      paramErr = null;
      store.deleteParameter(n);   // failure surfaces via the error topic below
      return;
    }
    if (e.target.closest("#param-add-member")) {
      syncMemberRows();
      paramMembers.push({ name: "", type: "string", default: "" });
      drawRail(); return;
    }
    const mdel = e.target.closest("[data-member-del]");
    if (mdel) {
      syncMemberRows();
      paramMembers.splice(Number(mdel.getAttribute("data-member-del")), 1);
      drawRail(); return;
    }
    if (e.target.closest("#param-add-btn")) { paramFormOpen = true; paramErr = null; paramType = "string"; paramMembers = []; drawRail(); return; }
    if (e.target.closest("#param-add-cancel")) { paramFormOpen = false; paramErr = null; drawRail(); return; }
    if (e.target.closest("#param-add-submit")) {
      const name = (railEl.querySelector("#param-add-name") || {}).value || "";
      const type = (railEl.querySelector("#param-add-type") || {}).value || "string";
      const req = !!(railEl.querySelector("#param-add-req") || {}).checked;
      if (!name.trim()) return;
      const param = { type: type, required: req };
      const dv = (railEl.querySelector("#param-add-default") || {}).value || "";
      const ev = (railEl.querySelector("#param-add-enum") || {}).value || "";
      if (dv.trim() && type !== "object") param.default = dv.trim();
      if (ev.trim() && type === "string") param.enum = ev.split(",").map(function (x) { return x.trim(); }).filter(Boolean);
      if (type === "object") {
        syncMemberRows();
        const props = {};
        paramMembers.forEach(function (m) {
          if (!m.name.trim()) return;
          const mp = { type: m.type };
          // objects take no default (the engine refuses it); nested members
          // are declared afterwards in the inspector's member tree
          if ((m.default || "").trim() && m.type !== "object") mp.default = m.default.trim();
          props[m.name.trim()] = mp;
        });
        if (Object.keys(props).length) param.properties = props;
      }
      paramErr = null;
      store.addParameter(name.trim(), param).then(function (res) {
        // the store resolves null on failure and emits the verbatim error;
        // the error subscription below paints it — keep the form open.
        if (res) { paramFormOpen = false; paramErr = null; drawRail(); }
      });
      return;
    }
    const srcSubBtn = e.target.closest("#src-subtabs button[data-src-sub]");
    if (srcSubBtn) {
      srcSubTab = srcSubBtn.getAttribute("data-src-sub");
      if (srcSubTab === "fn" && fnRows === null) loadFunctions("");
      if (srcSubTab === "cls") loadCluster();
      drawRail();
      return;
    }

    const fnBtn = e.target.closest("[data-add-fn-pipe]");
    if (fnBtn) {
      const val = fnBtn.getAttribute("data-add-fn-pipe") || "";
      const parts = val.split("|");
      const fnName = parts[0];
      let fnRef = parts[1];
      if (!fnRef || !/:|@sha256:/.test(fnRef)) {
        fnRef = "ghcr.io/crossplane-contrib/" + fnName + ":latest";
      }
      const stepName = fnName.replace(/^function-/, "");
      const pos = (fnName === "function-auto-ready" || fnName === "function-sequencer") ? "after" : "before";
      store.replaceDoc(function (d) {
        d.spec = d.spec || {};
        d.spec.pipeline = d.spec.pipeline || [];
        if (!d.spec.pipeline.some(function (p) { return p.functionRef === fnName; })) {
          d.spec.pipeline.push({
            name: stepName,
            functionRef: fnName,
            package: fnRef,
            position: pos
          });
        }
      }).then(function () {
        drawRail();
      });
      return;
    }
  });

  railEl.addEventListener("input", function (e) {
    if (e.target.id === "fn-search") {
      const q = e.target.value.trim();
      clearTimeout(fnTimer);
      fnTimer = setTimeout(function () {
        loadFunctions(q);
      }, 200);
      return;
    }
    if (e.target.id !== "cat-search") return;
    const q = e.target.value.trim();
    clearTimeout(catTimer);
    catTimer = setTimeout(function () {
      if (!q) { catRows = null; drawRail(); return; }
      api.getCatalogue(q).then(function (r) {
        catRows = r.providers || [];
        if (rail === "src") drawRail();
      }).catch(function () { catRows = []; if (rail === "src") drawRail(); });
    }, 200);
  });

  /* ---- kind hover preview (slice 28) ---- */
  let previewTimer = null;
  let previewFor = null;      // "kind|av" currently shown/loading
  const previewCache = {};    // "kind|av" -> {total, required:[{path,type,description}]}

  function hideKindPreview() {
    clearTimeout(previewTimer); previewTimer = null; previewFor = null;
    const el = document.getElementById("kind-preview");
    if (el) el.hidden = true;
  }

  function showKindPreview(row) {
    const kind = row.getAttribute("data-kind"), av = row.getAttribute("data-av");
    const key = kind + "|" + av;
    previewFor = key;
    const paint = function (info) {
      if (previewFor !== key) return;
      let el = document.getElementById("kind-preview");
      if (!el) {
        el = document.createElement("div");
        el.id = "kind-preview";
        el.style.cssText = "position:fixed;z-index:50;max-width:260px;background:var(--surface);" +
          "border:1px solid var(--rule-2);border-radius:7px;box-shadow:var(--shadow-lg);" +
          "padding:9px 11px;font-size:10.5px;pointer-events:none";
        document.body.appendChild(el);
      }
      const r = row.getBoundingClientRect();
      el.style.left = (r.right + 8) + "px";
      el.style.top = Math.min(r.top, innerHeight - 180) + "px";
      const scope = /\.m\./.test(av) || row.getAttribute("data-provider") === "k8s" ? "Namespaced" : "Cluster";
      let h = '<div style="font-family:var(--mono);font-size:12px;font-weight:600">' + esc(kind) + "</div>" +
        '<div class="dg" style="margin:1px 0 6px">' + esc(av) + " \u00b7 " + scope + "</div>";
      if (info) {
        h += '<div class="dg" style="margin-bottom:4px">' + info.total + " fields \u00b7 " +
          info.required.length + " required</div>";
        info.required.slice(0, 5).forEach(function (f) {
          h += '<div style="margin-bottom:3px"><span style="font-family:var(--mono)">' + esc(f.path) +
            '</span> <span class="dg">' + esc(f.type) + "</span>" +
            (f.description ? '<div class="dg" style="font-size:9.5px;line-height:1.4">' +
              esc(f.description.slice(0, 110)) + (f.description.length > 110 ? "\u2026" : "") + "</div>" : "") +
            "</div>";
        });
      } else {
        h += '<div class="dg">loading\u2026</div>';
      }
      el.hidden = false;
      el.innerHTML = h;
    };
    if (previewCache[key]) { paint(previewCache[key]); return; }
    paint(null);
    api.getKindFields(av, kind, { requiredOnly: true }).then(function (req) {
      return api.getKindFields(av, kind).then(function (all) {
        previewCache[key] = { total: all.total, required: req.fields || [] };
        paint(previewCache[key]);
      });
    }).catch(function () { if (previewFor === key) hideKindPreview(); });
  }

  railEl.addEventListener("mouseover", function (e) {
    const row = e.target.closest(".kind[data-kind]");
    if (!row) { hideKindPreview(); return; }
    const key = row.getAttribute("data-kind") + "|" + row.getAttribute("data-av");
    if (key === previewFor) return;
    clearTimeout(previewTimer);
    previewTimer = setTimeout(function () { showKindPreview(row); }, 220);
  });
  railEl.addEventListener("mouseleave", hideKindPreview);
  railEl.addEventListener("dragstart", function (e) {
    hideKindPreview();
    const k = e.target.closest(".kind");
    if (!k) return;
    const payload = JSON.stringify({
      kind: k.getAttribute("data-kind"),
      apiVersion: k.getAttribute("data-av"),
      provider: k.getAttribute("data-provider"),
    });
    e.dataTransfer.effectAllowed = "copy";
    try {
      e.dataTransfer.setData("application/json", payload);
      e.dataTransfer.setData("text/plain", payload);
    } catch (_) { /* older engines */ }
  });

  /* ---- store subscriptions --------------------------------------------- */
  store.subscribe("error", function (e) {
    if (e && (e.source === "addParameter" || e.source === "deleteParameter")) { paramErr = e.message; drawRail(); }
  });

  let lastSourcesSig = "";
  store.subscribe("doc", function () {
    // Sources and kinds only change when a new doc has different sources (providers).
    const d = store.state.doc;
    const sig = ((d && d.spec && d.spec.sources) || [])
      .map(function (s) { return s.provider; }).join("|");
    if (sig !== lastSourcesSig) {
      lastSourcesSig = sig;
      loadKinds();
    }
    if (rail !== "kinds") drawRail();
  });

  /* ---- boot ------------------------------------------------------------ */
  drawRail();   // paints "Loading kinds…" immediately
  loadKinds();
}
