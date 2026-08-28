import { describe, it, expect, beforeEach, beforeAll, afterAll } from "vitest"
import { render, screen, within } from "@testing-library/react"
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
