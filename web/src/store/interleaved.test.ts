// Interleaved-undo coverage added at task close-out: the individual fix-round
// tests pin each behaviour in isolation; these pin the ORDERING when field
// commits and structural actions interleave — the overshoot class the undo
// trap fix exists to prevent.
import { describe, it, expect, beforeEach } from "vitest"
import { useBlueprint } from "./blueprint"
import blueprintFixture from "../api/fixtures/blueprint.json"

const qk = { kind: "Queue", group: "sqs.aws.m.upbound.io", version: "v1beta1",
  apiVersion: "sqs.aws.m.upbound.io/v1beta1", plural: "queues", scope: "Namespaced" as const,
  provider: "p", namespaced: true, required: 1, fields: 18 }

beforeEach(() => {
  useBlueprint.setState({ doc: structuredClone(blueprintFixture) as any, nodes: [], wires: [] })
})

describe("interleaved undo", () => {
  it("unwinds field commits and structural actions in order, no overshoot", () => {
    const s = useBlueprint.getState()
    s.addNode(qk, 0, 0)
    const id = useBlueprint.getState().nodes[0].id
    const name = useBlueprint.getState().nodes[0].name
    s.setField(id, "region", "eu-north-1")
    useBlueprint.getState().commitField(id, "region")
    s.addNode(qk, 50, 50)
    expect(useBlueprint.getState().nodes).toHaveLength(2)
    const res = () => useBlueprint.getState().doc!.spec.resources.find(r => r.name === name)!
    useBlueprint.getState().undo() // removes the 2nd node only
    expect(useBlueprint.getState().nodes).toHaveLength(1)
    expect(res().fields["region"]).toEqual({ value: "eu-north-1" })
    useBlueprint.getState().undo() // reverts the field edit only
    expect(res().fields["region"]).toBeUndefined()
    expect(useBlueprint.getState().nodes).toHaveLength(1)
    useBlueprint.getState().undo() // removes the 1st node
    expect(useBlueprint.getState().nodes).toHaveLength(0)
  })

  it("wire guard survives interleaving: setField without the flag cannot clobber a wire", () => {
    const s = useBlueprint.getState()
    s.addNode(qk, 0, 0)
    const id = useBlueprint.getState().nodes[0].id
    const name = useBlueprint.getState().nodes[0].name
    s.connect("maxMessageSize", id, "maxMessageSize")
    s.setField(id, "maxMessageSize", "9999")
    expect(useBlueprint.getState().doc!.spec.resources.find(r => r.name === name)!
      .fields["maxMessageSize"]).toEqual({ from: "params.maxMessageSize" })
  })
})
