/**
 * store.js — the single state container for the canvas app.
 *
 * FROZEN CONTRACT — region agents code against this file WITHOUT editing it.
 *
 * State shape (read via `store.state`, never mutate directly):
 *   {
 *     doc:              Object|null   — the full blueprint document, exactly as
 *                                       last persisted by the server,
 *     selectedResource: string|null   — resource name from doc.spec.resources[].name,
 *                                       or the literal "xrd" for the XRD/composite
 *                                       node, or null (nothing selected),
 *     positions:        Object        — client-side map name -> {x:number, y:number}
 *                                       (node canvas positions; never persisted),
 *     lastGenerate:     Object|null   — the last /api/generate result
 *                                       {outputs:[{path,bytes,body}], written}
 *   }
 *
 * Topics (FROZEN — these four, no others):
 *   "doc"        payload: the full doc            — emitted whenever the persisted
 *                                                   doc changes (load, replace,
 *                                                   parameter ops)
 *   "selection"  payload: string|null             — emitted when selectedResource changes
 *   "generate"   payload: the generate result     — emitted after a successful generate()
 *   "error"      payload: {status:number, message:string, source:string}
 *                — emitted on any failed API call made through the store.
 *                  `message` is the server's error text VERBATIM (show it).
 *                  `source` is the store method that failed, e.g. "replaceDoc".
 */

import * as api from "./api.js";

/** topic -> Set<fn> */
const subs = { doc: new Set(), selection: new Set(), generate: new Set(), error: new Set() };

function clone(x) {
  return x === null || x === undefined ? x : structuredClone(x);
}

