import { describe, it, expect, beforeEach } from "vitest"
import { useBlueprint } from "./blueprint"
import blueprintFixture from "../api/fixtures/blueprint.json"

const queueKind = {
  kind: "Queue", group: "sqs.aws.m.upbound.io", version: "v1beta1",
  apiVersion: "sqs.aws.m.upbound.io/v1beta1", plural: "queues",
  scope: "Namespaced" as const, provider: "ghcr.io/x/provider-aws-sqs:v2.7.0",
  namespaced: true, required: 1, fields: 18,
}

beforeEach(() => {
  useBlueprint.setState({ doc: structuredClone(blueprintFixture) as any, nodes: [], wires: [] })
})

describe("blueprint store", () => {
  it("adding a node adds a resource to the document", () => {
    useBlueprint.getState().addNode(queueKind, 100, 200)
    const { doc, nodes } = useBlueprint.getState()
    expect(nodes).toHaveLength(1)
    expect(doc!.spec.resources.some(r => r.kind === "Queue")).toBe(true)
  })

  it("gives each node a unique resource name, because the name is the identity annotation", () => {
    const s = useBlueprint.getState()
    s.addNode(queueKind, 0, 0)
    s.addNode(queueKind, 10, 10)
    const names = useBlueprint.getState().doc!.spec.resources.map(r => r.name)
    expect(new Set(names).size).toBe(names.length)
  })

  it("connecting writes {from: params.X} onto the target field", () => {
    const s = useBlueprint.getState()
    s.addNode(queueKind, 0, 0)
    const nodeId = useBlueprint.getState().nodes[0].id
    s.connect("maxMessageSize", nodeId, "maxMessageSize")
    const res = useBlueprint.getState().doc!.spec.resources.find(r => r.name === useBlueprint.getState().nodes[0].name)!
    expect(res.fields["maxMessageSize"]).toEqual({ from: "params.maxMessageSize" })
  })

  it("disconnecting removes the field, it does not leave an empty mapping", () => {
    const s = useBlueprint.getState()
    s.addNode(queueKind, 0, 0)
    const nodeId = useBlueprint.getState().nodes[0].id
    s.connect("maxMessageSize", nodeId, "maxMessageSize")
    const wireId = useBlueprint.getState().wires[0].id
    s.disconnect(wireId)
    const res = useBlueprint.getState().doc!.spec.resources.find(r => r.name === useBlueprint.getState().nodes[0].name)!
    expect(res.fields["maxMessageSize"]).toBeUndefined()
  })

  it("removing a node removes its wires too, leaving no dangling reference", () => {
    const s = useBlueprint.getState()
    s.addNode(queueKind, 0, 0)
    const nodeId = useBlueprint.getState().nodes[0].id
    s.connect("maxMessageSize", nodeId, "maxMessageSize")
    s.removeNode(nodeId)
    expect(useBlueprint.getState().wires).toHaveLength(0)
    expect(useBlueprint.getState().nodes).toHaveLength(0)
  })

  it("undo restores the previous document", () => {
    const s = useBlueprint.getState()
    const before = structuredClone(useBlueprint.getState().doc)
    s.addNode(queueKind, 0, 0)
    expect(useBlueprint.getState().canUndo()).toBe(true)
    s.undo()
    expect(useBlueprint.getState().doc).toEqual(before)
  })

  it("dragging does not create an undo entry per pointer event", () => {
    const s = useBlueprint.getState()
    s.addNode(queueKind, 0, 0)
    const id = useBlueprint.getState().nodes[0].id
    for (let i = 0; i < 50; i++) s.moveNode(id, i, i)
    let steps = 0
    while (useBlueprint.getState().canUndo() && steps < 60) { useBlueprint.getState().undo(); steps++ }
    expect(steps).toBeLessThan(5)
  })

  it("committing a drag folds it into exactly one undo step that restores the pre-drag position", () => {
    const s = useBlueprint.getState()
    s.addNode(queueKind, 0, 0)
    const id = useBlueprint.getState().nodes[0].id
    const before = { x: useBlueprint.getState().nodes[0].x, y: useBlueprint.getState().nodes[0].y }

    for (let i = 0; i < 50; i++) s.moveNode(id, i, i)
    s.commitMove()
    expect(useBlueprint.getState().canUndo()).toBe(true)

    // Exactly one undo() call — not a loop — must land back on the pre-drag
    // position, because commitMove() folded the whole 50-move gesture into
    // a single history entry.
    s.undo()
    const node = useBlueprint.getState().nodes.find(n => n.id === id)!
    expect(node.x).toBe(before.x)
    expect(node.y).toBe(before.y)

    // A second commitMove with no intervening moveNode calls has nothing
    // pending to fold (dragBaseline is already null) and must not grow
    // history.
    const historyLenBeforeSecondCommit = useBlueprint.getState().history.length
    s.commitMove()
    expect(useBlueprint.getState().history.length).toBe(historyLenBeforeSecondCommit)
  })

  describe("hydrateNodes", () => {
    // blueprintFixture's one resource, "main-queue", is a Queue whose
    // provider is "xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0" (the
    // namespaced, v2 variant) — distinct from a same-`kind` cluster-scoped
    // Queue that a kind-only match could not tell apart.
    const namespacedQueueKind = {
      kind: "Queue", group: "sqs.aws.m.upbound.io", version: "v1beta1",
      apiVersion: "sqs.aws.m.upbound.io/v1beta1", plural: "queues",
      scope: "Namespaced" as const, provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0",
      namespaced: true, required: 1, fields: 18,
    }
    const clusterQueueKind = {
      kind: "Queue", group: "sqs.aws.upbound.io", version: "v1beta1",
      apiVersion: "sqs.aws.upbound.io/v1beta1", plural: "queues",
      scope: "Cluster" as const, provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.21.0",
      namespaced: false, required: 1, fields: 18,
    }

    it("gives an un-hydrated resource a node, matched by (kind, provider) not kind alone", () => {
      // both kinds share `kind: "Queue"`; only the provider tells them apart.
      useBlueprint.getState().hydrateNodes([clusterQueueKind, namespacedQueueKind])
      const { nodes } = useBlueprint.getState()
      expect(nodes).toHaveLength(1)
      expect(nodes[0].name).toBe("main-queue")
      expect(nodes[0].apiVersion).toBe("sqs.aws.m.upbound.io/v1beta1")
    })

    it("leaves a resource un-hydrated when no Kind matches its (kind, provider) pair", () => {
      // clusterQueueKind alone shares `kind` with main-queue but not
      // `provider` — a kind-only match would wrongly hydrate it.
      useBlueprint.getState().hydrateNodes([clusterQueueKind])
      expect(useBlueprint.getState().nodes).toHaveLength(0)
    })

    it("skips a resource that already has a node, and does not duplicate it", () => {
      const s = useBlueprint.getState()
      s.addNode(namespacedQueueKind, 5, 5)
      const before = useBlueprint.getState().nodes.length
      s.hydrateNodes([namespacedQueueKind])
      // only "main-queue" (from the fixture) was un-hydrated; the node
      // addNode() just created for its own new resource is untouched.
      expect(useBlueprint.getState().nodes.length).toBe(before + 1)
    })

    it("never grows undo history — loading a document is not a user edit", () => {
      useBlueprint.getState().hydrateNodes([namespacedQueueKind])
      expect(useBlueprint.getState().canUndo()).toBe(false)
    })
  })
})
