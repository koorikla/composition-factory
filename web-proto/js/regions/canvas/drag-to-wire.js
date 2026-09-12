/**
 * canvas/drag-to-wire.js — drag-to-wire interaction state machine and field picker.
 *
 * Handles dragging from output/status/parameter ports to create wires:
 * - Interactive SVG preview path during pointer drag
 * - Drop onto a matching port dot -> direct wire application
 * - Drop onto a destination node card -> modal field/envelope/annotation picker
 * - Type compatibility checking and mismatch warnings
 * - Read-only metadata filtering
 */

import { store as defaultStore } from "../../store.js";
import * as defaultApi from "../../api.js";
import { esc } from "../../dom.js";
import { startDrag } from "../../drag.js";
import { COLORS } from "../../utils.js";
import { XR_ID, ENV_ID } from "./layout.js";

/** @type {Record<string, any>} */
let deps = {
  store: defaultStore,
  api: defaultApi,
  getCwEl: () => (typeof document !== "undefined" ? document.getElementById("cw") : null),
  getWiresEl: () => (typeof document !== "undefined" ? document.getElementById("wires") : null),
  portPos: () => null,
  drawWires: () => {},
  gestureBegin: () => {},
  gestureEnd: () => {},
  kindMeta: () => null,
  getKindsCache: () => null,
  setKindsCache: () => {},
  XR_ID,
  ENV_ID,
};

let pickerJustOpened = false;

export function initDragToWire(customDeps) {
  deps = Object.assign({}, deps, customDeps);
}

export function isPickerJustOpened() {
  return pickerJustOpened;
}

function getStore() {
  return deps.store || defaultStore;
}

function getApi() {
  return deps.api || defaultApi;
}

function doc() {
  return getStore().state.doc;
}

export function applyWire(srcOwner, srcPath, targetRes, targetPath) {
  const xrId = deps.XR_ID || XR_ID;
  const envId = deps.ENV_ID || ENV_ID;
  let fromExpr = "";
  if (srcPath && (srcPath.startsWith("params.") || srcPath.startsWith("env.") || srcPath.startsWith("resources."))) {
    fromExpr = srcPath;
  } else if (srcOwner === xrId) {
    fromExpr = "params." + srcPath;
  } else if (srcOwner === envId || srcOwner === "environment") {
    fromExpr = "env." + srcPath;
  } else if (srcPath === "metadata.name" || srcPath === "status.metadata.name") {
    fromExpr = "resources." + srcOwner + ".metadata.name";
  } else {
    fromExpr = "resources." + srcOwner + ".status." + srcPath.replace(/^status\./, "");
  }
  const S = getStore();
  return S.replaceDoc(function (draft) {
    const r = (draft.spec.resources || []).find(function (x) { return x.name === targetRes; });
    if (!r) return;
    if (targetPath.indexOf("envelope.") === 0) {
      r.envelope = r.envelope || {};
      r.envelope[targetPath.slice("envelope.".length)] = { from: fromExpr };
    } else if (targetPath.indexOf("annotations.") === 0) {
      r.annotations = r.annotations || {};
      r.annotations[targetPath.slice("annotations.".length)] = { from: fromExpr };
    } else if (targetPath === "forEach") {
      r.forEach = fromExpr;
    } else if (targetPath === "when") {
      r.when = fromExpr;
    } else {
      r.fields = r.fields || {};
      r.fields[targetPath] = { from: fromExpr };
    }
  }).then(function (ok) {
    if (ok) {
      S.select(targetRes);
      if (deps.drawWires) deps.drawWires();
    }
    return ok;
  });
}

export function closeWirePicker() {
  if (typeof document === "undefined") return;
  const el = document.getElementById("wire-picker");
  if (el) el.remove();
}

