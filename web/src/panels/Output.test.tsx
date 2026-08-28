import { describe, it, expect, beforeAll, afterAll, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { setupServer } from "msw/node"
import { handlers, failGenerate } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Output } from "./Output"
import blueprintFixture from "../api/fixtures/blueprint.json"

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterEach(() => {
  server.resetHandlers()
  // This file's own last two tests seed `doc` to drive the PUT-then-generate
  // refresh flow; reset it so a later test in this file doesn't inherit a
  // still-set document from an earlier one (this module's `useBlueprint`
  // instance is shared across every test in this file).
  useBlueprint.setState({ doc: null, nodes: [], wires: [] })
})
afterAll(() => server.close())

describe("output pane", () => {
  it("shows the generated artifacts the server reports", async () => {
    render(<Output />)
    expect(await screen.findByText(/composition/)).toBeInTheDocument()
    expect(screen.getByText(/functions\.yaml/)).toBeInTheDocument()
  })

  it("is read-only — generated output is never hand-edited", async () => {
    render(<Output />)
    const editor = await screen.findByTestId("yaml-view")
    expect(editor.getAttribute("aria-readonly")).toBe("true")
  })

  it("surfaces a generation failure instead of showing stale YAML", async () => {
    server.use(failGenerate())
    render(<Output />)
    expect(await screen.findByRole("alert")).toBeInTheDocument()
  })
})

// Contract extension (landed after this task's brief was written): a
// refresh is PUT /api/blueprint then POST /api/generate — see
// api/contract.ts's putBlueprint and this file's Output.tsx header comment.
// These two cases exercise the half the three verbatim tests above don't:
// an actual document flowing through PUT, and a PUT failure specifically
// (as opposed to a generate failure) still surfacing as an alert rather
// than stale output.
describe("output pane — PUT/generate refresh flow", () => {
  it("PUTs the store's current document before regenerating, so the preview reflects canvas edits", async () => {
    const doc = structuredClone(blueprintFixture) as any
    doc.metadata.name = "renamed-in-store"
    useBlueprint.setState({ doc })
    render(<Output />)
    await screen.findByTestId("yaml-view")
    const saved = await fetch("/api/blueprint").then(r => r.json())
    expect(saved.metadata.name).toBe("renamed-in-store")
  })

  it("surfaces a PUT failure exactly like a generate failure — never stale YAML", async () => {
    const doc = structuredClone(blueprintFixture) as any
    doc.spec.xrd.scope = "Cluster" // the mock's stand-in for "the engine rejected this document"
    useBlueprint.setState({ doc })
    render(<Output />)
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toMatch(/Cluster/)
    expect(screen.queryByTestId("yaml-view")).not.toBeInTheDocument()
  })
})
