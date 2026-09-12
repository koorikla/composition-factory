/**
 * Submodule: inspector/events.js
 * DOM event listeners, action dispatch maps, and mutation commits.
 */

import { fanOut } from "../../wires.js";
import { state } from "./state.js";
import { insertSnippetIntoTextarea, triggerExpressionPreview } from "./preview.js";
import {
  paramsOf, isParamLocked, cleanParamRefs, cleanMemberRefs, renameMemberRefs,
  cloneProps, memberParent, memberContainer, commitMembers, paramFrom, parseInputYAML,
  serializeInputYAML, setPathVal, inferFnMeta
} from "./xrd.js";

export async function commitValue(path, kind, text) {
  var res = state.selectedResource();
  if (!res) return;
  var entry = state.entryOf(res, path);
  if (entry && entry.from && kind === "value") {
    if (!confirm('This field is wired from "' + entry.from + '". Overwrite the wire with a literal value?')) {
      state.render();
      return;
    }
  }
  var ok;
  if (text === "") {
    ok = await state.setField(path, null);
  } else {
    var form = { value: "", from: "", raw: "" };
    form[kind] = text;
    ok = await state.setField(path, form);
  }
  if (ok !== null) delete state.uiMode[path];
}

export async function commitEnvelopeValue(path, kind, text) {
  var res = state.selectedResource();
  if (!res) return;
  var entry = state.envelopeEntryOf(res, path);
  var isWired = entry && (entry.from || entry.raw === "{{ $xr }}");
  if (isWired && kind === "value") {
    var wireSrc = entry.from || "XR name ($xr)";
    if (!confirm('This envelope field is wired from "' + wireSrc + '". Overwrite the wire with a literal value?')) {
      state.render();
      return;
    }
  }
  var ok;
  if (text === "") {
    ok = await state.setEnvelopeField(path, null);
  } else {
    var form = { value: "", from: "", raw: "" };
    form[kind] = text;
    ok = await state.setEnvelopeField(path, form);
  }
  if (ok !== null) delete state.uiMode["env:" + path];
}

