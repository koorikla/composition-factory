// A small shared cache: each ResourceNode fetches its OWN fields lazily
// (see ResourceNode.tsx) and reports them here so Canvas's global
// `isValidConnection` (see Canvas.tsx) can look up a target field's `type`
// without needing its own fetch, and without lifting every node's fetch up
// into Canvas. Write side (ResourceNode) goes through the context function;
// read side (Canvas) reads the same ref directly, since it's an imperative
// lookup during a pointer drag, not something that should trigger a
// re-render every time any node's fields load.
import { createContext } from "react"
import type { Field } from "../api/contract"

export type ReportFields = (nodeId: string, fields: Field[]) => void

export const FieldsCacheContext = createContext<ReportFields>(() => {})