async function resolveKindMetaAsync(resource) {
  const kindsCache = deps.getKindsCache ? deps.getKindsCache() : null;
  if (kindsCache && kindsCache.length) {
    const m = deps.kindMeta ? deps.kindMeta(resource) : null;
    if (m) return m;
  }
  try {
    const A = getApi();
    const res = await A.getKinds();
    const loaded = res.kinds || [];
    if (deps.setKindsCache) deps.setKindsCache(loaded);
    return (deps.kindMeta ? deps.kindMeta(resource) : null) || (loaded.find(function (k) { return k.kind === resource.kind; }) || null);
  } catch (_) {
    return null;
  }
}

export function isFieldPickerTypeMatch(srcType, targetType) {
  if (!srcType || !targetType) return true;
  if (srcType === targetType) return true;
  if ((srcType === "integer" || srcType === "number") && (targetType === "integer" || targetType === "number")) return true;
  if ((srcType === "map" || srcType === "object") && (targetType === "map" || targetType === "object")) return true;
  return false;
}

export function renderFieldPickerItems(listEl, items, selectedIndex, srcPath) {
  if (!items.length) {
    listEl.innerHTML = '<div class="empty">No matching fields found.</div>';
    return;
  }
  let h = "";
  let lastCat = null;
  items.forEach(function (item, idx) {
    if (item.category !== lastCat) {
      lastCat = item.category;
      h += '<div class="wire-picker-cat">' + esc(lastCat) + '</div>';
    }
    const mismatchBadge = item.typeMismatch
      ? '<span class="wire-picker-mismatch" title="Type mismatch: $' + esc(srcPath) + ' is ' + esc(item.srcType) + ', but field expects ' + esc(item.targetType) + '">mismatch: ' + esc(item.srcType) + ' \u2260 ' + esc(item.targetType) + '</span>'
      : '';
    const optWarnBadge = item.optionalWarning
      ? '<span class="wire-picker-opt-warning" title="Optional parameter bound to required field: render will omit if missing">optional \u2192 req</span>'
      : '';
    h += '<div class="wire-picker-item' + (idx === selectedIndex ? ' active' : '') + (item.suggested ? ' match' : '') + '" data-idx="' + idx + '">' +
      '<span style="font-family:var(--mono);color:' + (item.color || 'inherit') + '">' + esc(item.label || item.path) + '</span>' +
      '<span class="dg">' + esc(item.type || "") + '</span>' +
      (item.required ? '<span class="rq">req</span>' : '') +
      optWarnBadge +
      mismatchBadge +
      (item.description ? '<span class="desc" title="' + esc(item.description) + '">' + esc(item.description) + '</span>' : '') +
      '</div>';
  });
  listEl.innerHTML = h;
}

export function isReadOnlyMetadataField(path) {
  if (!path) return false;
  const p = path.replace(/^metadata\./, "");
  return (
    p === "managedFields" || p.startsWith("managedFields[") || p.startsWith("managedFields.") ||
    p === "ownerReferences" || p.startsWith("ownerReferences[") || p.startsWith("ownerReferences.") ||
    p === "uid" || p === "resourceVersion" || p === "creationTimestamp" || p === "generation" ||
    p === "deletionTimestamp" || p === "deletionGracePeriodSeconds" || p === "selfLink"
  );
}

