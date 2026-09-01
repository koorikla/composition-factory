import { describe, it, expect, beforeAll, afterEach, afterAll } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { setupServer } from "msw/node"
import { http, HttpResponse } from "msw"
import { handlers } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Palette } from "./Palette"
import blueprintFixture from "../api/fixtures/blueprint.json"

// This project has no @testing-library/jest-dom (see src/setupTests.ts's own
// note on this — it's not an installed dependency, and the brief for this
// task rules out adding new ones), so `.toHaveAttribute()` is not a matcher
// vitest's `expect` knows about anywhere in this project. setupTests.ts
// already shims the one jest-dom matcher Canvas.test.tsx's given assertions
// need (`toBeInTheDocument`) directly in that file rather than pulling in
// the whole library; this task's brief restricts changes to this file and
// Palette.tsx, so the equivalent narrow shim for the one additional matcher
// this file's given assertion needs lives here instead, following the same
// pattern (mirrors jest-dom's own `toHaveAttribute(name)` semantics: name
// present, value unchecked when no second argument is given).
declare module "vitest" {
  interface Matchers<T = any> {
    toHaveAttribute(name: string, value?: string): T
  }
}

expect.extend({
  toHaveAttribute(received: unknown, name: string, value?: string) {
    const pass =
      received instanceof Element &&
      received.hasAttribute(name) &&
      (value === undefined || received.getAttribute(name) === value)
    return {
      pass,
      message: () =>
        pass
          ? `expected element not to have attribute "${name}"`
          : `expected element to have attribute "${name}"${value === undefined ? "" : ` with value "${value}"`}`,
    }
  },
})

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

describe("palette", () => {
  it("lists kinds from the API, grouped by apiVersion", async () => {
    render(<Palette />)
    // kinds.json deliberately fixtures two "Queue" entries (namespaced +
    // cluster-scoped — see the fixture's own comment and test below), so a
    // single-match query here is wrong on its face: findAllByText is the
    // correct wait, not findByText. (Coordinator ruling 2026-08-28: this is
    // a defect in the original verbatim test, not in the fixture or the
    // implementation — see task-4-report.md.)
    const queues = await screen.findAllByText("Queue")
    expect(queues.length).toBeGreaterThan(0)
    expect(screen.getByText(/sqs\.aws\.m\.upbound\.io/)).toBeInTheDocument()
  })

  it("shows the required-field count so a user can judge a kind before dropping it", async () => {
    render(<Palette />)
    // Same fixture reality as above: both Queue variants show "1 req", so
    // this waits for (and asserts) at least one match rather than exactly
    // one.
    const reqs = await screen.findAllByText(/1 req/)
    expect(reqs.length).toBeGreaterThan(0)
  })

  it("filters through the API rather than client-side, so search scales past the loaded page", async () => {
    const user = userEvent.setup()
    render(<Palette />)
    await screen.findAllByText("Queue")
    await user.type(screen.getByRole("searchbox"), "nothingmatches")
    expect(await screen.findByText(/no kinds match/i)).toBeInTheDocument()
  })

  it("marks a namespaced kind distinctly from its cluster-scoped twin", async () => {
    render(<Palette />)
    await screen.findAllByText("Queue")
    // upjet ships every MR twice; a user picking the wrong one gets fields pruned
    expect(screen.getAllByText("Queue")).toHaveLength(2)
    // The fixture also carries the native Deployment (provider "k8s"), which
    // is namespaced too — so Namespaced badges are plural, Cluster's is not.
    expect(screen.getAllByTestId("scope-Namespaced").length).toBeGreaterThan(0)
    expect(screen.getByTestId("scope-Cluster")).toBeInTheDocument()
  })

  it("is keyboard operable — a kind can be added without dragging", async () => {
    // Keyboard parity means the node actually LANDS (fix wave F1): addNode
    // writes into `doc`, so the store needs a document — without one, the
    // previous "was tabindex present" assertion passed while Enter silently
    // added nothing at all.
    useBlueprint.setState({ doc: structuredClone(blueprintFixture) as any, nodes: [], wires: [] })
    const user = userEvent.setup()
    render(<Palette />)
    const item = await screen.findByTestId("kind-sqs.aws.m.upbound.io/v1beta1-Queue")
    expect(item).toHaveAttribute("tabindex")
    item.focus()
    await user.keyboard("{Enter}")
    // Enter adds at a default position; drag-and-drop is not the only path.
    const { nodes, doc } = useBlueprint.getState()
    expect(nodes).toHaveLength(1)
    expect(nodes[0].kind).toBe("Queue")
    expect(nodes[0].apiVersion).toBe("sqs.aws.m.upbound.io/v1beta1")
    expect(doc!.spec.resources.some(r => r.name === nodes[0].name)).toBe(true)
  })

  it("renders a kinds() failure as role=\"alert\", never the empty-search copy (fix wave E6)", async () => {
    server.use(
      http.get("/api/kinds", () =>
        HttpResponse.json({ error: "index unavailable: cache directory missing" }, { status: 500 }),
      ),
    )
    render(<Palette />)
    const alert = await screen.findByRole("alert")
    // The server's own message, verbatim — not a paraphrase.
    expect(alert.textContent).toBe("index unavailable: cache directory missing")
    // And NOT the `no kinds match ""` copy, which claims the user's search
    // (empty or not) matched nothing when the index never answered at all.
    expect(screen.queryByText(/no kinds match/)).toBeNull()
  })
})