export var boxClickActions = [
  {
    selector: "[data-quick-snippet], [data-env-quick-snippet]",
    run: function (chip) {
      var isEnv = chip.hasAttribute("data-env-quick-snippet");
      var path = chip.getAttribute(isEnv ? "data-env-quick-snippet" : "data-quick-snippet");
      var snippet = chip.getAttribute("data-snippet-val");
      if (!snippet) return;
      var taSelector = isEnv ? ('textarea[data-env-raw="' + CSS.escape(path) + '"]') : ('textarea[data-raw="' + CSS.escape(path) + '"]');
      var box = state.box || document.querySelector("#insp");
      var ta = box ? box.querySelector(taSelector) : null;
      if (ta) {
        insertSnippetIntoTextarea(ta, snippet);
      }
    }
  },
  {
    selector: "[data-toggle-desc]",
    run: function (el) {
      var descWrap = el.closest(".fld-d");
      if (descWrap) {
        var exp = descWrap.classList.toggle("expanded");
        el.textContent = exp ? "less" : "more";
      }
    }
  },
  {
    selector: "[data-ann-del]",
    run: function (el) {
      var adk = el.getAttribute("data-ann-del");
      var selRes = state.selectedResource();
      if (!selRes) return;
      state.store.replaceDoc(function (d) {
        var r = d.spec.resources.find(function (x) { return x.name === selRes.name; });
        if (r && r.annotations) { delete r.annotations[adk]; if (!Object.keys(r.annotations).length) delete r.annotations; }
      });
    }
  },
  {
    selector: "[data-ann-add]",
    run: function () {
      var box = state.box || document.querySelector("#insp");
      var keyEl = box.querySelector("[data-ann-key]");
      var valEl = box.querySelector("[data-ann-value]");
      var selRes2 = state.selectedResource();
      if (!selRes2 || !keyEl) return;
      var annKey = keyEl.value.trim();
      if (!annKey) {
        var kMsg = "Annotation key is required (e.g. prefix/name)";
        state.warnMsg = kMsg;
        state.store.emit("error", { message: kMsg });
        state.render();
        var kEl = box.querySelector("[data-ann-key]");
        if (kEl) kEl.focus();
        return;
      }
      var annVal = valEl ? valEl.value.trim() : "";
      if (!annVal) {
        state.annDraftKey = annKey;
        var vMsg = 'Annotation "' + annKey + '" requires a value (or a temporary placeholder to wire later)';
        state.warnMsg = vMsg;
        state.store.emit("error", { message: vMsg });
        state.render();
        var vEl = box.querySelector("[data-ann-value]");
        if (vEl) vEl.focus();
        return;
      }
      state.annDraftKey = annKey;
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = d.spec.resources.find(function (x) { return x.name === selRes2.name; });
          if (!r) return;
          r.annotations = r.annotations || {};
          r.annotations[annKey] = { value: annVal };
        });
      }).then(function (res) {
        if (res !== null) {
          state.annDraftKey = "";
          state.render();
        }
      });
    }
  },
  {
    selector: "button[data-m]",
    needsDoc: true,
    run: function (mb) {
      var isEnv = mb.hasAttribute("data-env");
      var path = mb.getAttribute("data-path");
      var m = mb.getAttribute("data-m");
      var res = state.selectedResource();
      var entry = isEnv ? (res ? state.envelopeEntryOf(res, path) : null) : (res ? state.entryOf(res, path) : null);
      var isWired = entry && (entry.from || (isEnv && entry.raw === "{{ $xr }}"));
      if (isWired && (m === "v" || m === "r")) {
        var wireSrc = entry.from || "XR name ($xr)";
        if (!confirm('This field is wired from "' + wireSrc + '". Switch modes and overwrite the wire?')) return;
      }
      var mKey = isEnv ? ("env:" + path) : path;
      state.uiMode[mKey] = m;
      if (m !== "w" && state.pendingNewParam === mKey) state.pendingNewParam = null;
      state.render();
    }
  },
  {
    selector: "[data-unwire]",
    needsDoc: true,
    run: function (un) {
      var p1 = un.getAttribute("data-unwire");
      state.setField(p1, null).then(function (r) { if (r !== null) delete state.uiMode[p1]; });
    }
  },
  {
    selector: "[data-env-unwire]",
    needsDoc: true,
    run: function (unEnv) {
      var pe = unEnv.getAttribute("data-env-unwire");
      state.setEnvelopeField(pe, null).then(function (r) { if (r !== null) delete state.uiMode["env:" + pe]; });
    }
  },
  {
    selector: "[data-add-map-entry]",
    needsDoc: true,
    run: function (addMap) {
      state.pendingNewMapEntry = addMap.getAttribute("data-add-map-entry");
      state.render();
    }
  },
  {
    selector: "[data-new-map-ok]",
    needsDoc: true,
    run: function (mapOk) {
      var mapPath = mapOk.getAttribute("data-new-map-ok");
      var box = state.box || document.querySelector("#insp");
      var keyInp = box.querySelector('[data-new-map-key="' + CSS.escape(mapPath) + '"]');
      var valInp = box.querySelector('[data-new-map-val="' + CSS.escape(mapPath) + '"]');
      var keyVal = keyInp && keyInp.value.trim();
      if (!keyVal) return;
      var initVal = (valInp && valInp.value.trim()) || "default";
      var fullPath = mapPath + "[" + keyVal + "]";
      state.setField(fullPath, { value: initVal }).then(function (r) {
        if (r !== null) {
          state.pendingNewMapEntry = null;
        }
      });
    }
  },
  {
    selector: "[data-new-map-cancel]",
    needsDoc: true,
    run: function () {
      state.pendingNewMapEntry = null;
      state.render();
    }
  },
  {
    selector: "[data-del-map-entry]",
    needsDoc: true,
    run: function (delMap) {
      var delPath = delMap.getAttribute("data-del-map-entry");
      state.setField(delPath, null).then(function (r) {
        if (r !== null) delete state.uiMode[delPath];
      });
    }
  },
  {
    selector: "[data-npok]",
    needsDoc: true,
    run: function (ok) {
      var p2 = ok.getAttribute("data-npok");
      var isEnv = p2.indexOf("env:") === 0;
      var realPath = isEnv ? p2.slice(4) : p2;
      var box = state.box || document.querySelector("#insp");
      var nameEl = box.querySelector('[data-npname="' + CSS.escape(p2) + '"]');
      var typeEl = box.querySelector('[data-nptype="' + CSS.escape(p2) + '"]');
      var reqEl = box.querySelector('input[type="checkbox"][data-npreq="' + CSS.escape(p2) + '"]');
      var isReq = reqEl ? reqEl.checked : (ok.getAttribute("data-npreq") === "true");
      var name = nameEl && nameEl.value.trim();
      var type = (typeEl && typeEl.value) || "string";
      if (!name) return;
      state.op(function () { return state.store.addParameter(name, { type: type, required: isReq }); })
        .then(function (docAfter) {
          if (docAfter === null) return null;
          if (isEnv) {
            return state.setEnvelopeField(realPath, { from: "params." + name, value: "", raw: "" });
          }
          return state.setField(realPath, { from: "params." + name, value: "", raw: "" });
        })
        .then(function (r) {
          if (r !== null) { state.pendingNewParam = null; delete state.uiMode[p2]; }
        });
    }
  },
  {
    selector: "[data-pipe-mode]",
    needsDoc: true,
    run: function (btn) {
      var pidx = parseInt(btn.getAttribute("data-pipe-mode"), 10);
      var target = btn.getAttribute("data-mode-to");
      state.pipeInputMode[pidx] = target;
      state.render();
    }
  },
  {
    selector: "[data-npcancel]",
    needsDoc: true,
    run: function () {
      state.pendingNewParam = null;
      state.render();
    }
  },
  {
    selector: "[data-madd]",
    needsDoc: true,
    run: function (madd, doc) {
      var maddKey = madd.getAttribute("data-madd").split("|");
      var maddParam = maddKey[0], maddPath = maddKey[1];
      var maddProps = cloneProps((paramsOf(doc)[maddParam] || {}).properties);
      var cont = memberContainer(maddProps, maddPath);
      if (!cont) return;
      var mBase = "member", mNm = mBase + "1", mI = 2;
      while (cont[mNm]) { mNm = mBase + mI; mI++; }
      cont[mNm] = { type: "string" };
      state.op(function () { return commitMembers(maddParam, maddProps); })
        .then(function (r) { if (r === null) state.render(); });
    }
  },
  {
    selector: "[data-mdel]",
    needsDoc: true,
    run: function (mdel, doc) {
      var mdelKey = mdel.getAttribute("data-mdel").split("|");
      var paramName = mdelKey[0];
      var memberPath = mdelKey[1];
      var fo = fanOut(doc, paramName + "." + memberPath);
      if (fo > 0) {
        if (!confirm('Member "' + paramName + '.' + memberPath + '" is wired into ' + fo + " field" + (fo === 1 ? "" : "s") + ". Delete it and unwire all referencing fields?")) {
          return;
        }
        var draft = JSON.parse(JSON.stringify(doc));
        cleanMemberRefs(draft, paramName, memberPath);
        state.op(function () { return state.store.replaceDoc(draft); });
        return;
      }
      var mdelProps = cloneProps((paramsOf(doc)[paramName] || {}).properties);
      var mdelLoc = memberParent(mdelProps, memberPath);
      if (!mdelLoc) return;
      delete mdelLoc.parent[mdelLoc.key];
      state.op(function () { return commitMembers(paramName, mdelProps); })
        .then(function (r) { if (r === null) state.render(); });
    }
  },
  {
    selector: "[data-pd]",
    needsDoc: true,
    run: function (pd, doc) {
      if (pd.hasAttribute("disabled")) return;
      var pn = pd.getAttribute("data-pd");
      if (isParamLocked(doc, pn)) return;
      var fo = fanOut(doc, pn);
      if (fo > 0 && !confirm('Parameter "' + pn + '" is wired into ' + fo + " field" + (fo === 1 ? "" : "s") + ". Delete it and unwire all referencing fields?")) return;
      if (state.paramOrder) {
        var idx = state.paramOrder.indexOf(pn);
        if (idx !== -1) state.paramOrder.splice(idx, 1);
      }
      if (fo > 0) {
        var draft = JSON.parse(JSON.stringify(doc));
        cleanParamRefs(draft, pn);
        state.op(function () { return state.store.replaceDoc(draft); });
      } else {
        state.op(function () { return state.store.deleteParameter(pn); });
      }
    }
  },
  {
    selector: "#addParamBtn",
    needsDoc: true,
    run: function (_, doc) {
      var params = paramsOf(doc);
      var base = "newParam", nm = base, i = 2;
      while (params[nm]) { nm = base + i; i++; }
      state.pendingFocusParam = nm;
      if (state.paramOrder && state.paramOrder.indexOf(nm) === -1) {
        state.paramOrder.push(nm);
      }
      state.op(function () { return state.store.addParameter(nm, { type: "string", required: false }); })
        .then(function (res) {
          if (res === null) {
            state.pendingFocusParam = null;
            return;
          }
          var box = state.box || document.querySelector("#insp");
          var inp = box ? box.querySelector('input[data-pn="' + CSS.escape(nm) + '"]') : null;
          if (inp) {
            inp.focus();
            if (inp.select) inp.select();
            state.pendingFocusParam = null;
          }
        });
    }
  },
  {
    selector: "#addAutoReadyBtn",
    needsDoc: true,
    run: function () {
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          d.spec.pipeline = d.spec.pipeline || [];
          d.spec.pipeline.push({
            name: "auto-ready",
            functionRef: "function-auto-ready",
            package: "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
            position: "after"
          });
        });
      });
    }
  },
  {
    selector: "#addPipeStepBtn",
    needsDoc: true,
    run: function () {
      var box = state.box || document.querySelector("#insp");
      var preset = (box.querySelector("#pipePresetSelect") && box.querySelector("#pipePresetSelect").value) || "custom";
      var pipePresetMap = {
        "auto-ready": {
          name: "auto-ready",
          functionRef: "function-auto-ready",
          package: "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0",
          position: "after"
        },
        "environment-configs": {
          name: "environment-configs",
          functionRef: "function-environment-configs",
          package: "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0",
          position: "before",
          input: "apiVersion: environmentconfigs.fn.crossplane.io/v1beta1\nkind: Input\nspec:\n  environmentConfigs:\n  - type: Reference\n    ref:\n      name: default"
        },
        "cel-filter": {
          name: "cel-filter",
          functionRef: "function-cel-filter",
          package: "xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.2.0",
          position: "after"
        },
        "extra-resources": {
          name: "extra-resources",
          functionRef: "function-extra-resources",
          package: "xpkg.crossplane.io/crossplane-contrib/function-extra-resources:v0.3.0",
          position: "before"
        },
        "custom": {
          name: "custom-step",
          functionRef: "function-custom",
          package: "",
          position: "after"
        }
      };
      var newStep = Object.assign({}, pipePresetMap[preset] || pipePresetMap.custom);
      if (preset === "custom") {
        var pkg = window.prompt("Function package reference (e.g. xpkg.crossplane.io/...):");
        if (!pkg || !pkg.trim()) return;
        newStep.package = pkg.trim();
      }
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          d.spec.pipeline = d.spec.pipeline || [];
          d.spec.pipeline.push(newStep);
        });
      });
    }
  },
  {
    selector: "[data-pipe-del]",
    needsDoc: true,
    run: function (pipeDel) {
      var pidx = parseInt(pipeDel.getAttribute("data-pipe-del"), 10);
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          if (!d.spec.pipeline) return;
          d.spec.pipeline.splice(pidx, 1);
          if (!d.spec.pipeline.length) delete d.spec.pipeline;
        });
      });
    }
  },
  {
    selector: "[data-wl-sync-app]",
    needsDoc: true,
    run: function (syncBtn) {
      var rname = syncBtn.getAttribute("data-wl-sync-app");
      var box = state.box || document.querySelector("#insp");
      var appInp = box.querySelector('[data-wl-app="' + rname + '"]');
      var val = appInp ? appInp.value.trim() : "";
      if (!val) val = rname;
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname; });
          if (!r) return;
          r.fields = r.fields || {};
          delete r.fields["spec.selector.matchLabels"];
          delete r.fields["spec.template.metadata.labels"];
          delete r.fields["spec.selector.matchLabels.app"];
          delete r.fields["spec.template.metadata.labels.app"];
          r.fields["spec.selector.matchLabels[app]"] = { value: val };
          r.fields["spec.template.metadata.labels[app]"] = { value: val };
          if (!r.fields["spec.template.spec.containers[0].name"]) {
            r.fields["spec.template.spec.containers[0].name"] = { value: rname };
          }
        });
      });
    }
  },
  {
    selector: "[data-svc-match-wl]",
    needsDoc: true,
    run: function (svcMatchBtn) {
      var rname2 = svcMatchBtn.getAttribute("data-svc-match-wl");
      var matchApp = svcMatchBtn.getAttribute("data-match-app") || "";
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname2; });
          if (!r) return;
          r.fields = r.fields || {};
          delete r.fields["spec.selector"];
          delete r.fields["spec.selector.app"];
          r.fields["spec.selector[app]"] = { value: matchApp };
          r.fields["spec.ports[0].port"] = { raw: "8080" };
        });
      });
    }
  },
  {
    selector: "[data-apply-std-labels]",
    needsDoc: true,
    run: function (stdLblBtn) {
      var rname3 = stdLblBtn.getAttribute("data-apply-std-labels");
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname3; });
          if (!r) return;
          r.annotations = r.annotations || {};
          r.annotations["app.kubernetes.io/managed-by"] = { value: "crossplane" };
          r.annotations["app.kubernetes.io/name"] = { raw: "'{{ $xr }}'" };
          r.annotations["app.kubernetes.io/instance"] = { raw: "'{{ $xr }}'" };
        });
      });
    }
  },
  {
    selector: "[data-apply-ext-name]",
    needsDoc: true,
    run: function (extNameBtn) {
      var rname4 = extNameBtn.getAttribute("data-apply-ext-name");
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rname4; });
          if (!r) return;
          r.annotations = r.annotations || {};
          r.annotations["crossplane.io/external-name"] = { raw: "'{{ $xr }}-" + rname4 + "'" };
        });
      });
    }
  },
  {
    selector: "[data-tab-switch]",
    run: function (el) {
      var target = el.getAttribute("data-tab-switch");
      if (target === "sources") target = "src";
      var btn = document.querySelector('#rtabs button[data-r="' + target + '"]');
      if (btn) btn.click();
    }
  },
  {
    selector: "button[data-env-wire-del]",
    needsDoc: true,
    run: function (btn) {
      var resName = btn.getAttribute("data-wire-res");
      var wirePath = btn.getAttribute("data-wire-path");
      var isEnv = btn.hasAttribute("data-wire-env");
      var isAnn = btn.hasAttribute("data-wire-ann");
      state.removeWire(resName, wirePath, isEnv, isAnn);
    }
  },
  {
    selector: "button[data-env-del-key]",
    needsDoc: true,
    run: function (btn) {
      var keyName = btn.getAttribute("data-env-del-key");
      state.deleteEnvKey(keyName);
    }
  },
  {
    selector: "#envAddKeyBtn",
    needsDoc: true,
    run: function (_, doc) {
      var env = (doc && doc.spec && doc.spec.environment) || {};
      var base = "key", nm = base + "1", i = 2;
      while (env[nm]) { nm = base + i; i++; }
      state.pendingFocusEnvKey = nm;
      state.addEnvKey(nm, { type: "string" });
    }
  }
];

