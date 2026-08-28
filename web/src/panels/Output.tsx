// The output pane: renders exactly what POST /api/generate reports, and
// nothing else — it never generates locally.
//
// The canvas document lives client-side (in the store), but /api/generate
// re-reads the document from DISK. Without pushing the store's current doc
// first, the preview would silently ignore everything the user just did on
// the canvas — so a refresh is always two calls, in order: PUT /api/blueprint
// (the full-document replace this task adds — see api/contract.ts's
// putBlueprint doc comment), then POST /api/generate {"write":false}. Either
// leg can fail (PUT on a document the engine's own validation rejects,
// generate on the same underlying document); either failure surfaces as an
// alert and the previous successful output is hidden, never left on screen
// looking current — showing yesterday's YAML next to today's error is
// exactly the silent-wrongness class `cf gen --check` exists to catch, and
// this pane must not reproduce it. Regeneration is triggered by the store's
// `doc` changing (see the effect below) and debounced: the Inspector calls
// `setField` on every keystroke, and firing a PUT+generate round trip per
// keystroke would be both wasteful and visibly janky.
//
// The generated view is a read-only CodeMirror instance for the same reason
// real generated files carry a do-not-edit header: hand-editing generated
// output is exactly the drift `cf gen --check` exists to detect. Note what
// GenerateResult (the frozen /api/generate response shape) actually
// contains: an output's `path` and `bytes`, never its text — there is no
// "generated YAML" in this contract to display verbatim. So the read-only
// view renders that metadata as a small YAML manifest of its own, rather
// than inventing file content the server never sent.
import { useEffect, useRef, useState } from "react"
import { EditorState } from "@codemirror/state"
import { EditorView } from "@codemirror/view"
import { yaml } from "@codemirror/lang-yaml"
import { minimalSetup } from "codemirror"
import { api, ApiError } from "../api/contract"
import type { GenerateResult } from "../api/contract"
import { useBlueprint } from "../store/blueprint"

// Long enough that a burst of store edits collapses into one round trip
// instead of one per keystroke (mirrors Palette.tsx's SEARCH_DEBOUNCE_MS);
// short enough that the preview still feels live.
const REGENERATE_DEBOUNCE_MS = 200

/** Renders a GenerateResult as a small read-only YAML manifest: the frozen
 * contract's GenerateResult carries only output metadata (path + bytes), so
 * this is a manifest OF that metadata, not a reconstruction of file
 * contents the server never sent. */
function manifestYaml(result: GenerateResult): string {
  const lines = [
    "# generated -- do not hand-edit; drift here is what `cf gen --check` catches",
    "outputs:",
    ...result.outputs.flatMap(o => [`  - path: ${o.path}`, `    bytes: ${o.bytes}`]),
    `written: ${result.written}`,
    "",
  ]
  return lines.join("\n")
}

interface YamlViewProps {
  value: string
}

/** A minimal, read-only CodeMirror mount. No React wrapper package is
 * installed for CodeMirror (the brief rules out new deps), so this wires
 * @codemirror/view directly: one EditorView is created once per mount, and
 * a later `value` change is applied via dispatch() rather than tearing the
 * view down and rebuilding it — the ordinary "update, don't remount" rule
 * for any imperative widget wrapped for React. */
function YamlView({ value }: YamlViewProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const viewRef = useRef<EditorView | null>(null)

  useEffect(() => {
    if (!containerRef.current) return
    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          minimalSetup,
          yaml(),
          // Belt and braces: EditorState.readOnly blocks every transaction
          // that isn't explicitly flagged to bypass it (the only path a
          // generated-output view has no business exposing), and
          // EditorView.editable additionally drops contenteditable from the
          // DOM itself, so this is not just "edits are rejected" but "there
          // is nothing here inviting a click-and-type in the first place."
          EditorState.readOnly.of(true),
          EditorView.editable.of(false),
        ],
      }),
      parent: containerRef.current,
    })
    viewRef.current = view
    return () => {
      view.destroy()
      viewRef.current = null
    }
    // Deliberately mount-once: subsequent `value` changes go through the
    // dispatch effect below, not a remount (which would lose scroll
    // position for no reason on every regenerate).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const view = viewRef.current
    if (!view || view.state.doc.toString() === value) return
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
  }, [value])

  return (
    <div
      data-testid="yaml-view"
      aria-readonly="true"
      ref={containerRef}
      className="mono"
      style={{
        fontSize: 12,
        border: "1px solid var(--rule)",
        borderRadius: 4,
        background: "var(--surface)",
        overflow: "auto",
        maxHeight: 220,
      }}
    />
  )
}

function messageOf(e: unknown, fallback: string): string {
  if (e instanceof ApiError) return e.message
  return fallback
}

export function Output() {
  const doc = useBlueprint(s => s.doc)
  const [result, setResult] = useState<GenerateResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Debounced on `doc` — the store's current document is the trigger for
  // every regenerate, exactly as an addNode/connect/setField edit makes it.
  useEffect(() => {
    let cancelled = false
    const timer = setTimeout(() => {
      async function refresh() {
        if (doc) {
          try {
            await api.putBlueprint(doc)
          } catch (e) {
            if (cancelled) return
            // A failed PUT never reaches /api/generate at all — surfaced
            // exactly like a generate failure: an alert, and the previous
            // successful output (if any) is hidden, never left showing.
            setError(messageOf(e, "failed to save the document"))
            setResult(null)
            return
          }
        }
        try {
          const generated = await api.generate(false)
          if (cancelled) return
          setResult(generated)
          setError(null)
        } catch (e) {
          if (cancelled) return
          setError(messageOf(e, "generation failed"))
          setResult(null)
        }
      }
      refresh()
    }, REGENERATE_DEBOUNCE_MS)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [doc])

  return (
    <div
      data-testid="output"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 8,
        padding: 10,
        height: "100%",
        overflow: "auto",
        background: "var(--surface-2)",
        color: "var(--ink)",
        fontFamily: "var(--sans)",
      }}
    >
      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <span style={{ fontWeight: 600 }}>Output</span>
        <span className="mono" style={{ fontSize: 10, color: "var(--faint)" }}>
          write: false
        </span>
      </div>
      {error ? (
        <div role="alert" className="mono" style={{ fontSize: 12, color: "var(--err)" }}>
          {error}
        </div>
      ) : (
        result && <YamlView value={manifestYaml(result)} />
      )}
    </div>
  )
}
