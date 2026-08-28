// The inspector: the field tree for one selected node, over the fields API.
//
// Required-first is not a display nicety here, it is the whole point: an
// EC2 Instance has 263 forProvider properties and the largest MR schema
// this project has measured is 1.7 MB. A form (or a component tree) that
// opens on every field is unusable and, for the biggest kinds, would choke
// the browser before a user ever saw it. So this panel opens on
// `required_only=true` and fetches the rest ONLY when the filter is
// switched to "all" — never fetches everything and filters client-side,
// the same rule ResourceNode.tsx (the canvas ports) already follows.
//
// A field's description is shown unconditionally: it is the ONLY
// documentation a CRD carries (there is no separate doc site to link to),
// and the reference-inference layer this product is building toward reads
// that same prose. Dropping it to save vertical space would be dropping
// the one thing a user has to go on.
//
// The store is the single source of truth: every input here is controlled
// directly from `doc` (via `useBlueprint`), and every edit goes through the
// store's `setField`/`connect` — this component holds no shadow copy of a
// field's value, only the ephemeral, presentation-only "is this field's
// current text invalid, and why" state, which is not product data.
import { useEffect, useState, type CSSProperties } from "react"
import { api } from "../api/contract"
import type { Field } from "../api/contract"
import { useBlueprint } from "../store/blueprint"

// ---------------------------------------------------------------------------
// Control-character validation
//
// Mirrors internal/blueprint/load.go's checkScalar exactly: every emitter
// writes a resource field's literal value as a single-line YAML scalar
// (Doc.Line writes `indent + text + "\n"` verbatim, and a single-quoted
// scalar is still a one-line construct), so an embedded newline, carriage
// return, tab or other control rune either breaks the document outright or
// — worse — silently grows it a bogus top-level key. There is no dedicated
// "validate this field" or "save this field" HTTP route in the frozen M3
// contract (see docs/superpowers/plans/2026-08-28-m3-canvas.md's "frozen
// contract" table): resource field edits are local-only, unlike XRD
// parameter edits, which do round-trip through
// POST/PUT/DELETE /api/blueprint/parameters. POST /api/generate is the only
// route that ever surfaces blueprint.Validate() errors, and it takes
// `{"write":bool}` only — it re-reads the document from disk, exactly like
// the real Go handler does, and never carries the client's in-progress
// edit. So this check runs the identical rule, with the identical message
// shape, client-side — "server validation, mirrored" rather than a literal
// round trip that the current contract has no vehicle for. See the task
// report for why this was not routed through MSW instead.
//
// THIS IS A SECOND COPY OF A RULE THAT LIVES IN GO. If
// internal/blueprint/load.go's checkScalar ever changes — which runes it
// rejects, the message shape, the byte-offset convention — update this
// mirror to match. It WILL drift silently otherwise: nothing here is
// generated from, or tested against, the Go source.
// ---------------------------------------------------------------------------

const CONTROL_CHAR_EXPLANATION =
  "newlines, carriage returns, tabs and other non-printable runes are not allowed " +
  "because the emitter writes this value as a single-line YAML scalar -- a line break " +
  "escapes it and silently changes the generated document's structure"

/** unicode.IsControl (Go) is Unicode category Cc: C0 controls (incl. \t \n \r),
 * DEL, and the C1 controls. U+2028/U+2029 are added explicitly, matching
 * checkScalar's own comment: YAML 1.1 treats both as line breaks even though
 * they are not category Cc (they're Zl/Zp). */
function isControlRune(codePoint: number): boolean {
  if (codePoint === 0x2028 || codePoint === 0x2029) return true
  if (codePoint <= 0x1f) return true
  if (codePoint >= 0x7f && codePoint <= 0x9f) return true
  return false
}

// Go's fmt %q for a rune (strconv.QuoteRune) names seven control characters
// individually; everything else control-ish falls through to a numeric
// escape. Keyed by code point, not by JS string, so 0x07 etc. don't need a
// second "which literal char is this" lookup.
const GO_NAMED_RUNE_ESCAPES: Record<number, string> = {
  0x07: "\\a", // alert / BEL
  0x08: "\\b", // backspace
  0x09: "\\t", // tab
  0x0a: "\\n", // line feed
  0x0b: "\\v", // vertical tab
  0x0c: "\\f", // form feed
  0x0d: "\\r", // carriage return
}