export function onBoxClick(e) {
  for (var i = 0; i < boxClickActions.length; i++) {
    var item = boxClickActions[i];
    var el = e.target.closest(item.selector);
    if (el) {
      if (item.needsDoc) {
        var d = state.store && state.store.state && state.store.state.doc;
        if (!d) return;
        item.run(el, d);
      } else {
        item.run(el);
      }
      return;
    }
  }
}

export var wlSimpleFieldMap = {
  "data-wl-replicas": "spec.replicas",
  "data-wl-image": "spec.template.spec.containers[0].image",
  "data-wl-cname": "spec.template.spec.containers[0].name",
  "data-wl-cport": "spec.template.spec.containers[0].ports[0].containerPort",
  "data-svc-app": "spec.selector[app]",
  "data-svc-port": "spec.ports[0].port",
  "data-svc-tgtport": "spec.ports[0].targetPort",
  "data-svc-type": "spec.type"
};

export var directCommitMap = {
  "data-v": function (t) { commitValue(t.getAttribute("data-v"), "value", t.value); },
  "data-raw": function (t) { commitValue(t.getAttribute("data-raw"), "raw", t.value); },
  "data-env-v": function (t) { commitEnvelopeValue(t.getAttribute("data-env-v"), "value", t.value); },
  "data-env-raw": function (t) { commitEnvelopeValue(t.getAttribute("data-env-raw"), "raw", t.value); }
};

