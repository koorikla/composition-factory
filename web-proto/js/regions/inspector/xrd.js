/**
 * Submodule: inspector/xrd.js
 * XRD composite form rendering, parameter member tree, pipeline step schemas.
 */

import { esc } from "../../dom.js";
import { fanOut } from "../../wires.js";
import { state, PARAM_TYPES } from "./state.js";

export { PARAM_TYPES };

export function parseWhen(str) {
  if (!str) return {};
  var m = /^params\.([A-Za-z][A-Za-z0-9]*)(?:\s(==|!=)\s"([^"]*)")?$/.exec(str);
  if (!m) return {};
  return { param: m[1], op: m[2] || "==", val: m[3] };
}

export function paramsOf(doc) {
  return doc && doc.spec && doc.spec.xrd && doc.spec.xrd.parameters || {};
}

export function isParamLocked(doc, n) {
  if (!doc || !doc.spec || n !== "providerName") return false;
  var xrd = doc.spec.xrd || {};
  var scope = xrd.scope || "Namespaced";
  if (scope !== "Namespaced") return false;
  var resources = doc.spec.resources || [];
  return resources.length === 0 || resources.some(function (r) {
    return r && r.provider !== "k8s";
  });
}

export function isParamRef(ref, pn) {
  if (typeof ref !== "string" || !ref || !pn) return false;
  if (ref === "params." + pn || ref.indexOf("params." + pn + ".") === 0) return true;
  if (ref === "parameters." + pn || ref.indexOf("parameters." + pn + ".") === 0) return true;
  return false;
}

export function isRawParamRef(raw, pn) {
  if (typeof raw !== "string" || !raw || !pn) return false;
  var escaped = pn.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  var re = new RegExp("(?:\\$spec|\\.spec|\\$params|\\.params|params|parameters)\\." + escaped + "(?:$|[^a-zA-Z0-9_])");
  return re.test(raw);
}

export function isObjectReferencingParam(obj, pn) {
  if (!obj || typeof obj !== "object") return false;
  for (var k of Object.keys(obj)) {
    var v = obj[k];
    if (typeof v === "string" && (isParamRef(v, pn) || isRawParamRef(v, pn))) return true;
    if (typeof v === "object" && isObjectReferencingParam(v, pn)) return true;
  }
  return false;
}

export function isWhenReferencingParam(whenStr, pn) {
  if (!whenStr || typeof whenStr !== "string") return false;
  if (isParamRef(whenStr, pn)) return true;
  var parsed = parseWhen(whenStr);
  if (parsed && parsed.param === pn) return true;
  var m = /^(?:params|parameters)\.([A-Za-z0-9_-]+)/.exec(whenStr);
  if (m && m[1] === pn) return true;
  return false;
}

export function cleanParamRefs(draft, pn) {
  if (!draft || !draft.spec) return;
  if (draft.spec.xrd && draft.spec.xrd.parameters) {
    delete draft.spec.xrd.parameters[pn];
  }
  var resources = draft.spec.resources || [];
  resources.forEach(function (r) {
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        var f = r.fields[k];
        if (f && (isParamRef(f.from, pn) || isRawParamRef(f.raw, pn))) {
          delete r.fields[k];
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        var f = r.envelope[k];
        if (f && (isParamRef(f.from, pn) || isRawParamRef(f.raw, pn))) {
          delete r.envelope[k];
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        var f = r.annotations[k];
        if (f && (isParamRef(f.from, pn) || isRawParamRef(f.raw, pn))) {
          delete r.annotations[k];
        }
      });
      if (Object.keys(r.annotations).length === 0) delete r.annotations;
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        if (isParamRef(r.connectionSecret, pn) || isRawParamRef(r.connectionSecret, pn)) {
          delete r.connectionSecret;
        }
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys = r.connectionSecret.keys.filter(function (item) {
            if (typeof item === "string") return !isParamRef(item, pn) && !isRawParamRef(item, pn);
            if (item && typeof item === "object") {
              if (item.from && (isParamRef(item.from, pn) || isRawParamRef(item.from, pn))) return false;
              if (item.raw && isRawParamRef(item.raw, pn)) return false;
              if (isObjectReferencingParam(item, pn)) return false;
            }
            return true;
          });
          if (r.connectionSecret.keys.length === 0) delete r.connectionSecret;
        } else if (isObjectReferencingParam(r.connectionSecret, pn)) {
          delete r.connectionSecret;
        }
      }
    }
    if (r.when && isWhenReferencingParam(r.when, pn)) {
      delete r.when;
    }
    if (r.forEach && (isParamRef(r.forEach, pn) || isRawParamRef(r.forEach, pn))) {
      delete r.forEach;
    }
  });
}

