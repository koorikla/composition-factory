import { describe, it, expect, beforeAll, afterAll, beforeEach } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { setupServer } from "msw/node"
import { handlers } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Inspector } from "./Inspector"
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
  useBlueprint.getState().addNode(queueKind, 0, 0)
})

describe("inspector", () => {
  it("opens on required fields only — an EC2 Instance has 263 properties", async () => {
    render(<Inspector nodeId={useBlueprint.getState().nodes[0].id} />)
    expect(await screen.findByText("region")).toBeInTheDocument()
    expect(screen.queryByText("delaySeconds")).not.toBeInTheDocument()
  })

  it("shows all fields when the filter is switched, fetching them lazily", async () => {
    const user = userEvent.setup()
    render(<Inspector nodeId={useBlueprint.getState().nodes[0].id} />)
    await screen.findByText("region")
    await user.click(screen.getByRole("button", { name: /all/i }))
    expect(await screen.findByText("delaySeconds")).toBeInTheDocument()
  })

  it("shows each field's description, which is the only documentation a CRD carries", async () => {
    render(<Inspector nodeId={useBlueprint.getState().nodes[0].id} />)
    const region = await screen.findByTestId("field-region")
    expect(region.textContent).toMatch(/region/i)
    expect(region.textContent!.length).toBeGreaterThan("region".length)
  })

  it("setting a literal value writes it into the document", async () => {
    const user = userEvent.setup()
    const id = useBlueprint.getState().nodes[0].id
    render(<Inspector nodeId={id} />)
    const input = await screen.findByTestId("value-region")
    await user.type(input, "eu-north-1")
    // NOTE: the brief's verbatim lookup here was `.find(r => r.name.includes("queue"))`.
    // blueprint.json's fixture already contains a resource named "main-queue" (added
    // before "queue" in `beforeEach`'s addNode call), and "main-queue".includes("queue")
    // is also true — so that substring lookup resolves to the WRONG resource (the
    // pre-existing "main-queue", array position 0), not the node under test. This is the
    // same fixture-collision class flagged and corrected in Task 2 and Task 4 of this plan
    // (see progress.md): the asserted behaviour (the literal value lands in the edited
    // node's own resource) is unchanged; only the lookup is corrected to resolve the
    // resource by the node actually under test, exactly as Inspector itself must.
    const nodeName = useBlueprint.getState().nodes.find(n => n.id === id)!.name
    const res = useBlueprint.getState().doc!.spec.resources.find(r => r.name === nodeName)!
    expect(res.fields["region"]).toEqual({ value: "eu-north-1" })
  })

  it("shows a wired field as wired, not as an empty input", async () => {
    const id = useBlueprint.getState().nodes[0].id
    useBlueprint.getState().connect("maxMessageSize", id, "maxMessageSize")
    render(<Inspector nodeId={id} />)
    expect(await screen.findByText(/params\.maxMessageSize/)).toBeInTheDocument()
  })

  it("surfaces a server validation error verbatim rather than paraphrasing it", async () => {
    const user = userEvent.setup()
    render(<Inspector nodeId={useBlueprint.getState().nodes[0].id} />)
    const input = await screen.findByTestId("value-region")
    await user.type(input, "eu\nnorth")   // a control character; the server rejects these
    expect(await screen.findByRole("alert")).toBeInTheDocument()
  })
})