export var paramFieldUpdaters = {
  "data-pt": function (t) { return { type: t.value }; },
  "data-pr": function (t) { return { required: t.checked }; },
  "data-pdef": function (t) { return { default: t.value }; },
  "data-pe": function (t) {
    var vals = t.value.split(",").map(function (s) { return s.trim(); }).filter(Boolean);
    return { enum: vals.length ? vals : null };
  }
};

export function inferPlural(kind) {
  var lower = (kind || "").toLowerCase();
  if (!lower) return "";
  if (lower.endsWith("s") || lower.endsWith("x") || lower.endsWith("z") || lower.endsWith("ch") || lower.endsWith("sh")) {
    return lower + "es";
  }
  if (lower.endsWith("y") && lower.length > 1) {
    var c = lower.charAt(lower.length - 2);
    if ("aeiou".indexOf(c) === -1) {
      return lower.slice(0, -1) + "ies";
    }
  }
  return lower + "s";
}

export var xrdFieldUpdaters = {
  xk: function (t) {
    var kv = t.value.trim();
    if (!kv) { state.render(); return; }
    state.op(function () {
      return state.store.replaceDoc(function (d) {
        d.spec.xrd.kind = kv;
        d.spec.xrd.plural = inferPlural(kv);
      });
    }).then(function (r) { if (r === null) state.render(); });
  },
  xs: function (t) {
    var sv = t.value;
    state.op(function () {
      return state.store.replaceDoc(function (d) { d.spec.xrd.scope = sv; });
    }).then(function (r) { if (r === null) state.render(); });
  }
};