var catFnsPromise = null;
export async function getCatalogueFunctions() {
  if (!catFnsPromise) {
    catFnsPromise = state.api.getCatalogueFunctions().catch(function () { return []; });
  }
  return catFnsPromise;
}

export function inferFnMeta(step) {
  var ref = step.functionRef || "";
  var pkg = step.package || "";
  if (ref === "function-auto-ready" || pkg.indexOf("function-auto-ready") !== -1) {
    return null; // auto-ready has no input
  }
  if (ref === "function-environment-configs" || pkg.indexOf("function-environment-configs") !== -1) {
    return { apiVersion: "environmentconfigs.fn.crossplane.io/v1beta1", kind: "Input" };
  }
  if (ref === "function-cel-filter" || pkg.indexOf("function-cel-filter") !== -1) {
    return { apiVersion: "cel.fn.crossplane.io/v1alpha1", kind: "Filter" };
  }
  if (ref === "function-extra-resources" || pkg.indexOf("function-extra-resources") !== -1) {
    return { apiVersion: "extra-resources.fn.crossplane.io/v1alpha1", kind: "Input" };
  }
  return null;
}

function parseYAMLScalar(v) {
  if (v === undefined || v === null) return "";
  v = v.trim();
  if (!v) return "";
  if ((v.startsWith('"') && v.endsWith('"') && v.length >= 2) ||
      (v.startsWith("'") && v.endsWith("'") && v.length >= 2)) {
    if (v.startsWith('"')) {
      try { return JSON.parse(v); } catch (_) {}
    }
    return v.slice(1, -1);
  }
  var hashIdx = v.indexOf(" #");
  if (hashIdx !== -1) {
    v = v.substring(0, hashIdx).trim();
  }
  if (v === "true") return true;
  if (v === "false") return false;
  if (v === "null") return null;
  if (!isNaN(Number(v)) && !isNaN(parseFloat(v))) return Number(v);
  if (v.startsWith("[") || v.startsWith("{")) {
    try { return JSON.parse(v); } catch (_) {}
  }
  return v;
}

function readBlockScalar(lines, startIdx, parentIndent) {
  var block = [];
  var j = startIdx + 1;
  var blockIndent = -1;
  while (j < lines.length) {
    var bLine = lines[j];
    if (!bLine.trim()) {
      block.push("");
      j++;
      continue;
    }
    var bIndent = bLine.search(/\S/);
    if (blockIndent === -1) {
      if (bIndent > parentIndent) blockIndent = bIndent;
      else break;
    }
    if (bIndent < blockIndent) break;
    block.push(bLine.substring(blockIndent));
    j++;
  }
  return { value: block.join("\n").replace(/\n+$/, ""), nextIndex: j };
}

