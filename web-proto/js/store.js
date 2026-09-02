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

/**
 * Every mutating operation runs through this chain: the doc is cloned and
 * the request fired only when the previous operation has settled, so two
 * rapid actions can never lose each other's changes (the ghost-resurrection
 * class: an edit cloned from the pre-delete doc re-PUTting the deleted
 * resource). Failures don't break the chain.
 */
let opChain = Promise.resolve();
function enqueue(taskFn) {
  const run = opChain.then(taskFn, taskFn);
  opChain = run.then(function () {}, function () {});
  return run;
}

export const store = {
  state: {
    doc: null,
    selectedResource: null,
    positions: {},
    undoStack: [],
    redoStack: [],
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
    const self = this;
    return enqueue(function () { return self._replaceDocNow(mutatorFn); });
  },

  async _replaceDocNow(mutatorFn) {
    if (!this.state.doc) {
      this.emit("error", { status: 0, message: "no document loaded", source: "replaceDoc" });
      return null;
    }
    const next = clone(this.state.doc);
    const returned = mutatorFn(next);
    const candidate = returned === undefined ? next : returned;
    try {
      const prev = clone(this.state.doc);
      const doc = await api.putBlueprint(candidate);
      this._recordHistory(prev);
      this.state.doc = doc;
      this.emit("doc", doc);
      return doc;
    } catch (e) {
      this.emit("error", { status: e.status, message: e.message, source: "replaceDoc" });
      return null;
    }
  },

  /** Push the pre-change doc; a new change always clears the redo branch. */
  _recordHistory(prevDoc) {
    this.state.undoStack.push(prevDoc);
    if (this.state.undoStack.length > 50) this.state.undoStack.shift();
    this.state.redoStack.length = 0;
  },

  canUndo() { return this.state.undoStack.length > 0; },
  canRedo() { return this.state.redoStack.length > 0; },

  /**
   * Undo/redo re-PUT a snapshot — the server stays the source of truth.
   * On a rejected PUT the stacks are restored and the error topic fires.
   */
  async undo() { return this._timeTravel(this.state.undoStack, this.state.redoStack, "undo"); },
  async redo() { return this._timeTravel(this.state.redoStack, this.state.undoStack, "redo"); },

  async _timeTravel(from, to, source) {
    const self = this;
    return enqueue(function () { return self._timeTravelNow(from, to, source); });
  },

  async _timeTravelNow(from, to, source) {
    if (!from.length) return null;
    const target = from.pop();
    const current = clone(this.state.doc);
    try {
      const doc = await api.putBlueprint(target);
      to.push(current);
      this.state.doc = doc;
      this.emit("doc", doc);
      return doc;
    } catch (e) {
      from.push(target);
      this.emit("error", { status: e.status, message: e.message, source });
      return null;
    }
  },

  /**
   * Change the selection and emit "selection". No-op if unchanged.
   * @param {string|null} name Resource name, "xrd" for the composite node, or null.
   */
  select(name) {
    if (this.state.selectedResource === name) return; // no-op reselects don't re-render
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
    return this._paramOp("addParameter", function () { return api.addParameter(name, parameter); });
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
    return this._paramOp("updateParameter", function () { return api.updateParameter(name, parameter); });
  },

  /**
   * DELETE /api/blueprint/parameters/{name}. Same success/failure behavior
   * as addParameter (source:"deleteParameter").
   * @param {string} name
   * @returns {Promise<Object|null>}
   */
  async deleteParameter(name) {
    return this._paramOp("deleteParameter", function () { return api.deleteParameter(name); });
  },

  /**
   * POST /api/blueprint/parameters/{name}/rename — renames the parameter and
   * every wire referencing it. Same success/failure behavior as addParameter
   * (source:"renameParameter").
   * @param {string} name Current name.
   * @param {string} to   New name.
   * @returns {Promise<Object|null>}
   */
  /** POST /api/blueprint/import — YAML through the file gate; one undo step. */
  async importBlueprint(yamlText) {
    return this._paramOp("importBlueprint", function () { return api.importBlueprint(yamlText); });
  },

  /** POST /api/examples/{id}/load — load starter blueprint and import/cache its providers. */
  async loadExample(id) {
    return this._paramOp("loadExample", function () { return api.loadExample(id); });
  },

  /** POST /api/blueprint/resources/{name}/rename — same contract as the parameter ops. */
  async renameResource(name, to) {
    return this._paramOp("renameResource", function () { return api.renameResource(name, to); });
  },

  async renameParameter(name, to) {
    return this._paramOp("renameParameter", function () { return api.renameParameter(name, to); });
  },

  /**
   * POST /api/generate {write:false}. On success state.lastGenerate is set
   * and "generate" is emitted with the result; on failure "error" is emitted
   * ({status, message, source:"generate"}).
   * @param {boolean} [write=false]
   * @returns {Promise<Object|null>} {outputs:[{path,bytes,body}], written}, or null.
   */
  _generateSeq: 0,

  async generate(write) {
    // latest-wins: a slow earlier generate must never overwrite a newer
    // result (stale composition shown for a fresh doc — the ghost class).
    const seq = ++this._generateSeq;
    try {
      const result = await api.generate(!!write);
      if (seq !== this._generateSeq) return result; // superseded — drop silently
      this.state.lastGenerate = result;
      this.emit("generate", result);
      return result;
    } catch (e) {
      if (seq === this._generateSeq) {
        this.emit("error", { status: e.status, message: e.message, source: "generate" });
      }
      return null;
    }
  },

  /**
   * @private Shared handler for parameter routes (each returns the full
   * persisted blueprint).
   */
  async _paramOp(source, requestFn) {
    const self = this;
    return enqueue(function () { return self._paramOpNow(source, requestFn); });
  },

  async _paramOpNow(source, requestFn) {
    try {
      const prev = clone(this.state.doc);
      const doc = await requestFn();
      this._recordHistory(prev);
      this.state.doc = doc;
      this.emit("doc", doc);
      return doc;
    } catch (e) {
      this.emit("error", { status: e.status, message: e.message, source });
      return null;
    }
  },
};