export function buildFieldPickerCandidates(specFields, envelopeFields, filter, ctx) {
  const xrId = deps.XR_ID || XR_ID;
  const envId = deps.ENV_ID || ENV_ID;
  const rawQuery = (filter || "").trim();
  const q = rawQuery.toLowerCase();
  const items = [];
  const leafName = (ctx.srcPath || "").split(".").pop() || "";
  const srcTerm = leafName.toLowerCase().replace(/[^a-z0-9]/g, "");
  const srcOwnerTerm = (ctx.srcOwner && ctx.srcOwner !== xrId && ctx.srcOwner !== envId && ctx.srcOwner !== "environment")
    ? ctx.srcOwner.toLowerCase().replace(/[^a-z0-9]/g, "") : "";
  const srcType = ctx.srcType || "string";
  const res = ctx.resource || {};
  const isSecret = res.kind === "Secret";
  const isSrcParamReq = !!(ctx.param && (ctx.param.required || ctx.param.requiredChain));

  // 1. Spec / forProvider fields
  (specFields || []).forEach(function (f) {
    const p = f.path;
    if (isReadOnlyMetadataField(p)) return;
    const pLower = p.toLowerCase();
    let desc = f.description || "";
    if (isSecret) {
      if (p === "data" || p.startsWith("data.") || p.startsWith("data[")) {
        desc = desc ? desc + " (base64 encoded)" : "Secret data (base64 encoded, auto-encoded on wire)";
      } else if (p === "stringData" || p.startsWith("stringData.") || p.startsWith("stringData[")) {
        desc = desc ? desc + " (plaintext)" : "Secret stringData (unencoded plaintext)";
      }
    }
    const descLower = desc.toLowerCase();
    const pNorm = pLower.replace(/[^a-z0-9]/g, "");
    if (q && pLower.indexOf(q) === -1 && descLower.indexOf(q) === -1) {
      return;
    }
    const targetType = f.type || "string";
    const typeMatch = isFieldPickerTypeMatch(srcType, targetType);
    const isReq = !!(f.requiredChain || f.required);
    const isOptWarn = !!(isReq && ctx.srcOwner === xrId && !isSrcParamReq);
    const isMatch = (srcTerm && (pNorm.indexOf(srcTerm) >= 0 || srcTerm.indexOf(pNorm) >= 0)) ||
      (srcOwnerTerm && (pNorm.indexOf(srcOwnerTerm) >= 0 || srcOwnerTerm.indexOf(pNorm) >= 0));
    let score = 20;
    if (isReq) score += 40;
    if (isSecret && (p === "stringData" || p.startsWith("stringData."))) score += 15;
    if (isMatch && typeMatch) score += 100;
    else if (isMatch) score += 30;
    if (!typeMatch) score -= 30;
    if (q) {
      if (pLower === q) score += 60;
      else if (pLower.startsWith(q)) score += 40;
      else if (pLower.indexOf(q) >= 0) score += 20;
    }
    items.push({
      type: targetType,
      path: p,
      label: p,
      category: isMatch && typeMatch && !q ? "Suggested Matches" : "Spec Fields",
      required: isReq,
      optionalWarning: isOptWarn,
      suggested: isMatch && typeMatch,
      typeMismatch: !typeMatch,
      srcType: srcType,
      targetType: targetType,
      description: desc,
      applyType: "field",
      score: score,
    });
  });

  // 2. Envelope fields
  (envelopeFields || []).forEach(function (ef) {
    const p = ef.path;
    if (isReadOnlyMetadataField(p)) return;
    const pLower = p.toLowerCase();
    const desc = ef.description || "";
    if (q && pLower.indexOf(q) === -1 && desc.toLowerCase().indexOf(q) === -1) {
      return;
    }
    const isReq = !!(ef.requiredChain || ef.required);
    const isOptWarn = !!(isReq && ctx.srcOwner === xrId && !isSrcParamReq);
    let score = 10;
    if (isReq) score += 30;
    if (q) {
      if (pLower === q) score += 50;
      else if (pLower.startsWith(q)) score += 30;
      else if (pLower.indexOf(q) >= 0) score += 15;
    }
    items.push({
      type: ef.type || "string",
      path: p,
      label: "envelope." + p,
      category: "Envelope",
      required: isReq,
      optionalWarning: isOptWarn,
      suggested: false,
      description: desc,
      applyType: "envelope",
      color: "var(--wire-ref)",
      score: score,
    });
  });

  // 3. Known annotations (if relevant)
  const isK8sOrIAM = (res.provider && (res.provider.indexOf("aws") >= 0 || res.provider.indexOf("k8s") >= 0)) ||
    res.kind === "ServiceAccount" || res.kind === "Role";
  if (isK8sOrIAM) {
    const knownAnns = [
      { key: "eks.amazonaws.com/role-arn", desc: "EKS IAM Role ARN to assume" },
      { key: "crossplane.io/external-name", desc: "Cloud resource name override" },
    ];
    knownAnns.forEach(function (ann) {
      const kLower = ann.key.toLowerCase();
      const dLower = ann.desc.toLowerCase();
      if (!q || kLower.indexOf(q) >= 0 || dLower.indexOf(q) >= 0) {
        const isMatch = srcTerm.indexOf("arn") >= 0 || srcTerm.indexOf("role") >= 0;
        let score = 5;
        if (isMatch) score += 80;
        if (q && kLower.indexOf(q) >= 0) score += 25;
        items.push({
          type: "string",
          path: ann.key,
          label: "annotations." + ann.key,
          category: isMatch && !q ? "Suggested Matches" : "Annotations",
          required: false,
          suggested: isMatch,
          description: ann.desc,
          applyType: "ann",
          color: "var(--wire-status)",
          score: score,
        });
      }
    });
  }

  // 4. Custom query option if typed (preserves exact casing)
  if (rawQuery) {
    if (!items.some(function (it) { return it.path === rawQuery && it.applyType === "ann"; })) {
      items.push({
        type: "string",
        path: rawQuery,
        label: "annotations." + rawQuery,
        category: "Custom",
        required: false,
        suggested: false,
        description: "Set as custom annotation",
        applyType: "ann",
        color: "var(--wire-status)",
        score: -10,
      });
    }
    if (!items.some(function (it) { return it.path === rawQuery && it.applyType === "field"; })) {
      items.push({
        type: "string",
        path: rawQuery,
        label: rawQuery,
        category: "Custom",
        required: false,
        suggested: false,
        description: "Set as custom field path",
        applyType: "field",
        score: -20,
      });
    }
  }

  items.sort(function (a, b) {
    if (b.score !== a.score) return b.score - a.score;
    return a.label.localeCompare(b.label);
  });

  return items;
}