function parseYAMLBlock(lines, startIndex, currentIndent) {
  var result = null;
  var i = startIndex;

  while (i < lines.length) {
    var rawLine = lines[i];
    var trimmed = rawLine.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      i++;
      continue;
    }

    var indent = rawLine.search(/\S/);
    if (indent < currentIndent) {
      break;
    }

    var content = rawLine.substring(indent);
    if (content.startsWith("- ") || content === "-") {
      if (result === null) result = [];
      else if (!Array.isArray(result)) break;

      var afterDash = content.substring(1).trim();
      if (!afterDash) {
        var dashChild = parseYAMLBlock(lines, i + 1, indent + 1);
        result.push(dashChild.value);
        i = dashChild.nextIndex;
        continue;
      }

      var dashColon = afterDash.indexOf(":");
      if (dashColon !== -1 && (dashColon === afterDash.length - 1 || afterDash[dashColon + 1] === " ")) {
        var itemObj = {};
        var itemKey = afterDash.substring(0, dashColon).trim();
        var itemVal = afterDash.substring(dashColon + 1).trim();
        if (itemVal === "|" || itemVal === "|-") {
          var itemBlock = readBlockScalar(lines, i, indent);
          itemObj[itemKey] = itemBlock.value;
          i = itemBlock.nextIndex;
        } else if (itemVal) {
          itemObj[itemKey] = parseYAMLScalar(itemVal);
          i++;
        } else {
          var itemChild = parseYAMLBlock(lines, i + 1, indent + 2);
          itemObj[itemKey] = itemChild.value;
          i = itemChild.nextIndex;
        }

        while (i < lines.length) {
          var nextRaw = lines[i];
          if (!nextRaw.trim() || nextRaw.trim().startsWith("#")) {
            i++;
            continue;
          }
          var nextIndent = nextRaw.search(/\S/);
          if (nextIndent <= indent) break;
          var nextContent = nextRaw.substring(nextIndent);
          if (nextContent.startsWith("- ") || nextContent === "-") break;
          var nextColon = nextContent.indexOf(":");
          if (nextColon !== -1 && (nextColon === nextContent.length - 1 || nextContent[nextColon + 1] === " ")) {
            var nk = nextContent.substring(0, nextColon).trim();
            var nv = nextContent.substring(nextColon + 1).trim();
            if (nv === "|" || nv === "|-") {
              var nBlock = readBlockScalar(lines, i, nextIndent);
              itemObj[nk] = nBlock.value;
              i = nBlock.nextIndex;
            } else if (nv) {
              itemObj[nk] = parseYAMLScalar(nv);
              i++;
            } else {
              var nChild = parseYAMLBlock(lines, i + 1, nextIndent + 1);
              itemObj[nk] = nChild.value;
              i = nChild.nextIndex;
            }
          } else {
            i++;
          }
        }
        result.push(itemObj);
        continue;
      } else {
        result.push(parseYAMLScalar(afterDash));
        i++;
        continue;
      }
    } else {
      if (result === null) result = {};
      else if (Array.isArray(result)) break;

      var colonIdx = content.indexOf(":");
      if (colonIdx === -1 || (colonIdx < content.length - 1 && content[colonIdx + 1] !== " ")) {
        i++;
        continue;
      }

      var k = content.substring(0, colonIdx).trim();
      var v = content.substring(colonIdx + 1).trim();

      if (v === "|" || v === "|-") {
        var mapBlock = readBlockScalar(lines, i, indent);
        result[k] = mapBlock.value;
        i = mapBlock.nextIndex;
      } else if (v) {
        result[k] = parseYAMLScalar(v);
        i++;
      } else {
        var mapChild = parseYAMLBlock(lines, i + 1, indent + 1);
        result[k] = mapChild.value !== null ? mapChild.value : "";
        i = mapChild.nextIndex;
      }
    }
  }

  return { value: result !== null ? result : {}, nextIndex: i };
}

export function parseInputYAML(str) {
  if (!str || !str.trim()) return {};
  var lines = str.split("\n");
  var res = parseYAMLBlock(lines, 0, 0);
  return (res.value && typeof res.value === "object" && !Array.isArray(res.value)) ? res.value : {};
}