export var pipeAttrs = {
  "data-pipe-name": "name",
  "data-pipe-pos": "position",
  "data-pipe-fn": "functionRef",
  "data-pipe-pkg": "package",
  "data-pipe-input": "input"
};

function isOptParamWire(fromVal, params) {
  if (!fromVal || typeof fromVal !== "string") return false;
  if (fromVal.indexOf("params.") !== 0) return false;
  var pn = fromVal.slice(7);
  var parts = pn.split(".");
  var cur = params && params[parts[0]];
  if (!cur) return false;
  for (var i = 1; i < parts.length; i++) {
    if (!cur.properties || !cur.properties[parts[i]]) return false;
    cur = cur.properties[parts[i]];
  }
  return !cur.required;
}

function whenFromControls(rootEl, rn) {
  var pSel = rootEl.querySelector('[data-when-param="' + CSS.escape(rn) + '"]');
  var p = pSel && pSel.value;
  if (!p) return null;
  var doc = state.store && state.store.state && state.store.state.doc;
  var params = paramsOf(doc);
  var env = (doc && doc.spec && doc.spec.environment) || {};

  var isEnv = false;
  var key = p;
  if (p.indexOf("env.") === 0) {
    isEnv = true;
    key = p.slice(4);
  } else if (p.indexOf("params.") === 0) {
    isEnv = false;
    key = p.slice(7);
  } else if (env[p] && !params[p]) {
    isEnv = true;
    key = p;
  }

  var prefix = isEnv ? "env." : "params.";
  var decl = isEnv ? (env[key] || {}) : (params[key] || {});

  if (decl.type === "boolean") return prefix + key;
  var opEl = rootEl.querySelector('[data-when-op="' + CSS.escape(rn) + '"]');
  var valEl = rootEl.querySelector('[data-when-val="' + CSS.escape(rn) + '"]');
  var op = opEl ? opEl.value : "==";
  var fallbackVal = (decl.enum && decl.enum[0]) || (decl.default !== undefined ? String(decl.default) : "");
  var val = valEl ? valEl.value : fallbackVal;
  return prefix + key + " " + op + ' "' + val + '"';
}

