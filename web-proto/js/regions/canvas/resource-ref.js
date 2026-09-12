/**
 * canvas/resource-ref.js — Pure reference detection and cleanup for composed resources.
 *
 * Checks whether a raw template expression, wire path, or nested config references
 * a given resource name, mirroring Go's internal/blueprint/edit.go:rawReferencesResource.
 *
 * Detects:
 *  - Dotted references: resources.<name>, observed.resources.<name>, $.observed.resources.<name>
 *  - Go template index: {{ (index $observed.resources "<name>") }}
 *  - Go template hasKey: {{ hasKey $observed.resources "<name>" }}
 *  - Sprig dig: {{ hasKey (dig "resources" "<name>" ...) }}
 *  - Crossplane getComposedResource: {{ getComposedResource $observed "<name>" }}
 *
 * Supports double quotes ("), single quotes ('), and backticks (`).
 * Pure: zero DOM dependencies; runnable in Node and browser.
 */

export function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Checks whether val (wire path or raw expression) references the target resource.
 * @param {*} val
 * @param {string} name
 * @returns {boolean}
 */
export function isResourceRef(val, name) {
  if (typeof val !== "string" || !val || !name) return false;
  if (val === "resources." + name || val.startsWith("resources." + name + ".")) return true;

  const q = escapeRegex(name);

  // 1. Dotted references: resources.<name>, observed.resources.<name>, $.observed.resources.<name>
  const reDotted = new RegExp("(?:^|[^a-zA-Z0-9_$-])(?:(?:\\$|\\$\\.|\\.)?observed\\.resources|resources)\\." + q + "(?:$|[^a-zA-Z0-9_-])");
  if (reDotted.test(val)) return true;

  // 2. index expressions: e.g. {{ (index $observed.resources "<name>").resource.status.url }}
  const reIndex = new RegExp("\\bindex\\s+(?:(?:\\$|\\$\\.|\\.)?(?:observed\\.)?resources)\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reIndex.test(val)) return true;

  // 3. hasKey expressions: e.g. {{ hasKey $observed.resources "<name>" }}
  const reHasKey = new RegExp("\\bhasKey\\s+(?:(?:\\$|\\$\\.|\\.)?(?:observed\\.)?resources)\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reHasKey.test(val)) return true;

  // 4. Sprig dig expressions: e.g. {{ hasKey (dig "resources" "<name>" "resource" "status" dict $.observed) "url" }}
  const reDig = new RegExp("\\bdig\\s+(?:(?:\"observed\"|'observed'|`observed`)\\s+)?(?:\"resources\"|'resources'|`resources`)\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reDig.test(val)) return true;

  // 5. getComposedResource expressions: e.g. {{ getComposedResource $observed "<name>" }}
  const reGetComposed = new RegExp("\\bgetComposedResource\\s+[^\\s\"'`]+\\s+(?:\"" + q + "\"|'" + q + "'|`" + q + "`)(?:$|[^a-zA-Z0-9_-])");
  if (reGetComposed.test(val)) return true;

  return false;
}

/**
 * Recursively checks whether an object (e.g. connectionSecret) references the given resource.
 * @param {*} obj
 * @param {string} name
 * @returns {boolean}
 */
export function isObjectReferencingResource(obj, name) {
  if (!obj || typeof obj !== "object") return false;
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    if (typeof v === "string" && isResourceRef(v, name)) return true;
    if (typeof v === "object" && isObjectReferencingResource(v, name)) return true;
  }
  return false;
}

/**
 * Finds all downstream references to resource `name` in other resources' fields, envelope,
 * annotations, connectionSecret, when, or forEach, as well as templates in spec.templates
 * and spec.conventions.
 * @param {any[]|Object} resourcesOrDraft
 * @param {string} name
 * @param {Object} [templatesOrDoc]
 * @param {Array|Object} [conventions]
 * @returns {Array<{name: string, fields: Array<string>, type?: string, templates?: Array<string>}>}
 */
export function findDownstreamRefs(resourcesOrDraft, name, templatesOrDoc, conventions) {
  if (!name) return [];

  let resources;
  let templates = null;
  let convs = null;

  if (resourcesOrDraft && !Array.isArray(resourcesOrDraft) && resourcesOrDraft.spec) {
    resources = resourcesOrDraft.spec.resources || [];
    templates = templatesOrDoc || resourcesOrDraft.spec.templates || null;
    convs = conventions || resourcesOrDraft.spec.conventions || null;
  } else {
    resources = resourcesOrDraft || [];
    if (templatesOrDoc && !Array.isArray(templatesOrDoc) && templatesOrDoc.spec) {
      templates = templatesOrDoc.spec.templates || null;
      convs = conventions || templatesOrDoc.spec.conventions || null;
    } else if (templatesOrDoc && typeof templatesOrDoc === "object") {
      if (templatesOrDoc.templates || templatesOrDoc.conventions) {
        templates = templatesOrDoc.templates || null;
        convs = conventions || templatesOrDoc.conventions || null;
      } else {
        templates = templatesOrDoc;
      }
    }
    if (conventions) {
      if (Array.isArray(conventions)) {
        convs = conventions;
      } else if (conventions.spec && conventions.spec.conventions) {
        convs = conventions.spec.conventions;
      } else if (conventions.conventions) {
        convs = conventions.conventions;
      }
    }
  }

  const depTemplates = [];
  if (templates && typeof templates === "object") {
    Object.keys(templates).forEach(function (tName) {
      const body = templates[tName];
      if (typeof body === "string" && isResourceRef(body, name)) {
        depTemplates.push(tName);
      }
    });
  }

  const downstream = [];
  (resources || []).forEach(function (r) {
    if (r.name === name) return;
    const depFields = [];
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        const f = r.fields[k];
        if (f) {
          if ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name))) {
            depFields.push(k);
          } else if (f.template && depTemplates.indexOf(f.template) !== -1) {
            depFields.push(k);
          }
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        const f = r.envelope[k];
        if (f) {
          if ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name))) {
            depFields.push("envelope." + k);
          } else if (f.template && depTemplates.indexOf(f.template) !== -1) {
            depFields.push("envelope." + k);
          }
        }
      });
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f) {
          if ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name))) {
            depFields.push("annotations." + k);
          } else if (f.template && depTemplates.indexOf(f.template) !== -1) {
            depFields.push("annotations." + k);
          }
        }
      });
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string" && isResourceRef(r.connectionSecret, name)) {
        depFields.push("connectionSecret");
      } else if (typeof r.connectionSecret === "object" && isObjectReferencingResource(r.connectionSecret, name)) {
        depFields.push("connectionSecret");
      }
    }
    if (r.when && isResourceRef(r.when, name)) {
      depFields.push("when");
    }
    if (r.forEach && isResourceRef(r.forEach, name)) {
      depFields.push("forEach");
    }
    if (depFields.length > 0) {
      downstream.push({ name: r.name, fields: depFields });
    }
  });

  depTemplates.forEach(function (tName) {
    downstream.push({
      name: "template:" + tName,
      fields: [tName],
      type: "template",
      templates: [tName],
    });
  });

  if (Array.isArray(convs)) {
    convs.forEach(function (c) {
      if (c && c.template && depTemplates.indexOf(c.template) !== -1) {
        downstream.push({
          name: "convention:" + c.template,
          fields: [c.match ? "match:" + c.match : c.template],
          type: "convention",
          templates: [c.template],
        });
      }
    });
  }

  return downstream;
}