export function serializeInputYAML(obj, indent) {
  indent = indent || 0;
  var pad = " ".repeat(indent);
  var lines = [];
  if (!obj || typeof obj !== "object") return "";
  var keys = Object.keys(obj);
  if (indent === 0) {
    keys.sort(function (a, b) {
      var order = { apiVersion: 1, kind: 2, spec: 3 };
      return (order[a] || 99) - (order[b] || 99);
    });
  }
  keys.forEach(function (k) {
    var v = obj[k];
    if (v === undefined || v === null || v === "") return;
    if (typeof v === "object" && !Array.isArray(v)) {
      var nested = serializeInputYAML(v, indent + 2);
      if (nested) {
        lines.push(pad + k + ":\n" + nested);
      }
    } else if (Array.isArray(v)) {
      if (v.length === 0) {
        lines.push(pad + k + ": []");
      } else {
        var arrLines = [];
        v.forEach(function (item) {
          if (item !== null && typeof item === "object" && !Array.isArray(item)) {
            var itemKeys = Object.keys(item);
            if (itemKeys.length === 0) {
              arrLines.push(pad + "  - {}");
            } else {
              var first = true;
              itemKeys.forEach(function (ik) {
                var iv = item[ik];
                if (iv === undefined) return;
                var prefix = first ? (pad + "  - ") : (pad + "    ");
                first = false;
                if (typeof iv === "object" && !Array.isArray(iv)) {
                  var nested = serializeInputYAML(iv, indent + 4);
                  arrLines.push(prefix + ik + ":\n" + nested);
                } else if (Array.isArray(iv)) {
                  arrLines.push(prefix + ik + ": " + JSON.stringify(iv));
                } else if (typeof iv === "string" && iv.indexOf("\n") !== -1) {
                  arrLines.push(prefix + ik + ": |\n" + iv.split("\n").map(function (l) { return pad + "      " + l; }).join("\n"));
                } else {
                  arrLines.push(prefix + ik + ": " + iv);
                }
              });
            }
          } else {
            arrLines.push(pad + "  - " + item);
          }
        });
        lines.push(pad + k + ":\n" + arrLines.join("\n"));
      }
    } else if (typeof v === "string" && v.indexOf("\n") !== -1) {
      lines.push(pad + k + ": |\n" + v.split("\n").map(function (l) { return pad + "  " + l; }).join("\n"));
    } else {
      lines.push(pad + k + ": " + v);
    }
  });
  return lines.join("\n");
}

export function getPathVal(obj, path) {
  var parts = path.split(".");
  var cur = obj;
  for (var i = 0; i < parts.length; i++) {
    if (!cur || typeof cur !== "object") return "";
    cur = cur[parts[i]];
  }
  return cur !== undefined && cur !== null ? String(cur) : "";
}

export function setPathVal(obj, path, val) {
  var parts = path.split(".");
  var cur = obj;
  for (var i = 0; i < parts.length - 1; i++) {
    if (!cur[parts[i]] || typeof cur[parts[i]] !== "object") {
      cur[parts[i]] = {};
    }
    cur = cur[parts[i]];
  }
  var last = parts[parts.length - 1];
  if (val === "" || val === undefined || val === null) {
    delete cur[last];
  } else if (val === "true") {
    cur[last] = true;
  } else if (val === "false") {
    cur[last] = false;
  } else if (!isNaN(Number(val)) && val.trim() !== "") {
    cur[last] = Number(val);
  } else {
    cur[last] = val;
  }
}

export async function fieldsFor(apiVersion, kind) {
  var key = apiVersion + "|" + kind;
  if (!state.fieldsCache[key]) {
    state.fieldsCache[key] = await state.api.getKindFields(apiVersion, kind);
  }
  return state.fieldsCache[key];
}

export function paramFrom(existing, patch) {
  var p = {
    type: (existing && existing.type) || "string",
    required: !!(existing && existing.required),
    enum: (existing && existing.enum) || null,
    default: (existing && existing.default) || "",
    description: (existing && existing.description) || "",
    properties: (existing && existing.properties) || null,
  };
  Object.keys(patch).forEach(function (k) { p[k] = patch[k]; });
  return p;
}

export function cloneProps(p) { return p ? JSON.parse(JSON.stringify(p)) : {}; }

export function memberParent(props, path) {
  var segs = path.split(".");
  var cur = props;
  for (var i = 0; i < segs.length - 1; i++) {
    cur = (cur[segs[i]] || {}).properties;
    if (!cur) return null;
  }
  return cur[segs[segs.length - 1]] === undefined ? null : { parent: cur, key: segs[segs.length - 1] };
}

export function memberContainer(props, parentPath) {
  if (!parentPath) return props;
  var segs = parentPath.split(".");
  var cur = props;
  for (var i = 0; i < segs.length; i++) {
    var m = cur[segs[i]];
    if (!m) return null;
    if (!m.properties) m.properties = {};
    cur = m.properties;
  }
  return cur;
}

export function commitMembers(paramName, props) {
  var params = paramsOf(state.store.state.doc);
  return state.store.updateParameter(paramName,
    paramFrom(params[paramName], { properties: Object.keys(props).length ? props : null }));
}

var MEMBER_TYPES = ["string", "integer", "number", "boolean", "object"];