export function onBoxChange(e) {
  var t = e.target;
  if (!t) return;
  var box = state.box || document.querySelector("#insp");
  var doc = state.store && state.store.state && state.store.state.doc;
  if (t.matches("#envSelName, input[data-env-selection-name]")) {
    var envSelNameMode = (box.querySelector("#envSelMode") && box.querySelector("#envSelMode").value) || "Reference";
    var envSelNameVal = t.value.trim() || "default";
    state.updateEnvSelection(envSelNameMode, envSelNameVal, "");
    return;
  }
  if (t.matches("#envSelLabels, input[data-env-selection-labels]")) {
    var envSelLblMode = (box.querySelector("#envSelMode") && box.querySelector("#envSelMode").value) || "Selector";
    var envSelLblVal = t.value.trim();
    state.updateEnvSelection(envSelLblMode, "", envSelLblVal);
    return;
  }
  if (t.matches("#envSelMode")) {
    var envChangeMode = t.value;
    var nameInp = box.querySelector("#envSelName");
    var lblInp = box.querySelector("#envSelLabels");
    var curName = nameInp ? nameInp.value.trim() : "default";
    var curLbl = lblInp ? lblInp.value.trim() : "environment=default";
    state.updateEnvSelection(envChangeMode, curName, curLbl);
    return;
  }
  if (t.matches("input[data-env-name]")) {
    var oldKey = t.getAttribute("data-env-name");
    var newKey = t.value.trim();
    if (!newKey || newKey === oldKey) {
      t.value = oldKey;
      return;
    }
    state.renameEnvKey(oldKey, newKey, t);
    return;
  }
  if (t.matches("select[data-env-type]")) {
    var envTypeKey = t.getAttribute("data-env-type");
    state.setEnvKeyField(envTypeKey, "type", t.value);
    return;
  }
  if (t.matches("input[data-env-req]")) {
    var envReqKey = t.getAttribute("data-env-req");
    state.setEnvKeyField(envReqKey, "required", t.checked);
    return;
  }
  if (t.matches("input[data-env-val]")) {
    var envValKey = t.getAttribute("data-env-val");
    state.setEnvKeyField(envValKey, "value", t.value.trim());
    return;
  }
  if (t.matches("select[data-insert-snippet], select[data-env-insert-snippet]")) {
    var isEnv = t.hasAttribute("data-env-insert-snippet");
    var path = t.getAttribute(isEnv ? "data-env-insert-snippet" : "data-insert-snippet");
    var snippet = t.value;
    t.value = "";
    if (!snippet) return;
    var taSelector = isEnv ? ('textarea[data-env-raw="' + CSS.escape(path) + '"]') : ('textarea[data-raw="' + CSS.escape(path) + '"]');
    var ta = box.querySelector(taSelector);
    if (ta) {
      insertSnippetIntoTextarea(ta, snippet);
    }
    return;
  }
  if (t.matches("[data-when-param],[data-when-op],[data-when-val]")) {
    var wrn = t.getAttribute("data-when-param") ||
      t.getAttribute("data-when-op") || t.getAttribute("data-when-val");
    var expr = whenFromControls(box, wrn);
    state.store.replaceDoc(function (d) {
      var r = d.spec.resources.find(function (x) { return x.name === wrn; });
      if (!r) return;
      if (expr) {
        r.when = expr;
      } else {
        var isExplicitAlways = t.matches("[data-when-param]") && !t.value;
        var isEnvActive = r.when && /^(?:env|\$env)\./.test(r.when.trim());
        if (isExplicitAlways || !isEnvActive) {
          delete r.when;
        }
      }
    });
    return;
  }
  if (t.matches("select[data-foreach]")) {
    var rn = t.getAttribute("data-foreach");
    var val = t.value;
    state.store.replaceDoc(function (d) {
      var r = d.spec.resources.find(function (x) { return x.name === rn; });
      if (!r) return;
      if (val) r.forEach = val; else delete r.forEach;
    });
    return;
  }
  if (t.hasAttribute("data-wl-app")) {
    var wlAppRname = t.getAttribute("data-wl-app");
    var wlAppVal = t.value.trim();
    if (wlAppVal) {
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === wlAppRname; });
          if (!r) return;
          r.fields = r.fields || {};
          delete r.fields["spec.selector.matchLabels"];
          delete r.fields["spec.template.metadata.labels"];
          delete r.fields["spec.selector.matchLabels.app"];
          delete r.fields["spec.template.metadata.labels.app"];
          r.fields["spec.selector.matchLabels[app]"] = { value: wlAppVal };
          r.fields["spec.template.metadata.labels[app]"] = { value: wlAppVal };
        });
      });
    }
    return;
  }

  for (var wlAttr in wlSimpleFieldMap) {
    if (t.hasAttribute(wlAttr)) {
      var wlRname = t.getAttribute(wlAttr);
      var wlPath = wlSimpleFieldMap[wlAttr];
      var wlVal = t.value.trim();
      (function (rName, fPath, fVal) {
        state.op(function () {
          return state.store.replaceDoc(function (d) {
            var r = (d.spec && d.spec.resources || []).find(function (x) { return x.name === rName; });
            if (!r) return;
            r.fields = r.fields || {};
            if (fVal) r.fields[fPath] = { value: fVal };
            else delete r.fields[fPath];
            // A blueprint from disk may still carry the whole selector as one
            // raw/dotted entry; leaving it beside the [app] entry would emit
            // both, so the map entry supersedes it.
            if (fPath === "spec.selector[app]") {
              delete r.fields["spec.selector"];
              delete r.fields["spec.selector.app"];
            }
          });
        });
      })(wlRname, wlPath, wlVal);
      return;
    }
  }

  for (var directAttr in directCommitMap) {
    if (t.hasAttribute(directAttr)) {
      directCommitMap[directAttr](t);
      return;
    }
  }

  if (t.hasAttribute("data-wire")) {
    if (t.dataset.keyNav === "true") return;
    const path = t.getAttribute("data-wire");
    const v = t.value;
    if (v === "__new__") { state.pendingNewParam = path; state.render(); return; }
    if (!v) return;
    const fromVal = (v.indexOf("params.") === 0 || v.indexOf("resources.") === 0 || v.indexOf("env.") === 0) ? v : ("params." + v);
    if (t.getAttribute("data-fld-req") === "true" && isOptParamWire(fromVal, paramsOf(doc))) {
      const parentCard = t.closest(".bound");
      const existingWarn = parentCard && parentCard.parentElement && parentCard.parentElement.querySelector(".wire-warn");
      if (!existingWarn && parentCard) {
        const warnDiv = document.createElement("div");
        warnDiv.style.marginTop = "2px";
        warnDiv.innerHTML = '<span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span>';
        parentCard.after(warnDiv);
      }
    }
    state.setField(path, { from: fromVal, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete state.uiMode[path]; state.pendingNewParam = null; } });
    return;
  }

  if (t.hasAttribute("data-env-wire")) {
    if (t.dataset.keyNav === "true") return;
    const path = t.getAttribute("data-env-wire");
    const v = t.value;
    if (v === "__new__") { state.pendingNewParam = "env:" + path; state.render(); return; }
    if (!v) return;
    if (v === "$xr") {
      state.setEnvelopeField(path, { from: "", value: "", raw: "{{ $xr }}" })
        .then(function (r) { if (r !== null) { delete state.uiMode["env:" + path]; state.pendingNewParam = null; } });
      return;
    }
    const fromVal = (v.indexOf("params.") === 0 || v.indexOf("resources.") === 0 || v.indexOf("env.") === 0) ? v : ("params." + v);
    if (t.getAttribute("data-fld-req") === "true" && isOptParamWire(fromVal, paramsOf(doc))) {
      const parentCard = t.closest(".bound");
      const existingWarn = parentCard && parentCard.parentElement && parentCard.parentElement.querySelector(".wire-warn");
      if (!existingWarn && parentCard) {
        const warnDiv = document.createElement("div");
        warnDiv.style.marginTop = "2px";
        warnDiv.innerHTML = '<span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if missing">&#9888; optional param into required field</span>';
        parentCard.after(warnDiv);
      }
    }
    state.setEnvelopeField(path, { from: fromVal, value: "", raw: "" })
      .then(function (r) { if (r !== null) { delete state.uiMode["env:" + path]; state.pendingNewParam = null; } });
    return;
  }

  if (t.hasAttribute("data-pn")) {
    var oldName = t.getAttribute("data-pn"), newName = t.value.trim();
    if (t.hasAttribute("readonly") || t.hasAttribute("disabled") || isParamLocked(doc, oldName)) {
      state.render();
      return;
    }
    if (!newName || newName === oldName) { state.render(); return; }
    t.setAttribute("data-pn", newName);
    if (state.paramOrder) {
      var idx = state.paramOrder.indexOf(oldName);
      if (idx !== -1) state.paramOrder[idx] = newName;
    }
    state.pendingRenamedParam = { from: oldName, to: newName };
    state.op(function () { return state.store.renameParameter(oldName, newName); })
      .then(function (r) {
        if (r === null) {
          if (state.paramOrder) {
            var idx = state.paramOrder.indexOf(newName);
            if (idx !== -1) state.paramOrder[idx] = oldName;
          }
          state.render();
        }
      });
    return;
  }

  if (t.hasAttribute("data-mname")) {
    var mKey = t.getAttribute("data-mname").split("|");
    var mParam = mKey[0], mPath = mKey[1];
    var mProps = cloneProps((paramsOf(doc)[mParam] || {}).properties);
    var mLoc = memberParent(mProps, mPath);
    if (!mLoc) return;
    var mNew = t.value.trim();
    if (!mNew || mNew === mLoc.key) { state.render(); return; }
    if (mLoc.parent[mNew]) { state.render(); return; }

    var segs = mPath.split(".");
    segs[segs.length - 1] = mNew;
    var newPath = segs.join(".");
    var oldPath = mPath;

    t.setAttribute("data-mname", mParam + "|" + newPath);

    var fo = fanOut(doc, mParam + "." + oldPath);
    if (fo > 0) {
      var draft = JSON.parse(JSON.stringify(doc));
      renameMemberRefs(draft, mParam, oldPath, newPath);
      state.op(function () { return state.store.replaceDoc(draft); })
        .then(function (r) { if (r === null) state.render(); });
    } else {
      mLoc.parent[mNew] = mLoc.parent[mLoc.key];
      delete mLoc.parent[mLoc.key];
      state.op(function () { return commitMembers(mParam, mProps); })
        .then(function (r) { if (r === null) state.render(); });
    }
    return;
  }

  var mAttr = null;
  ["data-mtype", "data-mreq", "data-mdef"].some(function (a) {
    if (t.hasAttribute(a)) { mAttr = a; return true; }
    return false;
  });
  if (mAttr) {
    mKey = t.getAttribute(mAttr).split("|");
    mParam = mKey[0];
    mPath = mKey[1];
    mProps = cloneProps((paramsOf(doc)[mParam] || {}).properties);
    mLoc = memberParent(mProps, mPath);
    if (!mLoc) return;
    if (mAttr === "data-mtype") {
      var curMemberType = (mLoc.parent[mLoc.key] && mLoc.parent[mLoc.key].type) || "string";
      var newMemberType = t.value;
      if (curMemberType === newMemberType) return;
      var isMemberMismatch = (curMemberType === "object" && newMemberType !== "object") || (curMemberType !== "object" && newMemberType === "object");
      var mFo = isMemberMismatch ? fanOut(doc, mParam + "." + mPath) : 0;
      if (mFo > 0) {
        if (!confirm('Member "' + mParam + '.' + mPath + '" is wired to ' + mFo + " field" + (mFo === 1 ? "" : "s") + ". Changing type to " + newMemberType + " will remove its properties and unwire those fields. Proceed?")) {
          state.render();
          return;
        }
        mLoc.parent[mLoc.key].type = newMemberType;
        if (newMemberType !== "object") delete mLoc.parent[mLoc.key].properties;
        if (newMemberType === "object") {
          delete mLoc.parent[mLoc.key].default;
          delete mLoc.parent[mLoc.key].enum;
        }
        var mDraft = JSON.parse(JSON.stringify(doc));
        cleanMemberRefs(mDraft, mParam, mPath);
        if (mDraft.spec && mDraft.spec.xrd && mDraft.spec.xrd.parameters && mDraft.spec.xrd.parameters[mParam]) {
          mDraft.spec.xrd.parameters[mParam].properties = cloneProps(mProps);
        }
        state.op(function () { return state.store.replaceDoc(mDraft); })
          .then(function (r) { if (r === null) state.render(); });
        return;
      }
    }
    var memberHandler = {
      "data-mtype": function () {
        mLoc.parent[mLoc.key].type = t.value;
        if (t.value !== "object") delete mLoc.parent[mLoc.key].properties;
        if (t.value === "object") { delete mLoc.parent[mLoc.key].default; delete mLoc.parent[mLoc.key].enum; }
        return true;
      },
      "data-mreq": function () {
        mLoc.parent[mLoc.key].required = t.checked;
        return true;
      },
      "data-mdef": function () {
        if (t.value) mLoc.parent[mLoc.key].default = t.value;
        else delete mLoc.parent[mLoc.key].default;
        return true;
      }
    };
    if (memberHandler[mAttr] && memberHandler[mAttr]()) {
      state.op(function () { return commitMembers(mParam, mProps); })
        .then(function (r) { if (r === null) state.render(); });
    }
    return;
  }

  var params = paramsOf(doc);
  for (var pAttr in paramFieldUpdaters) {
    if (t.hasAttribute(pAttr)) {
      if (t.hasAttribute("disabled")) return;
      var paramName = t.getAttribute(pAttr);
      if (isParamLocked(doc, paramName) && (pAttr === "data-pt" || pAttr === "data-pr")) {
        state.render();
        return;
      }
      var patch = paramFieldUpdaters[pAttr](t);
      if (pAttr === "data-pt") {
        var curParam = params[paramName];
        var curType = (curParam && curParam.type) || "string";
        var newType = t.value;
        if (curType === newType) return;
        var isTypeMismatch = (curType === "object" && newType !== "object") || (curType !== "object" && newType === "object");
        var pFo = isTypeMismatch ? fanOut(doc, paramName) : 0;
        if (pFo > 0) {
          if (!confirm('Parameter "' + paramName + '" is wired to ' + pFo + " field" + (pFo === 1 ? "" : "s") + ". Changing type to " + newType + " will remove its properties and unwire those fields. Proceed?")) {
            state.render();
            return;
          }
          var pDraft = JSON.parse(JSON.stringify(doc));
          cleanParamRefs(pDraft, paramName);
          pDraft.spec = pDraft.spec || {};
          pDraft.spec.xrd = pDraft.spec.xrd || {};
          pDraft.spec.xrd.parameters = pDraft.spec.xrd.parameters || {};
          var p = paramFrom(params[paramName], patch);
          if (p.type !== "object") delete p.properties;
          pDraft.spec.xrd.parameters[paramName] = p;
          state.op(function () { return state.store.replaceDoc(pDraft); })
            .then(function (r) { if (r === null) state.render(); });
          return;
        }
      }
      (function (pn, pPatch) {
        state.op(function () { return state.store.updateParameter(pn, paramFrom(params[pn], pPatch)); })
          .then(function (r) { if (r === null) state.render(); });
      })(paramName, patch);
      return;
    }
  }

  if (xrdFieldUpdaters[t.id]) {
    xrdFieldUpdaters[t.id](t);
    return;
  }

  if (t.hasAttribute("data-pipe-fld")) {
    var pidx = parseInt(t.getAttribute("data-pipe-fld"), 10);
    var fpath = t.getAttribute("data-fld-path");
    var fval = t.value;
    (function (idx, path, val) {
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          if (!d.spec.pipeline || !d.spec.pipeline[idx]) return;
          var cur = d.spec.pipeline[idx].input || "";
          var obj = parseInputYAML(cur);
          if (!obj.apiVersion || !obj.kind) {
            var inf = inferFnMeta(d.spec.pipeline[idx]);
            if (inf) {
              obj.apiVersion = obj.apiVersion || inf.apiVersion;
              obj.kind = obj.kind || inf.kind;
            }
          }
          setPathVal(obj, path, val);
          var serialized = serializeInputYAML(obj);
          if (serialized.trim()) d.spec.pipeline[idx].input = serialized;
          else delete d.spec.pipeline[idx].input;
        });
      }).then(function (r) { if (r === null) state.render(); });
    })(pidx, fpath, fval);
    return;
  }

  for (var pipeAttr in pipeAttrs) {
    if (t.hasAttribute(pipeAttr)) {
      var pidx2 = parseInt(t.getAttribute(pipeAttr), 10);
      var attrKey = pipeAttrs[pipeAttr];
      var pval = t.value;
      (function (idx, key, val) {
        state.op(function () {
          return state.store.replaceDoc(function (d) {
            if (!d.spec.pipeline || !d.spec.pipeline[idx]) return;
            if (key === "input" && !val.trim()) {
              delete d.spec.pipeline[idx].input;
            } else {
              d.spec.pipeline[idx][key] = val;
            }
          });
        }).then(function (r) { if (r === null) state.render(); });
      })(pidx2, attrKey, pval);
      return;
    }
  }
}

