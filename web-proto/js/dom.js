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
