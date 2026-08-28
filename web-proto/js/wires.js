/**
 * wires.js — pure helpers over the blueprint document. No DOM, no state.
 *
 * FROZEN CONTRACT — region agents code against this file WITHOUT editing it.
 *
 * Engine truth: wires live IN the doc. A resource field of the form
 *   { from: "params.X" }
 * is a wire from parameter X to that field. Field forms are exactly-one-of
 * {value|from|raw} (the server pads the other keys with "" — treat "" as absent).
 */

/**
 * List every wire in the document.
 * @param {Object} doc The full blueprint document.
 * @returns {Array<{param:string, resource:string, path:string}>}
 *   param    — parameter name (the part after "params."),
 *   resource — resource name (doc.spec.resources[].name),
 *   path     — the field path within that resource (key of resources[].fields).
 *   Order: resources in doc order, field paths sorted within each resource.
 */
export function listWires(doc) {
  const out = [];
  const resources = doc && doc.spec && doc.spec.resources || [];
  resources.forEach(function (r) {
    const fields = r.fields || {};
    Object.keys(fields).sort().forEach(function (path) {
      const f = fields[path];
      if (f && typeof f.from === "string" && f.from.indexOf("params.") === 0) {
        out.push({ param: f.from.slice("params.".length), resource: r.name, path });
      }
    });
  });
  return out;
}

/**
 * Fan-out of one parameter: how many fields it is wired into.
 * @param {Object} doc The full blueprint document.
 * @param {string} param Parameter name (without the "params." prefix).
 * @returns {number}
 */
export function fanOut(doc, param) {
  return listWires(doc).filter(function (w) { return w.param === param; }).length;
}