export function onFsegClick(e) {
  var b = e.target.closest("button");
  if (!b) return;
  state.filter = b.getAttribute("data-f");
  if (state.fseg) {
    Array.prototype.forEach.call(state.fseg.children, function (c) {
      c.setAttribute("aria-pressed", String(c === b));
    });
  }
  state.render();
}

export function bindInspectorEvents(box, fseg) {
  box.addEventListener("click", onBoxClick);
  box.addEventListener("change", onBoxChange);
  box.addEventListener("keydown", function (e) {
    var t = e.target;
    if (!t) return;
    if (t.matches && t.matches("input[data-env-name]")) {
      if (e.key === "Enter") {
        e.preventDefault();
        t.blur();
        return;
      }
      if (e.key === "Escape") {
        t.value = t.getAttribute("data-env-name") || "";
        t.blur();
        return;
      }
    }
    if (t.matches && t.matches("select[data-wire], select[data-env-wire]")) {
      if (e.key === "Enter") {
        delete t.dataset.keyNav;
        e.preventDefault();
        onBoxChange({ target: t });
        t.blur();
        return;
      }
      if (e.key === "Escape") {
        delete t.dataset.keyNav;
        t.blur();
        return;
      }
      if (e.key !== "Tab") {
        t.dataset.keyNav = "true";
        setTimeout(function () {
          delete t.dataset.keyNav;
        }, 50);
      }
      return;
    }
    if (e.key === "Enter" && (t.tagName === "INPUT" || t.tagName === "SELECT")) {
      if (t.matches && t.matches("[data-npname]")) {
        e.preventDefault();
        var npKey = t.getAttribute("data-npname");
        var npAddBtn = box.querySelector('[data-npok="' + CSS.escape(npKey) + '"]');
        if (npAddBtn) npAddBtn.click();
        return;
      }
      if (t.matches && (t.matches("[data-ann-key]") || t.matches("[data-ann-value]"))) {
        e.preventDefault();
        var addBtn = box.querySelector("[data-ann-add]");
        if (addBtn) addBtn.click();
        return;
      }
      e.preventDefault();
      t.blur();
    }
  });
  box.addEventListener("input", function (e) {
    if (state.warnMsg) {
      state.warnMsg = null;
      var wb = box.querySelector(".warnbar");
      if (wb) wb.remove();
    }
    var t = e.target;
    if (t && t.matches && t.matches("[data-ann-key]")) {
      state.annDraftKey = t.value;
    }
    if (!t || !t.matches("textarea.raw")) return;
    var isEnv = t.hasAttribute("data-env-raw");
    var path = t.getAttribute(isEnv ? "data-env-raw" : "data-raw");
    triggerExpressionPreview(t, isEnv, path);
  });
  if (fseg) fseg.addEventListener("click", onFsegClick);
}