/** Formats one rune exactly the way Go's fmt %q (strconv.QuoteRune) does,
 * for the specific set checkScalar ever hands it: the seven named C0
 * escapes above; \xHH (two lowercase hex digits) for every other C0
 * control and DEL; \uHHHH (four lowercase hex digits, NO braces — that's
 * JS's \u{...} syntax, not Go's) for the C1 controls (0x80-0x9f, which
 * includes NEL, U+0085) and for U+2028/U+2029. isControlRune() below never
 * passes this anything outside that set, so those three branches are
 * exhaustive for this mirror's actual input — not a general %q
 * implementation. */
function quoteRune(ch: string): string {
  const codePoint = ch.codePointAt(0)!
  const named = GO_NAMED_RUNE_ESCAPES[codePoint]
  if (named) return `'${named}'`
  if (codePoint < 0x20 || codePoint === 0x7f) {
    return `'\\x${codePoint.toString(16).padStart(2, "0")}'`
  }
  return `'\\u${codePoint.toString(16).padStart(4, "0")}'`
}

const byteLength = (s: string) => new TextEncoder().encode(s).length

/** Returns the verbatim-style error Go's checkScalar(fieldPath, s) would
 * return, or null if s is clean. fieldPath mirrors the shape Validate()
 * builds for a resource field: `resource "<name>" field "<path>": value`.
 * Exported for direct unit testing (fix round 1, Finding 4) — the rune
 * table this exists to get exactly right is easier to assert against
 * directly than through a live textarea and RTL. */
export function checkScalar(resourceName: string, fieldPath: string, s: string): string | null {
  let byteOffset = 0
  for (const ch of s) {
    const codePoint = ch.codePointAt(0)!
    if (isControlRune(codePoint)) {
      return (
        `resource ${JSON.stringify(resourceName)} field ${JSON.stringify(fieldPath)}: value: ` +
        `contains the control character ${quoteRune(ch)} at byte ${byteOffset}; ${CONTROL_CHAR_EXPLANATION}`
      )
    }
    byteOffset += byteLength(ch)
  }
  return null
}

// ---------------------------------------------------------------------------
// Field fetch + merge
// ---------------------------------------------------------------------------

type Filter = "required" | "all"

/** Fetches one filtered field list, then tops it up with any field this
 * resource already has an assignment for but that the filter excluded
 * (e.g. a non-required field wired to a parameter before this panel ever
 * fetched it) — a bound field must stay visible regardless of which filter
 * is active, or switching to "required" would make an existing wire vanish
 * from view while leaving it fully intact in the document underneath.
 * Mirrors canvas/ResourceNode.tsx's identical required-plus-bound merge. */
