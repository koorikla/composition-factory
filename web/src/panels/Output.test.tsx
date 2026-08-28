import { describe, it, expect, beforeAll, afterAll, afterEach } from "vitest"
import { render, screen } from "@testing-library/react"
import { setupServer } from "msw/node"
import { handlers, failGenerate } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Output } from "./Output"
import blueprintFixture from "../api/fixtures/blueprint.json"
import generateFixture from "../api/fixtures/generate.json"

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
    // Task 7: the view now renders each output's real generated `body`
    // (updated from an earlier path+bytes-only manifest) — an exact-text
    // match on the path heading, rather than a loose /composition/ regex,
    // since a realistic YAML body legitimately contains substrings like
    // "compositionfactory" (a label value) that a loose match would also
    // catch, per the exact-match convention this project settled on for
    // this exact class of collision (see progress.md, Task 4).
    await screen.findByTestId("yaml-view")
    expect(screen.getByText("composition.yaml")).toBeInTheDocument()
    expect(screen.getByText("functions.yaml")).toBeInTheDocument()
  })

  it("is read-only — generated output is never hand-edited, and says so through real ARIA", async () => {
    render(<Output />)
    // aria-readonly on a role-less div is inert ARIA (fix wave E4): the
    // pane is a labelled region instead, which assistive tech actually
    // announces, and its content is <pre> — read-only by construction.
    const editor = await screen.findByRole("region", { name: "Generated YAML (read-only)" })
    expect(editor.getAttribute("data-testid")).toBe("yaml-view")
    expect(editor.getAttribute("aria-readonly")).toBeNull()
  })

  it("surfaces a generation failure instead of showing stale YAML", async () => {
    server.use(failGenerate())
    render(<Output />)
    expect(await screen.findByRole("alert")).toBeInTheDocument()
  })
})

// Task 7: the server now sends each output's actual generated YAML (`body`,
// byte-for-byte what the engine writes — see api/contract.ts's
// GenerateOutput.body doc comment). These cases exercise that the pane
// renders that real content verbatim, not the path+bytes-only manifest it
// used to synthesize.
describe("output pane — real YAML bodies", () => {
  it("renders each output's generated body verbatim, including its comment line", async () => {
    render(<Output />)
    const view = await screen.findByTestId("yaml-view")
    for (const output of generateFixture.outputs) {
      // An exact multi-line substring match (not a loose keyword check) —
      // this proves the whole body, comment line included, made it through
      // unaltered, not just some recognizable fragment of it.
      expect(view.textContent).toContain(output.body)
    }
  })

  it("preserves whitespace exactly — a rendered body has no trimming or reformatting", async () => {
    render(<Output />)
    await screen.findByTestId("yaml-view")
    const pres = screen.getAllByTestId("yaml-body")
    expect(pres).toHaveLength(generateFixture.outputs.length)
    // Strict equality against the raw fixture string, not just `.includes` —
    // this catches a stray trim(), an added/dropped trailing newline, or any
    // other whitespace massaging the brief rules out.
    expect(pres[0].textContent).toBe(generateFixture.outputs[0].body)
  })

  it("shows a heading naming each output's path", async () => {
    render(<Output />)
    await screen.findByTestId("yaml-view")
    for (const output of generateFixture.outputs) {
      expect(screen.getByText(output.path)).toBeInTheDocument()
    }
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
