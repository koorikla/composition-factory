import { describe, it, expect, beforeAll, afterAll, afterEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { setupServer } from "msw/node"
import { handlers, resetBlueprintFixture } from "./api/mocks"
import { useBlueprint } from "./store/blueprint"
import App from "./App"

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterEach(() => {
  server.resetHandlers()
  resetBlueprintFixture()
  useBlueprint.setState({ doc: null, nodes: [], wires: [] })
})
afterAll(() => server.close())

const queueKind = {
  kind: "Queue", group: "sqs.aws.m.upbound.io", version: "v1beta1",
  apiVersion: "sqs.aws.m.upbound.io/v1beta1", plural: "queues",
  scope: "Namespaced" as const, provider: "p", namespaced: true, required: 1, fields: 18,
}

/** App.load() populates the store from MSW, then Canvas's hydration effect
 * asynchronously gives the fixture's one resource ("main-queue") a node —
 * wait for BOTH so a late hydration can't skew a node count mid-test. */
async function renderAppAndSettle() {
  render(<App />)
  await waitFor(() => expect(useBlueprint.getState().doc).not.toBeNull())
  await waitFor(() => expect(useBlueprint.getState().nodes).toHaveLength(1))
}

describe("app shell undo keybinding (fix wave A5)", () => {
  it("Ctrl+Z anywhere outside a text control calls the store's undo", async () => {
    await renderAppAndSettle()

    useBlueprint.getState().addNode(queueKind, 10, 10)
    expect(useBlueprint.getState().nodes).toHaveLength(2)

    fireEvent.keyDown(document.body, { key: "z", ctrlKey: true })
    expect(useBlueprint.getState().nodes).toHaveLength(1)
  })

  it("Cmd+Z (metaKey) works too", async () => {
    await renderAppAndSettle()

    useBlueprint.getState().addNode(queueKind, 10, 10)
    expect(useBlueprint.getState().nodes).toHaveLength(2)
    fireEvent.keyDown(document.body, { key: "z", metaKey: true })
    expect(useBlueprint.getState().nodes).toHaveLength(1)
  })

  it("Ctrl+Z inside a text control is left to the browser's own text undo", async () => {
    await renderAppAndSettle()

    useBlueprint.getState().addNode(queueKind, 10, 10)
    expect(useBlueprint.getState().nodes).toHaveLength(2)

    // The palette's search box is a real <input>; a keydown targeted there
    // bubbles to window but the handler must skip it.
    const searchbox = await screen.findByRole("searchbox")
    searchbox.focus()
    fireEvent.keyDown(searchbox, { key: "z", ctrlKey: true })
    expect(useBlueprint.getState().nodes).toHaveLength(2)
  })
})