export const store = {
  state: {
    doc: null,
    selectedResource: null,
    positions: {},
    lastGenerate: null,
  },

  /**
   * Subscribe to a topic.
   * @param {"doc"|"selection"|"generate"|"error"} topic
   * @param {function(*): void} fn Called with the topic payload.
   * @returns {function(): void} Unsubscribe function.
   */
  subscribe(topic, fn) {
    if (!subs[topic]) throw new Error("unknown topic: " + topic);
    subs[topic].add(fn);
    return function () { subs[topic].delete(fn); };
  },

  /**
   * Emit a payload to every subscriber of a topic. Region agents normally
   * never call this directly — the store emits; regions subscribe.
   * @param {"doc"|"selection"|"generate"|"error"} topic
   * @param {*} payload
   */
  emit(topic, payload) {
    if (!subs[topic]) throw new Error("unknown topic: " + topic);
    subs[topic].forEach(function (fn) { fn(payload); });
  },

  /**
   * GET the full blueprint from the server into state.doc and emit "doc".
   * On failure emits "error" ({status, message, source:"loadDoc"}).
   * @returns {Promise<Object|null>} The doc, or null on failure.
   */
  async loadDoc() {
    try {
      const doc = await api.getBlueprint();
      this.state.doc = doc;
      this.emit("doc", doc);
      return doc;
    } catch (e) {
      this.emit("error", { status: e.status, message: e.message, source: "loadDoc" });
      return null;
    }
  },

  /**
   * Mutate-and-persist: deep-clones state.doc, applies `mutatorFn` to the
   * clone, PUTs the result as a full-document replace.
   *  - on 200: state.doc becomes the server's persisted doc, "doc" is emitted.
   *  - on 400 (or any failure): state.doc is UNCHANGED, "error" is emitted
   *    with the server's validation message verbatim
   *    ({status, message, source:"replaceDoc"}).
   * @param {function(Object): (Object|void)} mutatorFn Receives a deep clone
   *   of the current doc; mutate it in place (or return a replacement doc).
   * @returns {Promise<Object|null>} The persisted doc, or null on failure.
   */
  async replaceDoc(mutatorFn) {
    if (!this.state.doc) {
      this.emit("error", { status: 0, message: "no document loaded", source: "replaceDoc" });
      return null;
    }
    const next = clone(this.state.doc);
    const returned = mutatorFn(next);
    const candidate = returned === undefined ? next : returned;
    try {
      const doc = await api.putBlueprint(candidate);
      this.state.doc = doc;
      this.emit("doc", doc);
      return doc;
    } catch (e) {
      this.emit("error", { status: e.status, message: e.message, source: "replaceDoc" });
      return null;
    }
  },

  /**
   * Change the selection and emit "selection". No-op if unchanged.
   * @param {string|null} name Resource name, "xrd" for the composite node, or null.
   */
  select(name) {
    if (this.state.selectedResource === name) return;
    this.state.selectedResource = name;
    this.emit("selection", name);
  },

  /**
   * Record a node's client-side canvas position. Positions are never
   * persisted and emit NO topic — the canvas region owns rendering them.
   * @param {string} name Resource name (or "xrd").
   * @param {{x:number, y:number}} pos
   */
  setPosition(name, pos) {
    this.state.positions[name] = { x: pos.x, y: pos.y };
  },

  /**
   * Get a node's recorded position, or null.
   * @param {string} name
   * @returns {{x:number, y:number}|null}
   */
  getPosition(name) {
    return this.state.positions[name] || null;
  },

  /**
   * POST /api/blueprint/parameters. On success state.doc is the returned
   * persisted blueprint and "doc" is emitted; on failure "error" is emitted
   * ({status, message, source:"addParameter"}) and state is unchanged.
   * @param {string} name
   * @param {{type:string, required?:boolean, enum?:string[]|null,
   *   default?:string, description?:string}} parameter
   * @returns {Promise<Object|null>} The persisted doc, or null on failure.
   */
  async addParameter(name, parameter) {
    return this._paramOp("addParameter", api.addParameter(name, parameter));
  },

  /**
   * PUT /api/blueprint/parameters/{name}. Same success/failure behavior as
   * addParameter (source:"updateParameter").
   * @param {string} name
   * @param {{type:string, required?:boolean, enum?:string[]|null,
   *   default?:string, description?:string}} parameter
   * @returns {Promise<Object|null>}
   */
  async updateParameter(name, parameter) {
    return this._paramOp("updateParameter", api.updateParameter(name, parameter));
  },

  /**
   * DELETE /api/blueprint/parameters/{name}. Same success/failure behavior
   * as addParameter (source:"deleteParameter").
   * @param {string} name
   * @returns {Promise<Object|null>}
   */
  async deleteParameter(name) {
    return this._paramOp("deleteParameter", api.deleteParameter(name));
  },

  /**
   * POST /api/blueprint/parameters/{name}/rename — renames the parameter and
   * every wire referencing it. Same success/failure behavior as addParameter
   * (source:"renameParameter").
   * @param {string} name Current name.
   * @param {string} to   New name.
   * @returns {Promise<Object|null>}
   */
  async renameParameter(name, to) {
    return this._paramOp("renameParameter", api.renameParameter(name, to));
  },

  /**
   * POST /api/generate {write:false}. On success state.lastGenerate is set
   * and "generate" is emitted with the result; on failure "error" is emitted
   * ({status, message, source:"generate"}).
   * @param {boolean} [write=false]
   * @returns {Promise<Object|null>} {outputs:[{path,bytes,body}], written}, or null.
   */
  async generate(write) {
    try {
      const result = await api.generate(!!write);
      this.state.lastGenerate = result;
      this.emit("generate", result);
      return result;
    } catch (e) {
      this.emit("error", { status: e.status, message: e.message, source: "generate" });
      return null;
    }
  },

  /**
   * @private Shared handler for parameter routes (each returns the full
   * persisted blueprint).
   */
  async _paramOp(source, promise) {
    try {
      const doc = await promise;
      this.state.doc = doc;
      this.emit("doc", doc);
      return doc;
    } catch (e) {
      this.emit("error", { status: e.status, message: e.message, source });
      return null;
    }
  },
};
