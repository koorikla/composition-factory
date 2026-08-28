import { describe, it, expect, beforeAll, afterEach, afterAll, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { setupServer } from "msw/node"
import { http, HttpResponse } from "msw"
import { handlers } from "../api/mocks"
import { useBlueprint } from "../store/blueprint"
import { Inspector, checkScalar } from "./Inspector"
import blueprintFixture from "../api/fixtures/blueprint.json"

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
  useBlueprint.getState().addNode(queueKind, 0, 0)
})

describe("inspector", () => {
  it("opens on required fields only — an EC2 Instance has 263 properties", async () => {
    render(<Inspector nodeId={useBlueprint.getState().nodes[0].id} />)
    expect(await screen.findByText("region")).toBeInTheDocument()
    expect(screen.queryByText("delaySeconds")).not.toBeInTheDocument()
    // The required marker announces itself through real ARIA — role="img"
    // is what makes the aria-label live on a <span> (fix wave E5).
    expect(screen.getByRole("img", { name: "required" })).toBeInTheDocument()
  })

  it("fails closed when fields() fails: role=\"alert\" with the server's message, no editable inputs (fix wave E6)", async () => {
    server.use(
      http.get("/api/kinds/:apiVersion/:kind/fields", () =>
        HttpResponse.json({ error: "schema cache unreadable" }, { status: 500 }),
      ),
    )
    render(<Inspector nodeId={useBlueprint.getState().nodes[0].id} />)
    const alert = await screen.findByTestId("fields-error")
    expect(alert.getAttribute("role")).toBe("alert")
    // Verbatim server message, not a paraphrase.
    expect(alert.textContent).toBe("schema cache unreadable")
    // Fail-closed: no field list was fetched, so no editable input may
    // render — typing into a field this panel never saw the schema for is
    // exactly what "closed" forbids.
    expect(screen.queryByTestId("value-region")).toBeNull()
    expect(document.querySelector("textarea")).toBeNull()
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

// Coordinator fix round 1: four Important findings on setField/commitField
// and the checkScalar mirror, all reviewer-verified empirically.
describe("inspector (fix round 1)", () => {
  it("Finding 2 (the undo trap): type, blur, undo restores the pre-edit field and leaves the node intact", async () => {
    const user = userEvent.setup()
    const id = useBlueprint.getState().nodes[0].id
    const name = useBlueprint.getState().nodes[0].name
    render(<Inspector nodeId={id} />)
    const input = await screen.findByTestId("value-region")

    await user.type(input, "eu-north-1")
    fireEvent.blur(input)

    let res = useBlueprint.getState().doc!.spec.resources.find(r => r.name === name)!
    expect(res.fields["region"]).toEqual({ value: "eu-north-1" })

    // Before the fix, setField pushed no history at all, so this undo()
    // would instead pop addNode's own entry -- the node disappearing, not
    // the field reverting.
    useBlueprint.getState().undo()

    expect(useBlueprint.getState().nodes.find(n => n.id === id)).toBeDefined()
    res = useBlueprint.getState().doc!.spec.resources.find(r => r.name === name)!
    expect(res.fields["region"]).toBeUndefined()
  })

  describe("Finding 4: checkScalar's rune quoting matches Go's fmt %q exactly for the detected set", () => {
    const cases: Array<[string, number, string]> = [
      ["BEL (0x07)", 0x07, "'\\a'"],
      ["backspace (0x08)", 0x08, "'\\b'"],
      ["vertical tab (0x0b)", 0x0b, "'\\v'"],
      ["form feed (0x0c)", 0x0c, "'\\f'"],
      ["an unnamed C0 control (0x01)", 0x01, "'\\x01'"],
      ["DEL (0x7f)", 0x7f, "'\\x7f'"],
      ["a C1 control / NEL (0x85)", 0x85, "'\\u0085'"],
      ["line separator U+2028", 0x2028, "'\\u2028'"],
      ["paragraph separator U+2029", 0x2029, "'\\u2029'"],
    ]

    it.each(cases)("%s quotes as %s", (_label, codePoint, expectedQuoted) => {
      const ch = String.fromCodePoint(codePoint)
      const message = checkScalar("queue", "region", ch)
      expect(message).not.toBeNull()
      expect(message).toContain(`contains the control character ${expectedQuoted}`)
    })
  })
})