/**
 * Cleans all downstream references to resource `name` in other resources' fields, envelope,
 * annotations, connectionSecret, when, or forEach, as well as referencing templates in spec.templates,
 * and conventions or fields referencing those deleted templates.
 * @param {any[]|Object} resourcesOrDraft
 * @param {string} name
 * @param {Object} [templatesOrDoc]
 * @param {Object|Array} [draftOrConventions]
 * @param {Object} [draftArg]
 */
export function cleanDownstreamRefs(resourcesOrDraft, name, templatesOrDoc, draftOrConventions, draftArg) {
  if (!name) return;

  let resources;
  let templates = null;
  let draft = null;
  let convs = null;

  if (resourcesOrDraft && !Array.isArray(resourcesOrDraft) && resourcesOrDraft.spec) {
    draft = resourcesOrDraft;
    resources = draft.spec.resources || [];
    templates = templatesOrDoc || draft.spec.templates || null;
    convs = draft.spec.conventions || null;
  } else {
    resources = resourcesOrDraft || [];
    if (templatesOrDoc && !Array.isArray(templatesOrDoc) && templatesOrDoc.spec) {
      draft = templatesOrDoc;
      templates = draft.spec.templates || null;
      convs = draft.spec.conventions || null;
    } else if (templatesOrDoc && typeof templatesOrDoc === "object") {
      templates = templatesOrDoc;
    }

    if (draftOrConventions) {
      if (draftOrConventions.spec) {
        draft = draftOrConventions;
        if (!convs) convs = draft.spec.conventions || null;
      } else if (Array.isArray(draftOrConventions)) {
        convs = draftOrConventions;
      }
    }
    if (draftArg && draftArg.spec) {
      draft = draftArg;
      if (!convs) convs = draft.spec.conventions || null;
    }
  }

  const deletedTemplates = [];
  if (templates && typeof templates === "object") {
    Object.keys(templates).forEach(function (tName) {
      const body = templates[tName];
      if (typeof body === "string" && isResourceRef(body, name)) {
        delete templates[tName];
        deletedTemplates.push(tName);
      }
    });
    if (Object.keys(templates).length === 0 && draft && draft.spec && draft.spec.templates) {
      delete draft.spec.templates;
    }
  }

  if (deletedTemplates.length > 0) {
    if (draft && draft.spec && Array.isArray(draft.spec.conventions)) {
      draft.spec.conventions = draft.spec.conventions.filter(function (c) {
        return c && deletedTemplates.indexOf(c.template) === -1;
      });
      if (draft.spec.conventions.length === 0) {
        delete draft.spec.conventions;
      }
    } else if (Array.isArray(convs)) {
      const remaining = convs.filter(function (c) {
        return c && deletedTemplates.indexOf(c.template) === -1;
      });
      convs.length = 0;
      remaining.forEach(function (c) { convs.push(c); });
    }
  }

  (resources || []).forEach(function (r) {
    if (r.name === name) return;
    if (r.fields) {
      Object.keys(r.fields).forEach(function (k) {
        const f = r.fields[k];
        if (f) {
          if ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name))) {
            delete r.fields[k];
          } else if (deletedTemplates.length > 0 && f.template && deletedTemplates.indexOf(f.template) !== -1) {
            delete r.fields[k];
          }
        }
      });
    }
    if (r.envelope) {
      Object.keys(r.envelope).forEach(function (k) {
        const f = r.envelope[k];
        if (f) {
          if ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name))) {
            delete r.envelope[k];
          } else if (deletedTemplates.length > 0 && f.template && deletedTemplates.indexOf(f.template) !== -1) {
            delete r.envelope[k];
          }
        }
      });
      if (Object.keys(r.envelope).length === 0) delete r.envelope;
    }
    if (r.annotations) {
      Object.keys(r.annotations).forEach(function (k) {
        const f = r.annotations[k];
        if (f) {
          if ((f.from && isResourceRef(f.from, name)) || (f.raw && isResourceRef(f.raw, name))) {
            delete r.annotations[k];
          } else if (deletedTemplates.length > 0 && f.template && deletedTemplates.indexOf(f.template) !== -1) {
            delete r.annotations[k];
          }
        }
      });
      if (Object.keys(r.annotations).length === 0) delete r.annotations;
    }
    if (r.connectionSecret) {
      if (typeof r.connectionSecret === "string") {
        if (isResourceRef(r.connectionSecret, name)) delete r.connectionSecret;
      } else if (typeof r.connectionSecret === "object") {
        if (Array.isArray(r.connectionSecret.keys)) {
          r.connectionSecret.keys = r.connectionSecret.keys.filter(function (item) {
            if (typeof item === "string") return !isResourceRef(item, name);
            if (item && typeof item === "object") {
              if (item.from && isResourceRef(item.from, name)) return false;
              if (item.fromNode && item.fromNode === name) return false;
            }
            return true;
          });
          if (r.connectionSecret.keys.length === 0) delete r.connectionSecret;
        } else if (isObjectReferencingResource(r.connectionSecret, name)) {
          delete r.connectionSecret;
        }
      }
    }
    if (r.when && isResourceRef(r.when, name)) {
      delete r.when;
    }
    if (r.forEach && isResourceRef(r.forEach, name)) {
      delete r.forEach;
    }
  });
}