async function fetchFields(
  apiVersion: string,
  kind: string,
  requiredOnly: boolean,
  boundPaths: string[],
): Promise<Field[]> {
  const primary = await api.fields(apiVersion, kind, { requiredOnly })
  let all = primary.fields
  const have = new Set(all.map(f => f.path))
  const missing = boundPaths.filter(p => !have.has(p))
  if (missing.length > 0) {
    const extras = await Promise.all(missing.map(p => api.fields(apiVersion, kind, { prefix: p })))
    all = [...all, ...extras.flatMap(e => e.fields)]
  }
  return all
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export interface InspectorProps {
  nodeId: string
}

export function Inspector({ nodeId }: InspectorProps) {
  const node = useBlueprint(s => s.nodes.find(n => n.id === nodeId))
  const resource = useBlueprint(s =>
    node ? s.doc?.spec.resources.find(r => r.name === node.name) : undefined,
  )
  const setField = useBlueprint(s => s.setField)
  const commitField = useBlueprint(s => s.commitField)

  const [filter, setFilter] = useState<Filter>("required")
  const [fields, setFields] = useState<Field[]>([])
  const [errors, setErrors] = useState<Record<string, string>>({})

  const boundPaths = resource ? Object.keys(resource.fields) : []
  const boundKey = boundPaths.slice().sort().join(",")

  useEffect(() => {
    if (!node) return
    let cancelled = false
    fetchFields(node.apiVersion, node.kind, filter === "required", boundPaths)
      .then(all => {
        if (!cancelled) setFields(all)
      })
      .catch(() => {
        if (!cancelled) setFields([])
      })
    return () => {
      cancelled = true
    }
    // boundKey, not boundPaths (a fresh array every render): re-fetch only
    // when which paths are bound actually changes, same as ResourceNode.
  }, [node?.apiVersion, node?.kind, filter, boundKey])

  if (!node || !resource) return null

  // Required-first: the panel's whole reason to exist. Once "all" is
  // fetched, the required fields a user actually has to fill in still lead
  // the list rather than sorting alphabetically in among 260-odd optional
  // ones.
  const sorted = [...fields].sort((a, b) => Number(b.required) - Number(a.required))

  function handleChange(path: string, value: string) {
    setField(nodeId, path, value)
    const err = checkScalar(resource!.name, path, value)
    setErrors(prev => {
      if (err === null) {
        if (!(path in prev)) return prev
        const next = { ...prev }
        delete next[path]
        return next
      }
      return { ...prev, [path]: err }
    })
  }

  return (
    <div
      data-testid="inspector"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 10,
        padding: 10,
        height: "100%",
        overflow: "auto",
        background: "var(--surface-2)",
        color: "var(--ink)",
        fontFamily: "var(--sans)",
      }}
    >
      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <span style={{ fontWeight: 600 }}>{node.kind}</span>
        <span className="mono" style={{ fontSize: 11, color: "var(--faint)" }}>
          {resource.name}
        </span>
      </div>

      <div role="group" aria-label="field filter" style={{ display: "flex", gap: 6 }}>
        <button
          type="button"
          aria-pressed={filter === "required"}
          onClick={() => setFilter("required")}
          style={filterButtonStyle(filter === "required")}
        >
          Required
        </button>
        <button
          type="button"
          aria-pressed={filter === "all"}
          onClick={() => setFilter("all")}
          style={filterButtonStyle(filter === "all")}
        >
          All
        </button>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {sorted.map(f => {
          const assignment = resource.fields[f.path]
          const wiredFrom = assignment?.from
          const error = errors[f.path]
          return (
            <div
              key={f.path}
              data-testid={`field-${f.path}`}
              style={{
                display: "flex",
                flexDirection: "column",
                gap: 4,
                paddingBottom: 8,
                borderBottom: "1px solid var(--rule)",
              }}
            >
              <div style={{ display: "flex", alignItems: "baseline", gap: 4 }}>
                <span className="mono" style={{ fontWeight: 600 }}>
                  {f.path}
                </span>
                {f.required && (
                  <span
                    aria-label="required"
                    title="required"
                    style={{ color: "var(--warn)" }}
                  >
                    *
                  </span>
                )}
                <span className="mono" style={{ fontSize: 10, color: "var(--faint)" }}>
                  {f.type}
                </span>
              </div>
              <p style={{ margin: 0, fontSize: 12, color: "var(--muted)" }}>{f.description}</p>
              {wiredFrom ? (
                <div
                  data-testid={`wired-${f.path}`}
                  className="mono"
                  style={{
                    fontSize: 12,
                    color: "var(--wire-xrd)",
                    background: "var(--wire-xrd-soft)",
                    borderRadius: 3,
                    padding: "3px 6px",
                    width: "fit-content",
                  }}
                >
                  ← {wiredFrom}
                </div>
              ) : (
                <>
                  <textarea
                    data-testid={`value-${f.path}`}
                    aria-label={f.path}
                    aria-invalid={error ? true : undefined}
                    rows={1}
                    value={assignment?.value ?? ""}
                    onChange={event => handleChange(f.path, event.target.value)}
                    // Folds the whole typing gesture into one undo step
                    // (fix round 1, Finding 2) — blur reliably fires before
                    // a different field's textarea gains focus, so this
                    // alone also satisfies "commit before a different field
                    // gains focus," with no separate onFocus bookkeeping.
                    onBlur={() => commitField(nodeId, f.path)}
                    style={{
                      resize: "vertical",
                      minHeight: 26,
                      padding: "4px 6px",
                      border: `1px solid ${error ? "var(--err)" : "var(--rule)"}`,
                      borderRadius: 4,
                      background: "var(--surface)",
                      color: "var(--ink)",
                      fontFamily: "var(--mono)",
                      fontSize: 12,
                    }}
                  />
                  {error && (
                    <div
                      role="alert"
                      className="mono"
                      style={{ fontSize: 11, color: "var(--err)" }}
                    >
                      {error}
                    </div>
                  )}
                </>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function filterButtonStyle(active: boolean): CSSProperties {
  return {
    padding: "3px 10px",
    fontSize: 12,
    border: `1px solid ${active ? "var(--wire-xrd)" : "var(--rule)"}`,
    borderRadius: 4,
    background: active ? "var(--wire-xrd-soft)" : "var(--surface)",
    color: "var(--ink)",
    cursor: "pointer",
  }
}
