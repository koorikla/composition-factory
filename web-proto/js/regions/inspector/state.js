/**
 * Shared runtime state for the Inspector region and its submodules
 * (xrd.js, preview.js, events.js).
 */

import { store as defaultStore } from "../../store.js";
import * as defaultApi from "../../api.js";

export var PARAM_TYPES = ["string", "integer", "number", "boolean", "object"];

export var state = {
  store: defaultStore,
  api: defaultApi,
  root: null,
  box: null,   // #insp
  fseg: null,  // #fseg

  filter: "req",              // "req" | "set" | "all"
  warnMsg: null,              // verbatim server error to show, or null
  uiMode: {},                 // path -> "v"|"w"|"r" local mode override (selected resource only)
  pendingNewParam: null,      // field path currently showing the inline new-parameter form
  pendingNewMapEntry: null,   // map field path currently showing the inline add-key form
  pendingFocusParam: null,    // parameter name to focus and select in XRD inspector after render
  paramOrder: null,           // stable order of XRD parameter names while inspector is open
  pendingRenamedParam: null,  // { from: string, to: string } to follow focus across async rename
  pendingFocusEnvKey: null,   // environment key name to focus and select after render
  pendingRenamedEnvKey: null, // { from: string, to: string } to follow focus across async rename
  lastDocName: null,
  annDraftKey: "",            // draft annotation key being entered (CF-136)
  annDraftRes: "",            // resource name annDraftKey belongs to (CF-136)
  renderToken: 0,
  pipeInputMode: {},

  kindsPromise: null,         // cached GET /api/kinds
  fieldsCache: {},            // "apiVersion|kind" -> {fields,total}
  kindDetailCache: {},        // "apiVersion|kind" -> {kind, envelope, status}

  // Coordinator delegates set during init/coordinator load:
  render: function () {},
  op: async function (fn) { return fn ? fn() : null; },
  selectedResource: function () { return null; },
  entryOf: function () { return null; },
  envelopeEntryOf: function () { return null; },
  docMode: function () { return "v"; },
  setField: async function () { return null; },
  setEnvelopeField: async function () { return null; },
  updateEnvSelection: function () {},
  renameEnvKey: function () {},
  setEnvKeyField: function () {},
  deleteEnvKey: function () {},
  addEnvKey: function () {},
  removeWire: function () {},
  snapshotFocusedEdit: function () { return null; },
  restoreFocusedEdit: function () {},
};