export function openFieldPicker(x, y, srcOwner, srcPath, targetRes) {
  closeWirePicker();
  pickerJustOpened = true;
  setTimeout(function () { pickerJustOpened = false; }, 150);
  const d = doc();
  if (!d) return;
  const res = (d.spec.resources || []).find(function (r) { return r.name === targetRes; });
  if (!res) return;

  const pop = document.createElement("div");
  pop.id = "wire-picker";
  pop.className = "wire-picker";

  const left = Math.min(x, window.innerWidth - 370);
  const top = Math.min(y, window.innerHeight - 450);
  pop.style.left = Math.max(10, left) + "px";
  pop.style.top = Math.max(10, top) + "px";

  const xrId = deps.XR_ID || XR_ID;
  const envId = deps.ENV_ID || ENV_ID;
  const isEnv = srcOwner === envId || srcOwner === "environment";
  const srcLabel = srcOwner === xrId ? "$" + srcPath : (isEnv ? "env." + srcPath : srcOwner + "." + srcPath.replace(/^status\./, ""));
  pop.innerHTML =
    '<div class="wire-picker-h">' +
    '<span>Wire <span style="color:' + (isEnv ? 'var(--shared)' : 'var(--wire-xrd)') + '">' + esc(srcLabel) + '</span> \u2192 ' + esc(targetRes) + '</span>' +
    '<button class="del" id="wire-picker-close" style="margin-left:auto;cursor:pointer">\u00d7</button></div>' +
    '<div style="padding:6px 10px;border-bottom:1px solid var(--rule)">' +
    '<input id="wire-picker-search" class="search" style="width:100%" placeholder="Search fields, envelope, annotations\u2026" autofocus>' +
    '</div>' +
    '<div class="wire-picker-list" id="wire-picker-list"><div class="empty">Loading fields\u2026</div></div>';

  document.body.appendChild(pop);
  pop.querySelector("#wire-picker-close").addEventListener("click", closeWirePicker);
  const searchInput = /** @type {HTMLInputElement|null} */ (pop.querySelector("#wire-picker-search"));
  const listEl = pop.querySelector("#wire-picker-list");
  if (searchInput) setTimeout(function () { searchInput.focus(); }, 20);

  let selectedIndex = 0;
  let currentItems = [];

  let srcType = "string";
  let paramObj = null;
  if (srcOwner === xrId) {
    paramObj = (d.spec.xrd && d.spec.xrd.parameters && d.spec.xrd.parameters[srcPath]) || {};
    srcType = paramObj.type || "string";
  } else if (isEnv) {
    const envObj = (d.spec && d.spec.environment && d.spec.environment[srcPath]) || {};
    srcType = envObj.type || "string";
  }

  const pickerContext = {
    srcOwner: srcOwner,
    srcPath: srcPath,
    srcType: srcType,
    param: paramObj,
    resource: res,
  };

  let cachedSpecFields = [];
  let cachedEnvelopeFields = [];

  function updateList(q) {
    currentItems = buildFieldPickerCandidates(cachedSpecFields, cachedEnvelopeFields, q, pickerContext);
    selectedIndex = Math.min(selectedIndex, Math.max(0, currentItems.length - 1));
    renderFieldPickerItems(listEl, currentItems, selectedIndex, srcPath);
  }

  const S = getStore();
  async function selectItem(item) {
    if (!item) return;
    closeWirePicker();
    if (item.typeMismatch && srcOwner === xrId && item.targetType) {
      if (window.confirm("Parameter '$" + srcPath + "' is " + item.srcType + ", but '" + item.path + "' expects " + item.targetType + ".\n\nConvert parameter type to " + item.targetType + " and wire?")) {
        const pObj = (d.spec.xrd && d.spec.xrd.parameters && d.spec.xrd.parameters[srcPath]) || {};
        const pPatch = Object.assign({}, pObj, {
          type: item.targetType,
          enum: null,
          default: "",
        });
        await S.updateParameter(srcPath, pPatch);
      }
    }
    if (item.optionalWarning && srcOwner === xrId) {
      if (window.confirm("Parameter '$" + srcPath + "' is optional, but '" + item.path + "' is required.\n\nMark parameter as required to guarantee presence in render?")) {
        const pObj = (d.spec.xrd && d.spec.xrd.parameters && d.spec.xrd.parameters[srcPath]) || {};
        await S.updateParameter(srcPath, Object.assign({}, pObj, { required: true }));
      }
    }
    const applyActions = {
      ann: function () { return applyWire(srcOwner, srcPath, targetRes, "annotations." + item.path); },
      envelope: function () { return applyWire(srcOwner, srcPath, targetRes, "envelope." + item.path); },
      field: function () { return applyWire(srcOwner, srcPath, targetRes, item.path); },
    };
    const action = applyActions[item.applyType] || applyActions.field;
    return action();
  }

  function updateActiveItem() {
    listEl.querySelectorAll(".wire-picker-item").forEach(function (el, idx) {
      const isSel = idx === selectedIndex;
      el.classList.toggle("active", isSel);
      if (isSel) {
        el.scrollIntoView({ block: "nearest" });
      }
    });
  }

  const A = getApi();
  resolveKindMetaAsync(res).then(function (meta) {
    if (!meta) {
      cachedSpecFields = Object.keys(res.fields || {}).map(function (k) {
        return { path: k, type: "string" };
      });
      updateList(searchInput ? searchInput.value : "");
      return;
    }
    Promise.all([
      A.getKindFields(meta.apiVersion, meta.kind).catch(function () { return { fields: [] }; }),
      A.getKind(meta.apiVersion, meta.kind).catch(function () { return { envelope: [] }; })
    ]).then(function (results) {
      cachedSpecFields = (results[0] && results[0].fields) || [];
      cachedEnvelopeFields = (results[1] && results[1].envelope) || [];
      updateList(searchInput ? searchInput.value : "");
    }).catch(function (err) {
      listEl.innerHTML = '<div class="empty">Failed to load fields: ' + esc(err && err.message || err) + '</div>';
    });
  });

  if (searchInput) {
    searchInput.addEventListener("input", function () {
      selectedIndex = 0;
      updateList(searchInput.value);
    });
    const keyActions = {
      ArrowDown: function () {
        if (currentItems.length > 0) {
          selectedIndex = (selectedIndex + 1) % currentItems.length;
          updateActiveItem();
        }
      },
      ArrowUp: function () {
        if (currentItems.length > 0) {
          selectedIndex = (selectedIndex - 1 + currentItems.length) % currentItems.length;
          updateActiveItem();
        }
      },
      Enter: function () {
        selectItem(currentItems[selectedIndex]);
      },
      Escape: function () {
        closeWirePicker();
      },
    };
    searchInput.addEventListener("keydown", function (e) {
      if (keyActions[e.key]) {
        e.preventDefault();
        keyActions[e.key]();
      }
    });
  }

  listEl.addEventListener("click", function (e) {
    const itemEl = e.target.closest(".wire-picker-item");
    if (!itemEl) return;
    const idx = parseInt(itemEl.getAttribute("data-idx"), 10);
    selectItem(currentItems[idx]);
  });
}