export function memberTreeHtml(paramName, parentPath, props, depth) {
  var out = "";
  Object.keys(props || {}).sort().forEach(function (mn) {
    var mp = props[mn];
    var path = parentPath ? parentPath + "." + mn : mn;
    var key = paramName + "|" + path;
    out += '<div class="frow" style="margin:2px 0 0 ' + (depth * 12) + 'px">' +
      '<input class="tin" data-mname="' + esc(key) + '" value="' + esc(mn) + '" aria-label="Member name" style="flex:1;min-width:0">' +
      '<select class="tsel" data-mtype="' + esc(key) + '" aria-label="Member type">' +
      MEMBER_TYPES.map(function (t) {
        return "<option" + (t === mp.type ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<label class="g" style="display:inline-flex;align-items:center;gap:3px;font-size:11px">' +
      '<input type="checkbox" data-mreq="' + esc(key) + '"' + (mp.required ? " checked" : "") + ">req</label>" +
      (mp.type === "object" ? "" :
        '<input class="tin" data-mdef="' + esc(key) + '" value="' + esc(mp.default || "") + '" placeholder="default" style="flex:0 0 64px">') +
      '<button class="del" data-mdel="' + esc(key) + '" title="Remove member">\u00d7</button></div>';
    if (mp.type === "object") {
      out += memberTreeHtml(paramName, path, mp.properties, depth + 1);
      out += '<div style="margin-left:' + ((depth + 1) * 12) + 'px;padding:2px 0">' +
        '<button class="btn sm" data-madd="' + esc(paramName + "|" + path) + '">+ member</button></div>';
    }
  });
  return out;
}

export function paramDetailRow(n, p, locked) {
  if (p.type === "object") {
    var mh = (p.properties && Object.keys(p.properties).length ? "" :
      '<div class="g" style="padding:2px 0 2px">no members \u2192 free-form map (string values); add members for a typed schema</div>');
    mh += memberTreeHtml(n, "", p.properties, 0);
    mh += '<div style="padding:3px 0 4px;display:flex;align-items:center;gap:4px">' +
      '<button class="btn sm" data-madd="' + esc(n + "|") + '">+ member</button>' +
      '<button class="del" data-pd="' + esc(n) + '"' +
      (locked ? ' disabled title="providerName is required for managed resources in Namespaced XRD" style="cursor:not-allowed;opacity:0.5;margin-left:auto"' : ' title="Delete parameter" style="margin-left:auto"') +
      '>&#215;</button></div>';
    return mh;
  }
  var h = '<div class="frow" style="margin-bottom:0;gap:4px">';
  if (p.type === "boolean") {
    h += '<select class="tsel" data-pdef="' + esc(n) + '" aria-label="Default value" style="flex:1">' +
      '<option value=""' + (!p.default ? " selected" : "") + ">no default</option>" +
      '<option' + (p.default === "true" ? " selected" : "") + ">true</option>" +
      '<option' + (p.default === "false" ? " selected" : "") + ">false</option></select>";
  } else if (p.enum && p.enum.length) {
    h += '<span class="g" style="flex:1;font-size:11px;overflow:hidden;text-overflow:ellipsis">enum: ' + esc(p.enum.join(", ")) + "</span>";
  } else {
    h += '<input class="tin" data-pdef="' + esc(n) + '" value="' + esc(p.default || "") +
      '" placeholder="default value" aria-label="Default value" style="flex:1;min-width:60px">';
  }
  if (p.type !== "boolean") {
    h += '<input class="tin" data-pe="' + esc(n) + '" value="' + esc((p.enum || []).join(",")) +
      '" placeholder="enum,values" title="Comma-separated allowed values" aria-label="Enum values" style="flex:1;min-width:60px">';
  }
  h += '<button class="del" data-pd="' + esc(n) + '"' +
    (locked ? ' disabled title="providerName is required for managed resources in Namespaced XRD" style="cursor:not-allowed;opacity:0.5"' : ' title="Delete parameter"') +
    '>&#215;</button></div>';
  return h;
}

export async function renderXRD() {
  var myToken = state.renderToken;
  var doc = state.store && state.store.state && state.store.state.doc;
  var xrd = (doc && doc.spec && doc.spec.xrd) || {};
  var params = paramsOf(doc);
  var currentKeys = Object.keys(params);

  var curDocName = doc && doc.metadata && doc.metadata.name;
  if (curDocName !== state.lastDocName) {
    state.paramOrder = null;
    state.lastDocName = curDocName;
  }

  if (!state.paramOrder) {
    state.paramOrder = currentKeys.slice().sort();
  } else {
    var missingInParams = state.paramOrder.filter(function (k) { return !Object.prototype.hasOwnProperty.call(params, k); });
    var missingInOrder = currentKeys.filter(function (k) { return state.paramOrder.indexOf(k) === -1; });
    if (missingInParams.length === 1 && missingInOrder.length === 1) {
      var idx = state.paramOrder.indexOf(missingInParams[0]);
      state.paramOrder[idx] = missingInOrder[0];
    } else {
      var kept = state.paramOrder.filter(function (k) { return Object.prototype.hasOwnProperty.call(params, k); });
      currentKeys.forEach(function (k) {
        if (kept.indexOf(k) === -1) kept.push(k);
      });
      state.paramOrder = kept;
    }
  }
  var names = state.paramOrder;

  var h = state.warnMsg
    ? '<div class="warnbar" style="border-bottom:1px solid var(--rule)">' + esc(state.warnMsg) + "</div>"
    : "";

  h += '<div class="insp-t"><div class="frow">' +
    '<input class="tin" id="xk" value="' + esc(xrd.kind) + '" aria-label="Kind">' +
    '<select class="tsel" id="xs" aria-label="Scope">' +
    '<option' + (xrd.scope === "Namespaced" ? " selected" : "") + ">Namespaced</option>" +
    '<option' + (xrd.scope === "Cluster" ? " selected" : "") + ">Cluster</option></select></div>" +
    '<div class="g">' + esc((xrd.plural || "") + "." + (xrd.group || "")) + " &#183; " + esc(xrd.version) + "</div></div>";

  var env = (doc && doc.spec && doc.spec.environment) || {};
  var envKeysCount = Object.keys(env).length;
  h += '<div class="env-summary-row" style="padding:7px 12px 6px;border-bottom:1px solid var(--rule);font-size:11px;display:flex;align-items:center;gap:6px">' +
    '<span style="color:var(--ink)">Environment: ' + envKeysCount + ' key' + (envKeysCount === 1 ? '' : 's') + ' declared</span>' +
    '<span class="sp" style="flex:1"></span>' +
    '<button class="btn sm link" data-tab-switch="shared" style="color:var(--shared);font-weight:600;text-decoration:underline;cursor:pointer;background:none;border:none;padding:0;font-size:11px">SHARED</button>' +
    '</div>' +
    '<div style="padding:7px 12px 3px"><span class="lbl">Parameters (' + names.length + ")</span></div>";

  names.forEach(function (n) {
    var p = params[n] || {};
    var fo = fanOut(doc, n);
    var locked = isParamLocked(doc, n);
    h += '<div class="fld"><div class="frow" style="margin-bottom:3px;gap:4px">' +
      '<input class="tin bold" data-pn="' + esc(n) + '" value="' + esc(n) + '" aria-label="Parameter name"' +
      (locked ? ' readonly title="providerName is required for managed resources in Namespaced XRD" style="min-width:70px;flex:1 1 auto;cursor:not-allowed;opacity:0.75"' : ' style="min-width:70px;flex:1 1 auto"') + '>' +
      '<select class="tsel" data-pt="' + esc(n) + '" aria-label="Parameter type" style="flex:0 0 auto"' +
      (locked ? ' disabled title="providerName type must be string" style="cursor:not-allowed"' : '') + '>' +
      PARAM_TYPES.map(function (t) {
        return "<option" + (t === p.type ? " selected" : "") + ">" + t + "</option>";
      }).join("") + "</select>" +
      '<label class="g" style="display:inline-flex;align-items:center;gap:3px;font-size:11px;flex:0 0 auto;white-space:nowrap"' +
      (locked ? ' title="providerName is required for managed resources in Namespaced XRD"' : '') + '>' +
      '<input type="checkbox" data-pr="' + esc(n) + '"' + (p.required ? " checked" : "") +
      (locked ? ' disabled style="cursor:not-allowed"' : '') + '>req</label>' +
      '<span class="fan" title="Wired into ' + fo + ' field' + (fo === 1 ? "" : "s") + '" style="flex:0 0 auto">&#215;' + fo + "</span></div>" +
      paramDetailRow(n, p, locked) + "</div>";
  });
  h += '<div style="padding:8px 12px 14px">' +
    '<button class="btn sm pri" id="addParamBtn">+ Add parameter</button></div>';

  /* ---------- pipeline steps ---------- */
  var pipeline = (doc && doc.spec && doc.spec.pipeline) || [];
  h += '<div style="padding:14px 12px 3px;border-top:1px solid var(--rule);display:flex;align-items:center">' +
    '<span class="lbl">Pipeline (' + (pipeline.length ? pipeline.length + " custom" : "default") + ')</span>' +
    '</div>' +
    '<div class="g" style="padding:2px 12px 8px;font-size:11px">Functions executed during Composition render. <code>render-resources</code> (go-templating) runs at center.</div>';

  if (!pipeline.length) {
    h += '<div style="margin:0 12px 10px;padding:8px 10px;background:var(--surface-2);border:1px solid var(--rule);border-radius:6px;font-size:11px">' +
      '<div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">' +
      '<span style="font-family:var(--mono);font-weight:600;color:var(--ink)">1. render-resources</span>' +
      '<span class="dg">go-templating (core)</span></div>' +
      '<div style="display:flex;align-items:center;gap:6px">' +
      '<span style="font-family:var(--mono);color:var(--ink)">2. auto-ready</span>' +
      '<span class="dg">inferred default</span>' +
      '<button class="btn sm" id="addAutoReadyBtn" style="margin-left:auto;font-size:10px">+ Pin step</button>' +
      '</div></div>';
  } else {
    for (var i = 0; i < pipeline.length; i++) {
      var step = pipeline[i];
      var pos = step.position || "after";
      var parsedInp = parseInputYAML(step.input || "");
      var apiVer = parsedInp.apiVersion;
      var fKind = parsedInp.kind;
      if (!apiVer || !fKind) {
        var inf = inferFnMeta(step);
        if (inf) {
          apiVer = inf.apiVersion;
          fKind = inf.kind;
        }
      }
      var flds = null;
      if (apiVer && fKind) {
        try {
          flds = await fieldsFor(apiVer, fKind);
        } catch (_) {
          flds = null;
        }
      }
      var hasSchema = flds && flds.fields && flds.fields.length > 0;
      var mode = state.pipeInputMode[i] || (hasSchema ? "form" : "raw");

      h += '<div class="fld" style="margin:0 12px 8px;padding:8px 10px;background:var(--surface-2);border:1px solid var(--rule);border-radius:6px">' +
        '<div class="frow" style="margin-bottom:4px">' +
        '<input class="tin bold" data-pipe-name="' + i + '" value="' + esc(step.name || "") + '" placeholder="step-name" aria-label="Step name" style="flex:1">' +
        '<select class="tsel" data-pipe-pos="' + i + '" aria-label="Position">' +
        '<option value="before"' + (pos === "before" ? " selected" : "") + '>before render</option>' +
        '<option value="after"' + (pos === "after" ? " selected" : "") + '>after render</option></select>' +
        '<button class="del" data-pipe-del="' + i + '" title="Delete pipeline step">&#215;</button></div>' +
        '<div class="frow" style="margin-bottom:4px">' +
        '<input class="tin" data-pipe-fn="' + i + '" value="' + esc(step.functionRef || "") + '" placeholder="functionRef (e.g. function-auto-ready)" aria-label="Function ref">' +
        '</div>' +
        '<div class="frow" style="margin-bottom:4px">' +
        '<input class="tin" list="catalogueFnPkgs" data-pipe-pkg="' + i + '" value="' + esc(step.package || "") + '" placeholder="xpkg.crossplane.io/... (package)" aria-label="Function package">' +
        '</div>' +
        '<div style="margin-top:4px">';

      if (hasSchema && mode === "form") {
        h += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px">' +
          '<span class="dg" style="font-size:10px;font-weight:600">Input (' + esc(fKind) + '):</span>' +
          '<button class="btn sm" data-pipe-mode="' + i + '" data-mode-to="raw" style="font-size:9px;padding:1px 5px">Raw YAML</button>' +
          '</div>';
        flds.fields.forEach(function (f) {
          var curVal = getPathVal(parsedInp, f.path);
          h += '<div style="margin-bottom:4px;padding:3px 0">' +
            '<div style="display:flex;align-items:center;justify-content:space-between;font-size:11px">' +
            '<span><code style="font-family:var(--mono);color:var(--ink)">' + esc(f.path) + '</code>' +
            (f.required ? '<span class="rq" style="margin-left:4px;font-size:9px">req</span>' : "") + '</span>' +
            '<span style="color:var(--faint);font-size:10px">' + esc(f.type) + '</span></div>' +
            (f.description ? '<div class="g" style="font-size:10px;margin:1px 0 2px">' + esc(f.description) + '</div>' : "");
          if (f.type === "boolean") {
            h += '<select class="tsel" data-pipe-fld="' + i + '" data-fld-path="' + esc(f.path) + '">' +
              '<option value=""' + (!curVal ? " selected" : "") + '>unset</option>' +
              '<option value="true"' + (curVal === "true" ? " selected" : "") + '>true</option>' +
              '<option value="false"' + (curVal === "false" ? " selected" : "") + '>false</option></select>';
          } else if (f.type === "string" && (f.path.indexOf("template") !== -1 || f.path.indexOf("script") !== -1)) {
            h += '<textarea class="tin" data-pipe-fld="' + i + '" data-fld-path="' + esc(f.path) + '" rows="2" style="font-family:var(--mono);font-size:10px;width:100%">' + esc(curVal) + '</textarea>';
          } else {
            h += '<input class="tin" data-pipe-fld="' + i + '" data-fld-path="' + esc(f.path) + '" value="' + esc(curVal) + '" placeholder="' + esc(f.type) + '">';
          }
          h += '</div>';
        });
      } else {
        h += '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:2px">' +
          '<span class="dg" style="font-size:10px">Input YAML' + (hasSchema ? "" : " (uncached)") + ':</span>' +
          (hasSchema ? '<button class="btn sm" data-pipe-mode="' + i + '" data-mode-to="form" style="font-size:9px;padding:1px 5px">Typed Form</button>' : "") +
          '</div>' +
          '<textarea class="tin" data-pipe-input="' + i + '" rows="3" style="font-family:var(--mono);font-size:10px;width:100%;resize:vertical" placeholder="apiVersion: ...\nkind: ...">' + esc(step.input || "") + '</textarea>';
      }

      h += '</div></div>';
    }
  }

  var catFns = await getCatalogueFunctions().catch(function () { return []; });
  h += '<datalist id="catalogueFnPkgs">';
  catFns.forEach(function (cf) {
    if (cf.ref) {
      h += '<option value="' + esc(cf.ref) + '">' + esc(cf.name || "") + '</option>';
    }
  });
  h += '</datalist>';

  h += '<div style="padding:4px 12px 14px;display:flex;gap:6px;flex-wrap:wrap">' +
    '<select class="tsel" id="pipePresetSelect" style="flex:1">' +
    '<option value="auto-ready">+ function-auto-ready</option>' +
    '<option value="environment-configs">+ function-environment-configs</option>' +
    '<option value="cel-filter">+ function-cel-filter</option>' +
    '<option value="extra-resources">+ function-extra-resources</option>' +
    '<option value="custom">+ Custom function step</option>' +
    '</select>' +
    '<button class="btn sm pri" id="addPipeStepBtn">+ Add step</button>' +
    '</div>';

  if (myToken !== state.renderToken) return;
  var box = state.box || document.querySelector("#insp");
  if (!box) return;

  var snap = state.snapshotFocusedEdit ? state.snapshotFocusedEdit() : null;
  box.innerHTML = h;
  if (state.restoreFocusedEdit) state.restoreFocusedEdit(snap);
  state.pendingRenamedParam = null;
  if (state.pendingFocusParam) {
    var pInp = box.querySelector('input[data-pn="' + CSS.escape(state.pendingFocusParam) + '"]');
    if (pInp) {
      pInp.focus();
      if (pInp.select) pInp.select();
      state.pendingFocusParam = null;
    }
  }
}
