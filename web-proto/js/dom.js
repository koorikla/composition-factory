/**
 * dom.js — shared DOM utilities and HTML escaping for web-proto.
 */

/**
 * HTML-escape any value safely.
 * Handles null/undefined -> "", numbers/booleans -> string, and escapes &, <, >, ", '.
 * @param {*} s
 * @returns {string}
 */
export function esc(s) {
  return String(s === undefined || s === null ? "" : s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * Query selector shorthand.
 * @param {string} sel
 * @param {ParentNode} [parent=document]
 * @returns {Element|null}
 */
export function qs(sel, parent = document) {
  return parent.querySelector(sel);
}

/**
 * Query selector all shorthand as an Array.
 * @param {string} sel
 * @param {ParentNode} [parent=document]
 * @returns {Element[]}
 */
export function qsa(sel, parent = document) {
  return Array.from(parent.querySelectorAll(sel));
}

/**
 * Generic pointer-drag helper supporting mouse and touch interactions.
 * Uses pointer capture when supported to avoid leaking listeners on pointercancel.
 * @param {PointerEvent} e The initial pointerdown event
 * @param {(e: PointerEvent) => void} onMove Called on each pointermove
 * @param {(e: PointerEvent) => void} [onEnd] Called on pointerup / pointercancel
 */
export function startDrag(e, onMove, onEnd) {
  const target = e.currentTarget || e.target;
  const pointerId = e.pointerId;
  if (target && typeof target.setPointerCapture === "function" && pointerId !== undefined) {
    try { target.setPointerCapture(pointerId); } catch (_) {}
  }

  function handleMove(ev) {
    if (pointerId !== undefined && ev.pointerId !== pointerId) return;
    if (onMove) onMove(ev);
  }

  function handleEnd(ev) {
    if (pointerId !== undefined && ev.pointerId !== pointerId) return;
    if (target && typeof target.releasePointerCapture === "function" && pointerId !== undefined) {
      try { target.releasePointerCapture(pointerId); } catch (_) {}
    }
    window.removeEventListener("pointermove", handleMove);
    window.removeEventListener("pointerup", handleEnd);
    window.removeEventListener("pointercancel", handleEnd);
    if (onEnd) onEnd(ev);
  }

  window.addEventListener("pointermove", handleMove);
  window.addEventListener("pointerup", handleEnd);
  window.addEventListener("pointercancel", handleEnd);
}