export function onWireDragDown(e, portEl) {
  const xrId = deps.XR_ID || XR_ID;
  const envId = deps.ENV_ID || ENV_ID;
  const owner = portEl.getAttribute("data-owner");
  const path = portEl.getAttribute("data-path");
  const isOut = portEl.querySelector(".d.out") !== null || (e.target.classList && e.target.classList.contains("out"));
  const dir = isOut ? "out" : "in";

  const startPt = deps.portPos ? deps.portPos(owner, path) : null;
  if (!startPt) return;

  const startClientX = e.clientX, startClientY = e.clientY;
  let hasMoved = false;

  let previewPath = null;
  const isStatus = path.indexOf("status.") === 0;
  const isEnv = owner === envId || owner === "environment";
  const strokeColor = owner === xrId ? COLORS.xrd : (isEnv ? "var(--shared)" : (isStatus ? "var(--wire-status)" : COLORS.xrd));

  let lastHoverNode = null;
  let lastHoverPort = null;

  function clearHovers() {
    if (lastHoverNode) { lastHoverNode.classList.remove("wire-target-hover"); lastHoverNode = null; }
    if (lastHoverPort) { lastHoverPort.classList.remove("wire-target-hover"); lastHoverPort = null; }
  }

  function isValidTargetPort(targetPortEl) {
    if (!targetPortEl) return false;
    const tOwner = targetPortEl.getAttribute("data-owner");
    const tPath = targetPortEl.getAttribute("data-path");
    if (!tOwner || tOwner === owner) return false;
    if (dir === "out") {
      if (tOwner === xrId || tOwner === envId || tOwner === "environment") return false;
      if (tPath.startsWith("status.") || tPath.indexOf("status.") === 0) return false;
      if (tPath === "when") {
        return owner === xrId || owner === envId || owner === "environment";
      }
      return true;
    }
    if (dir === "in") {
      if (path === "when") {
        return tOwner === xrId || tOwner === envId || tOwner === "environment";
      }
      return tOwner === xrId || tOwner === envId || tOwner === "environment" || tPath.startsWith("status.") || tPath.indexOf("status.") === 0;
    }
    return false;
  }

  const cwEl = deps.getCwEl ? deps.getCwEl() : null;
  const wiresEl = deps.getWiresEl ? deps.getWiresEl() : null;

  function mv(ev) {
    if (!ev.buttons) { up(ev); return; }
    if (!hasMoved) {
      const dist = Math.hypot(ev.clientX - startClientX, ev.clientY - startClientY);
      if (dist < 4) return;
      hasMoved = true;
      previewPath = document.createElementNS("http://www.w3.org/2000/svg", "path");
      previewPath.id = "wire-drag-preview";
      previewPath.setAttribute("stroke", strokeColor);
      previewPath.setAttribute("fill", "none");
      previewPath.setAttribute("stroke-width", "2.5");
      if (wiresEl) wiresEl.appendChild(previewPath);
    }

    const cw = cwEl ? cwEl.getBoundingClientRect() : { left: 0, top: 0 };
    const mousePt = { x: ev.clientX - cw.left, y: ev.clientY - cw.top };

    let a = startPt, b = mousePt;
    if (dir === "in") { a = mousePt; b = startPt; }
    const dx = Math.max(30, Math.abs(b.x - a.x) * 0.45);
    if (previewPath) {
      previewPath.setAttribute("d", "M" + a.x + "," + a.y + " C" + (a.x + dx) + "," + a.y + " " + (b.x - dx) + "," + b.y + " " + b.x + "," + b.y);
    }

    clearHovers();
    const elements = document.elementsFromPoint(ev.clientX, ev.clientY) || [];
    let p = null, n = null;
    for (let i = 0; i < elements.length; i++) {
      if (!p) {
        const candidate = elements[i].closest(".port");
        if (isValidTargetPort(candidate)) p = candidate;
      }
      if (!n) n = elements[i].closest(".node");
    }
    if (p) {
      lastHoverPort = p;
      p.classList.add("wire-target-hover");
    } else if (n && n.getAttribute("data-id") !== owner) {
      if (dir === "out" && n.getAttribute("data-id") !== xrId && n.getAttribute("data-id") !== envId && n.getAttribute("data-id") !== "environment") {
        lastHoverNode = n;
        n.classList.add("wire-target-hover");
      }
    }
  }

  function up(ev) {
    if (previewPath) { previewPath.remove(); previewPath = null; }
    clearHovers();
    if (deps.gestureEnd) deps.gestureEnd();

    const S = getStore();
    if (!ev || !hasMoved) {
      // Just a click on the port: select the node
      if (owner) S.select(owner);
      return;
    }

    const elements = document.elementsFromPoint(ev.clientX, ev.clientY) || [];
    let targetPort = null, targetNode = null;
    for (let i = 0; i < elements.length; i++) {
      if (!targetPort) {
        const candidate = elements[i].closest(".port");
        if (isValidTargetPort(candidate)) targetPort = candidate;
      }
      if (!targetNode) targetNode = elements[i].closest(".node");
    }

    if (targetPort) {
      const tOwner = targetPort.getAttribute("data-owner");
      const tPath = targetPort.getAttribute("data-path");
      if (dir === "out" && tOwner !== xrId && tOwner !== envId && tOwner !== "environment") {
        applyWire(owner, path, tOwner, tPath);
      } else if (dir === "in" && (tOwner === xrId || tOwner === envId || tOwner === "environment" || tPath.indexOf("status.") === 0)) {
        applyWire(tOwner, tPath, owner, path);
      }
      return;
    }

    if (targetNode) {
      const tId = targetNode.getAttribute("data-id");
      if (tId && tId !== owner) {
        if (dir === "out" && tId !== xrId && tId !== envId && tId !== "environment") {
          openFieldPicker(ev.clientX, ev.clientY, owner, path, tId);
        }
      }
    }
  }

  const abortDrag = startDrag(e, mv, up);
  if (deps.gestureBegin) {
    deps.gestureBegin(function () {
      abortDrag();
      if (previewPath) { previewPath.remove(); previewPath = null; }
      clearHovers();
    });
  }
}
