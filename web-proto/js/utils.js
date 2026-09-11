/**
 * utils.js — shared utility functions for web-proto regions.
 */

import { store as defaultStore } from "./store.js";

/** Node color families across the canvas and palette. */
export const COLORS = {
  aws: "var(--wire-ref)",
  k8s: "var(--wire-status)",
  xrd: "var(--wire-xrd)",
  azure: "#0078d4",
  gcp: "#ea4335",
  helm: "#0f1689",
  cluster: "#06b6d4",
};

/**
 * Provider family for color scheme classification.
 * Supports k8s, azure, gcp, helm, cluster, aws.
 * Accepts either a provider/group string or an object with provider/group properties.
 *
 * @param {string|Object|null} target
 * @returns {string} One of "k8s", "azure", "gcp", "helm", "cluster", "aws"
 */
export function famOf(target) {
  if (!target) return "k8s";
  let s;
  if (typeof target === "object") {
    if (target.provider === "cluster") return "cluster";
    const p = target.provider || "";
    const g = target.group || "";
    s = (g + " " + p).toLowerCase().trim();
    if (!s) return "k8s";
  } else {
    s = String(target).toLowerCase().trim();
    if (!s) return "k8s";
    if (s === "cluster") return "cluster";
  }
  if (s.indexOf("kubernetes") >= 0 || s === "k8s") return "k8s";
  if (s.indexOf("azure") >= 0) return "azure";
  if (s.indexOf("gcp") >= 0 || s.indexOf("google") >= 0) return "gcp";
  if (s.indexOf("helm") >= 0) return "helm";
  if (s.indexOf("aws") >= 0) return "aws";
  if (typeof target === "object" && !target.provider) return "k8s";
  return "aws";
}

/** Prototype slug: CamelCase -> camel-case. */
export function slug(k) {
  return String(k).replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}

/**
 * Generate a unique resource name in blueprint doc d based on kind.
 *
 * @param {Object} d Blueprint document
 * @param {string} kind Resource kind name (e.g. "Bucket")
 * @returns {string}
 */
export function uniqueResourceName(d, kind) {
  const base = slug(kind);
  const names = {};
  ((d && d.spec && d.spec.resources) || []).forEach(function (r) { names[r.name] = true; });
  if (!names[base]) return base;
  let i = 2;
  while (names[base + "-" + i]) i++;
  return base + "-" + i;
}

/**
 * Map index-based spec.resources[N] coordinates in error messages to human-readable resource names.
 *
 * @param {string} msg Raw error message containing spec.resources[N]
 * @param {Object} [storeOrDoc] Optional store or blueprint document instance
 * @returns {string}
 */
export function mapResourceCoordinates(msg, storeOrDoc) {
  if (!msg || typeof msg !== "string") return msg;
  return msg.replace(/spec\.resources\[(\d+)\]/g, function (match, indexStr) {
    var idx = parseInt(indexStr, 10);
    var doc = (storeOrDoc && storeOrDoc.spec)
      ? storeOrDoc
      : (storeOrDoc && storeOrDoc.state && storeOrDoc.state.doc)
      || (defaultStore && defaultStore.state && defaultStore.state.doc)
      || (typeof window !== "undefined" && window.store && window.store.state && window.store.state.doc);
    var resList = doc && doc.spec && doc.spec.resources;
    if (resList && resList[idx]) {
      var name = resList[idx].name || resList[idx].kind;
      if (name) {
        return "resource '" + name + "' (" + match + ")";
      }
    }
    return match;
  });
}
