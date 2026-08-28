import { describe, it, expect, beforeEach, beforeAll, afterEach, afterAll } from "vitest"
import { render, screen, within, waitFor, fireEvent } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { setupServer } from "msw/node"
import { http, HttpResponse } from "msw"
import { handlers } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Canvas } from "./Canvas"
import blueprintFixture from "../api/fixtures/blueprint.json"
import kindsFixture from "../api/fixtures/kinds.json"

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterEach(() => server.resetHandlers())
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
    // colour-blind user both get. role="img" is what makes the aria-label
    // real: on a bare <span> it is inert ARIA (fix wave E5).
    const marker = within(xr).getByRole("img", { name: "required" })
    expect(marker.getAttribute("data-testid")).toBe("required-marker")
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
    // BOTH node types, not just the XR node: /^node-/ used to match only
    // "node-xr", so a resource node losing its tabindex passed unnoticed
    // (fix wave B3).
    const nodes = await screen.findAllByTestId(/^(node-xr|resource-)/)
    expect(nodes.length).toBe(2)
    for (const node of nodes) {
      expect(node.getAttribute("tabindex")).not.toBeNull()
    }
  })

  it("renders selection visibly on the selected node's body, tokens only (fix wave B3)", async () => {
    const user = userEvent.setup()
    useBlueprint.getState().addNode(queueKind, 40, 40)
    render(<Canvas />)
    const node = await screen.findByTestId(/^resource-/)
    expect(node.getAttribute("data-selected")).toBeNull()

    await user.click(node)
    await waitFor(() => expect(node.getAttribute("data-selected")).toBe("true"))
    // The selected border/shadow come from tokens, never literals.
    expect(node.getAttribute("style")).toContain("var(--wire-xrd)")
    expect(node.getAttribute("style")).toContain("var(--shadow-lg)")
  })

  it("colours the grid from the --grid token in both themes (fix wave B2)", async () => {
    render(<Canvas />)
    await screen.findByTestId("node-xr")
    const background = document.querySelector('[data-testid="rf__background"]') as SVGElement
    expect(background).not.toBeNull()
    expect(background.style.getPropertyValue("--xy-background-pattern-color-props")).toBe(
      "var(--grid)",
    )
  })

  it("overrides xyflow's vendor handle colours with theme tokens (fix wave B4)", async () => {
    const { container } = render(<Canvas />)
    await screen.findByTestId("node-xr")
    const styleTag = container.querySelector("style")!
    expect(styleTag.textContent).toContain("--xy-handle-background-color: var(--rule-2);")
    expect(styleTag.textContent).toContain("--xy-handle-border-color: var(--surface);")
  })

  describe("drop-to-create", () => {
    // A resource-free document: the fixture's own "main-queue" resource
    // would otherwise race Canvas's own async hydration effect (it fetches
    // api.kinds() then calls hydrateNodes() — see Canvas.tsx), which can
    // land a node before this test's own assertions run and make "zero
    // nodes before the drop" flaky. Starting from zero resources sidesteps
    // that race entirely: hydrateNodes() has nothing to hydrate.
    beforeEach(() => {
      const empty = structuredClone(blueprintFixture) as any
      empty.spec.resources = []
      useBlueprint.setState({ doc: empty, nodes: [], wires: [] })
    })

    it("adds a resource when a palette kind is dropped onto the canvas", async () => {
      // Mirrors Palette.tsx's KindRow.onDragStart exactly: the whole Kind,
      // JSON-serialized onto this one MIME type — Canvas's onDrop is the
      // only consumer of it.
      const dataTransfer = {
        getData: (type: string) =>
          type === "application/x-compositionfactory-kind" ? JSON.stringify(queueKind) : "",
        dropEffect: "",
      } as unknown as DataTransfer
      const { container } = render(<Canvas />)
      await screen.findByTestId("node-xr")
      expect(useBlueprint.getState().nodes).toHaveLength(0)
      const dropTarget = container.firstElementChild as HTMLElement
      fireEvent.dragOver(dropTarget, { dataTransfer })
      fireEvent.drop(dropTarget, { dataTransfer, clientX: 300, clientY: 150 })
      expect(useBlueprint.getState().nodes).toHaveLength(1)
      const added = useBlueprint.getState().nodes[0]
      expect(added.kind).toBe("Queue")
      expect(added.apiVersion).toBe(queueKind.apiVersion)
      // The resource the node represents landed in the document too — a
      // dropped kind isn't just a visual node, it's the same addNode() the
      // palette's Enter-to-add keyboard path already uses
      // (store/blueprint.ts).
      expect(useBlueprint.getState().doc!.spec.resources.some(r => r.name === added.name)).toBe(true)
    })

    it("ignores a drop that doesn't carry the palette's kind payload", async () => {
      const dataTransfer = {
        getData: () => "",
        dropEffect: "",
      } as unknown as DataTransfer
      const { container } = render(<Canvas />)
      await screen.findByTestId("node-xr")
      const dropTarget = container.firstElementChild as HTMLElement
      fireEvent.drop(dropTarget, { dataTransfer, clientX: 300, clientY: 150 })
      expect(useBlueprint.getState().nodes).toHaveLength(0)
    })
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

describe("fix wave B1 — hydration keyed on loadEpoch alone", () => {
  it("a doc-identity change while kinds() is in flight does not abandon hydration for that epoch", async () => {
    // Gate the kinds response with a deferred promise (no fake timers, per
    // repo convention) so the hydration fetch is verifiably in flight when
    // the document changes identity underneath it.
    let releaseKinds!: () => void
    const gate = new Promise<void>(resolve => {
      releaseKinds = resolve
    })
    server.use(
      http.get("/api/kinds", async () => {
        await gate
        return HttpResponse.json(kindsFixture)
      }),
    )

    // The fixture doc has one resource ("main-queue") and zero nodes, so the
    // mount-time hydration effect starts a kinds() fetch — now held open.
    render(<Canvas />)

    // An ordinary edit gives `doc` a brand-new identity via immer while the
    // fetch is pending. Keyed on doc identity, this used to re-run the
    // effect: the cleanup cancelled the in-flight fetch and the re-entry saw
    // the epoch already marked, permanently abandoning hydration.
    useBlueprint.getState().addNode(queueKind, 1, 1)
    expect(useBlueprint.getState().nodes).toHaveLength(1)

    releaseKinds()
    await waitFor(() =>
      expect(useBlueprint.getState().nodes.some(n => n.name === "main-queue")).toBe(true),
    )
  })
})

describe("fix wave B5 — a wired field always ranks into the visible port set", () => {
  it("renders the port handle and the edge for a wired field ranked past MAX_VISIBLE_PORTS", async () => {
    // The fixture's main-queue already carries one wired field
    // (maxMessageSize). Wire seven more non-required fields so the wired
    // count alone exceeds the six-port cap — under plain slice(0, 6)
    // truncation the later wired fields lose their <Handle> and their edges
    // silently stop rendering.
    useBlueprint.setState({
      nodes: [
        { id: "n1", kind: "Queue", apiVersion: "sqs.aws.m.upbound.io/v1beta1", name: "main-queue", x: 400, y: 40 },
      ],
    })
    const extraPaths = [
      "contentBasedDeduplication",
      "deduplicationScope",
      "delaySeconds",
      "fifoQueue",
      "fifoThroughputLimit",
      "kmsDataKeyReusePeriodSeconds",
      "kmsMasterKeyId",
    ]
    for (const path of extraPaths) {
      useBlueprint.getState().connect("maxMessageSize", "n1", path)
    }
    expect(useBlueprint.getState().wires).toHaveLength(8)

    render(<Canvas />)
    const resourceNode = await screen.findByTestId("resource-n1")

    // Every wired field's handle is present — including the last-ranked one.
    await waitFor(() => {
      for (const path of ["maxMessageSize", ...extraPaths]) {
        expect(resourceNode.querySelector(`[data-handleid="${path}"]`)).not.toBeNull()
      }
    })

    // The bound markers announce themselves through real ARIA (fix wave
    // E5): role="img" + aria-label, one per wired port row.
    expect(within(resourceNode).getAllByRole("img", { name: "bound" })).toHaveLength(8)

    // And the edge itself renders for the wired field ranked past the cap.
    await waitFor(() => {
      expect(document.querySelector('.react-flow__edge[data-id="n1:kmsMasterKeyId"]')).not.toBeNull()
    })
  })
})
