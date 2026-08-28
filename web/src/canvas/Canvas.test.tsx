import { describe, it, expect, beforeEach, beforeAll, afterAll } from "vitest"
import { render, screen, within, waitFor, fireEvent } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { setupServer } from "msw/node"
import { handlers } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Canvas } from "./Canvas"
import blueprintFixture from "../api/fixtures/blueprint.json"

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterAll(() => server.close())

const queueKind = {
  kind: "Queue", group: "sqs.aws.m.upbound.io", version: "v1beta1",
  apiVersion: "sqs.aws.m.upbound.io/v1beta1", plural: "queues",
  scope: "Namespaced" as const, provider: "p", namespaced: true, required: 1, fields: 18,
}

beforeEach(() => {
  useBlueprint.setState({ doc: structuredClone(blueprintFixture) as any, nodes: [], wires: [] })
})

describe("canvas", () => {
  it("renders one node per resource in the document", async () => {
    useBlueprint.getState().addNode(queueKind, 40, 40)
    render(<Canvas />)
    expect(await screen.findByText("Queue")).toBeInTheDocument()
  })

  it("renders the XR node showing the XRD parameters as output ports", async () => {
    render(<Canvas />)
    const xr = await screen.findByTestId("node-xr")
    expect(within(xr).getByText("providerName")).toBeInTheDocument()
  })

  it("shows a required field marker that is not conveyed by colour alone", async () => {
    render(<Canvas />)
    const xr = await screen.findByTestId("node-xr")
    // an asterisk, a 'req' badge or aria — something a screen reader and a
    // colour-blind user both get
    expect(within(xr).getByTestId("required-marker")).toBeInTheDocument()
  })

  it("deletes the selected node on Delete, removing its resource", async () => {
    const user = userEvent.setup()
    useBlueprint.getState().addNode(queueKind, 40, 40)
    render(<Canvas />)
    await user.click(await screen.findByText("Queue"))
    await user.keyboard("{Delete}")
    expect(useBlueprint.getState().nodes).toHaveLength(0)
  })

  it("is keyboard reachable — nodes are focusable", async () => {
    useBlueprint.getState().addNode(queueKind, 40, 40)
    render(<Canvas />)
    const node = await screen.findByTestId(/^node-/)
    expect(node.getAttribute("tabindex")).not.toBeNull()
  })
})

describe("fix round 1, Finding 1 — hydration re-arms on load(), not just on Canvas mount", () => {
  it("re-hydrates after a second load(), and never resurrects a node the user deleted", async () => {
    // Start with no document at all, so Canvas's own hydration effect does
    // nothing on mount — the probe below drives load() itself, the same
    // way opening (and later re-opening) a blueprint would in the real app.
    useBlueprint.setState({ doc: null, nodes: [], wires: [] })
    render(<Canvas />)
    expect(useBlueprint.getState().nodes).toHaveLength(0)

    await useBlueprint.getState().load()
    await waitFor(() => expect(useBlueprint.getState().nodes.length).toBeGreaterThan(0))
    const firstId = useBlueprint.getState().nodes[0].id
    expect(useBlueprint.getState().nodes[0].name).toBe("main-queue")

    // Delete the hydrated node, then make an unrelated edit — this is
    // exactly the sequence that used to silently resurrect the deleted
    // node once the old mount-only gate saw nodes.length pass back through
    // zero and misread that as "a fresh, unhydrated load."
    useBlueprint.getState().removeNode(firstId)
    expect(useBlueprint.getState().nodes).toHaveLength(0)
    useBlueprint.getState().addNode(queueKind, 1, 1)
    // give any errant hydration effect a full task-queue turn to (wrongly) fire
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(useBlueprint.getState().nodes).toHaveLength(1)
    expect(useBlueprint.getState().nodes[0].name).not.toBe("main-queue")

    // A second load() — the "open a different blueprint" flow, without
    // remounting Canvas — must re-hydrate: load() always resets `nodes`,
    // and the fixture's one resource must get a node again.
    await useBlueprint.getState().load()
    await waitFor(() => expect(useBlueprint.getState().nodes.length).toBeGreaterThan(0))
    expect(useBlueprint.getState().nodes[0].name).toBe("main-queue")
  })
})

describe("fix round 1, Finding 2 — incompatible drops are refused visibly, not silently", () => {
  it("announces an incompatible connection attempt through the aria-live region, and does not connect", async () => {
    // main-queue (from the fixture) already has maxMessageSize bound, so
    // its ResourceNode renders a "maxMessageSize" port (type: number)
    // without an extra load()+hydrate round trip — give it a node directly,
    // positioned well clear of the XR node.
    const NODE_X = 400
    const NODE_Y = 40
    useBlueprint.setState({
      nodes: [
        { id: "n1", kind: "Queue", apiVersion: "sqs.aws.m.upbound.io/v1beta1", name: "main-queue", x: NODE_X, y: NODE_Y },
      ],
    })
    render(<Canvas />)
    const xr = await screen.findByTestId("node-xr")
    const resourceNode = await screen.findByTestId("resource-n1")
    const fromHandle = xr.querySelector('[data-handleid="providerName"]') as HTMLElement
    expect(fromHandle).toBeTruthy()
    // ResourceNode fetches its fields asynchronously (see ResourceNode.tsx)
    // and only renders a <Handle> once they've loaded — wait for the real
    // one rather than the port label alone, since the drag below needs the
    // actual handle DOM node.
    let toHandle: HTMLElement | null = null
    await waitFor(() => {
      toHandle = resourceNode.querySelector('[data-handleid="maxMessageSize"]')
      expect(toHandle).toBeTruthy()
    })

    // jsdom computes no real layout — every element's measured size is
    // fixed and identical (see setupTests.ts's ResizeObserver/
    // getBoundingClientRect polyfills, both pinned to a 120x120 square) —
    // so xyflow's own "closest handle by distance" search resolves EVERY
    // handle on a node to that node's own (x, y) position offset by
    // (60, 60), the centre of that fixed square, regardless of which
    // literal handle it is. Landing the simulated pointer there is what
    // makes xyflow's real geometry search (not just this test's manual
    // `document.elementFromPoint` override — jsdom doesn't implement that
    // API at all either, see setupTests.ts) agree that a handle is nearby,
    // which is what lets `isValidConnection`'s `false` reach a definite
    // rejection (`FinalConnectionState.isValid === false`) instead of an
    // ambiguous "no candidate" (`null`) — confirmed empirically: at any
    // other simulated position, xyflow reports `null`, not `false`, and
    // onConnectEnd never fires a rejection. This drives the exact same
    // XYHandle pointer pipeline the app uses in a real browser end to end;
    // what's genuinely NOT testable under jsdom is the CSS hover ring
    // itself (jsdom computes no :hover or geometry-dependent styling at
    // all) — the assertion below, on the aria-live text, is the
    // accessible, colour-independent half the brief actually requires.
    const targetX = NODE_X + 60
    const targetY = NODE_Y + 60
    const original = document.elementFromPoint
    document.elementFromPoint = () => toHandle
    try {
      fireEvent.mouseDown(fromHandle, { button: 0, clientX: 0, clientY: 0 })
      fireEvent.mouseMove(document, { clientX: targetX, clientY: targetY })
      fireEvent.mouseUp(document, { clientX: targetX, clientY: targetY })
    } finally {
      document.elementFromPoint = original
    }

    const status = await screen.findByRole("status")
    await waitFor(() => expect(status.textContent).toBe("providerName → maxMessageSize: incompatible"))
    // the incompatible connection must not actually have been made
    expect(useBlueprint.getState().wires).toHaveLength(0)
  })
})
