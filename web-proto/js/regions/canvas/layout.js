/**
 * canvas/layout.js — pure topological dependency-tree layout algorithm.
 *
 * Status wires are creation-order facts: a consumer of another resource's
 * observed status cannot exist before its source reports, so it sits to the
 * RIGHT. Layers: XR at 0; a resource's layer = 1 + max(source layers of its
 * status wires).
 *
 * This module is pure: it can be executed and unit-tested in Node or browser
 * without DOM dependencies.
 */

import { listWires } from "../../wires.js";

export const XR_ID = "xrd";
export const ENV_ID = "environment";

export const LAYOUT_CONFIG = {
  GX: 60,
  GY: 24,
  X0: 40,
  Y0: 40,
  DEFAULT_WIDTH: 220,
  DEFAULT_HEIGHT: 160,
};

/**
 * Compute topological layers for resources in a blueprint document.
 * Returns an object mapping resource name to 1-based layer index.
 */
export function dependencyLayers(d) {
  const layers = {};
  const rs = (d && d.spec && d.spec.resources) || [];
  const deps = {};
  rs.forEach(function (r) { deps[r.name] = new Set(); });
  listWires(d).forEach(function (w) {
    if (w.kind === "status" && deps[w.resource] && deps[w.srcResource] && w.srcResource !== w.resource) {
      deps[w.resource].add(w.srcResource);
    }
  });
  function layerOf(name, seen) {
    if (layers[name] !== undefined) return layers[name];
    if (seen[name]) return 1; // cycle guard: flat
    seen[name] = true;
    let l = 1;
    if (deps[name]) {
      deps[name].forEach(function (src) { l = Math.max(l, layerOf(src, seen) + 1); });
    }
    layers[name] = l;
    return l;
  }
  rs.forEach(function (r) { layerOf(r.name, {}); });
  return layers;
}

/**
 * Compute positions for XR, Environment, and all resources in the document.
 *
 * Options:
 *   - getPosition(id): returns existing {x, y} or null/undefined
 *   - setPosition(id, pos): optional callback invoked for placed positions
 *   - getSize(id): returns {width, height} for a card (defaults to 220x160)
 *   - width(id): fallback width function if getSize not provided
 *   - height(id): fallback height function if getSize not provided
 *   - onlyUnplaced: boolean (true preserves user-placed card positions while reserving space)
 *   - autoPlaced: Set of card IDs owned by the auto-layout
 *   - XR_ID, ENV_ID, GX, GY, X0, Y0 overrides
 */
export function computeDependencyLayout(d, options = {}) {
  const autoPlaced = options.autoPlaced || new Set();
  if (!d || !d.spec) return { positions: {}, autoPlaced, layers: {} };

  const getPos = options.getPosition || (() => null);
  const setPos = options.setPosition || null;
  const getSize = options.getSize || function (id) {
    return {
      width: options.width ? options.width(id) : LAYOUT_CONFIG.DEFAULT_WIDTH,
      height: options.height ? options.height(id) : LAYOUT_CONFIG.DEFAULT_HEIGHT,
    };
  };
  const onlyUnplaced = !!options.onlyUnplaced;
  const xrId = options.XR_ID || XR_ID;
  const envId = options.ENV_ID || ENV_ID;
  const GX = options.GX ?? LAYOUT_CONFIG.GX;
  const GY = options.GY ?? LAYOUT_CONFIG.GY;
  const X0 = options.X0 ?? LAYOUT_CONFIG.X0;
  const Y0 = options.Y0 ?? LAYOUT_CONFIG.Y0;

  const layers = dependencyLayers(d);
  const byLayer = {};
  (d.spec.resources || []).forEach(function (r) {
    (byLayer[layers[r.name]] = byLayer[layers[r.name]] || []).push(r.name);
  });

  const positions = {};
  function setPosition(id, pos) {
    positions[id] = pos;
    if (setPos) setPos(id, pos);
  }

  function width(id) {
    const s = getSize(id);
    return (s && typeof s.width === "number") ? s.width : LAYOUT_CONFIG.DEFAULT_WIDTH;
  }
  function height(id) {
    const s = getSize(id);
    return (s && typeof s.height === "number") ? s.height : LAYOUT_CONFIG.DEFAULT_HEIGHT;
  }

  const hasEnv = !!(d.spec && d.spec.environment && Object.keys(d.spec.environment).length > 0);
  let sourceW = width(xrId);
  if (hasEnv) {
    sourceW = Math.max(sourceW, width(envId));
    const envY = Y0 + height(xrId) + GY;
    if (!getPos(envId) || !onlyUnplaced) setPosition(envId, { x: X0, y: envY });
  }
  let x = X0 + sourceW + GX; // layer 1 starts right of the source cards
  if (!getPos(xrId) || !onlyUnplaced) setPosition(xrId, { x: X0, y: Y0 });

  Object.keys(byLayer).map(Number).sort(function (a, b) { return a - b; }).forEach(function (L) {
    let y = Y0;
    let maxW = 0;
    byLayer[L].forEach(function (id) {
      if (onlyUnplaced && getPos(id) && !autoPlaced.has(id)) {
        // user-owned card: leave it where the user put it, but RESERVE its
        // slot in this column so auto-placed siblings never stack into it
        y += height(id) + GY;
        maxW = Math.max(maxW, width(id));
        return;
      }
      autoPlaced.add(id);
      setPosition(id, { x: x, y: y });
      y += height(id) + GY;
      maxW = Math.max(maxW, width(id));
    });
    x += maxW + GX;
  });

  return { positions, autoPlaced, layers };
}
