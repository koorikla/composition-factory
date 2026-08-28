import { describe, it, expect, beforeAll, afterAll, afterEach } from "vitest"
import { setupServer } from "msw/node"
import { handlers, resetBlueprintFixture } from "./mocks"
import { api, ApiError } from "./contract"

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterEach(() => {
  server.resetHandlers()
  // The success-path mutation tests below persist into the mock's
  // module-level blueprint state; start every case from the fixture.
  resetBlueprintFixture()
})
afterAll(() => server.close())

describe("contract client", () => {
  it("lists kinds from the fixture", async () => {
    const kinds = await api.kinds()
    expect(kinds.length).toBeGreaterThan(0)
    const q = kinds.find(k => k.kind === "Queue" && k.namespaced)
    expect(q).toBeDefined()
    expect(q!.apiVersion).toBe("sqs.aws.m.upbound.io/v1beta1")
    expect(q!.required).toBeGreaterThan(0)
  })

  it("escapes the apiVersion, which contains a slash", async () => {
    const { kind } = await api.kind("sqs.aws.m.upbound.io/v1beta1", "Queue")
    expect(kind.kind).toBe("Queue")
  })

  it("passes required_only through to the query string", async () => {
    const all = await api.fields("sqs.aws.m.upbound.io/v1beta1", "Queue", {})
    const req = await api.fields("sqs.aws.m.upbound.io/v1beta1", "Queue", { requiredOnly: true })
    expect(req.fields.length).toBeLessThan(all.fields.length)
    expect(req.fields.every(f => f.required)).toBe(true)
  })

  it("throws ApiError carrying the status and the server's message", async () => {
    await expect(api.kind("sqs.aws.m.upbound.io/v1beta1", "Nonexistent")).rejects.toMatchObject({
      status: 404,
    })
    try {
      await api.kind("sqs.aws.m.upbound.io/v1beta1", "Nonexistent")
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError)
      expect((e as ApiError).message).not.toBe("")
    }
  })

  it("surfaces a 409 distinctly from a 400, because they mean different things", async () => {
    await expect(api.deleteParameter("maxMessageSize")).rejects.toMatchObject({ status: 409 })
    await expect(api.addParameter("not a valid name", { type: "string" })).rejects.toMatchObject({ status: 400 })
  })
})

// Final-review fix wave, group C: the mock is re-aligned with the Go server
// (internal/api on main — the contract authority). Every case here asserts
// the corrected status and, where a message is load-bearing, the server's
// verbatim wording.
describe("mock/server contract alignment (fix wave C)", () => {
  it("C1: adding a duplicate parameter is a 409 conflict, with AddParameter's own message", async () => {
    try {
      await api.addParameter("maxMessageSize", { type: "integer" })
      expect.unreachable("duplicate add must reject")
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError)
      expect((e as ApiError).status).toBe(409)
      expect((e as ApiError).message).toBe('add parameter: "maxMessageSize" is already declared')
    }
  })

  it("C2: a rename whose target collides is a 409; an invalid target name stays 400", async () => {
    try {
      await api.renameParameter("providerName", "maxMessageSize")
      expect.unreachable("colliding rename must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(409)
      expect((e as ApiError).message).toBe('rename parameter: "maxMessageSize" is already declared')
    }

    try {
      await api.renameParameter("maxMessageSize", "max_size")
      expect.unreachable("invalid-name rename must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe(
        'rename parameter "maxMessageSize" to "max_size": spec.xrd.parameters.max_size: ' +
          "invalid parameter name (must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)",
      )
    }
  })

  it("C2: renaming a parameter to its own name is a no-op success, not a collision", async () => {
    const doc = await api.renameParameter("maxMessageSize", "maxMessageSize")
    expect(doc.spec.xrd.parameters["maxMessageSize"]).toBeDefined()
  })

  it("C3: fields `total` counts the filtered set BEFORE limit truncation", async () => {
    const unlimited = await api.fields("sqs.aws.m.upbound.io/v1beta1", "Queue", {})
    expect(unlimited.total).toBe(unlimited.fields.length)
    expect(unlimited.total).toBe(18)

    const limited = await api.fields("sqs.aws.m.upbound.io/v1beta1", "Queue", { limit: 1 })
    expect(limited.fields).toHaveLength(1)
    expect(limited.total).toBe(18)
  })

  it("C4: deleting providerName on a Namespaced blueprint is a 400 (Validate requires it), never a 204", async () => {
    try {
      await api.deleteParameter("providerName")
      expect.unreachable("deleting providerName must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe(
        'delete parameter "providerName": spec.xrd.parameters.providerName is required for a Namespaced XRD: ' +
          "the Composition emits providerConfigRef.name as {{ $spec.providerName }} for every " +
          "composed resource, so a blueprint without this parameter generates a Composition " +
          "that can never render. Add: providerName: {type: string, required: true}",
      )
    }
  })

  it("C5: the name rule mirrors blueprint.Validate's paramNameRE — no underscores, no YAML keywords", async () => {
    // Underscores are legal JS identifiers but NOT legal parameter names —
    // paramNameRE is ^[a-zA-Z][a-zA-Z0-9]*$ (the old mock accepted both of
    // these).
    await expect(api.addParameter("snake_case", { type: "string" })).rejects.toMatchObject({ status: 400 })
    await expect(api.addParameter("_leading", { type: "string" })).rejects.toMatchObject({ status: 400 })
    // A YAML keyword as a name would silently become a boolean map key.
    await expect(api.addParameter("yes", { type: "string" })).rejects.toMatchObject({ status: 400 })
  })

  it("C5: the type set is strictly the server's; array carries its own refusal", async () => {
    try {
      await api.addParameter("zones", { type: "array" })
      expect.unreachable("array parameter must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe(
        'add parameter "zones": spec.xrd.parameters.zones: type "array" is not supported in M1. ' +
          "The XRD emitter cannot write the required items: schema for it, and a from: " +
          "mapping would render Go's fmt of the slice (\"[a b c]\") -- valid YAML, silently " +
          "wrong. Use a scalar parameter, or a raw: field for a literal list",
      )
    }

    try {
      await api.addParameter("zones", { type: "frobnicate" })
      expect.unreachable("unknown type must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe('add parameter "zones": spec.xrd.parameters.zones: unknown type "frobnicate"')
    }
  })

  it("C6: a PUT body omitting a key that currently holds a value is a 400 naming the omitted keys", async () => {
    // providerName currently holds required: true and a description; a
    // "partial update" omitting them would silently zero both.
    try {
      await api.setParameter("providerName", { type: "string" })
      expect.unreachable("destructive partial PUT must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe(
        'refusing a partial update of parameter "providerName": PUT replaces the whole parameter, so omitting ' +
          "required, description would silently discard the value each of them currently holds. Send those keys " +
          'explicitly — their zero values (false, null, "") are how you clear one.',
      )
    }

    // Saying every currently-held key out loud makes the same replace legal.
    const doc = await api.setParameter("providerName", {
      type: "string",
      required: true,
      description: "ProviderConfig to reconcile the composed resources against.",
    })
    expect(doc.spec.xrd.parameters["providerName"].required).toBe(true)
  })

  it("C7: every parameter mutation resolves with the full persisted blueprint, DELETE included", async () => {
    const added = await api.addParameter("scratch", { type: "string" })
    expect(added.metadata.name).toBe("xqueue")
    expect(added.spec.xrd.parameters["scratch"]).toEqual({ type: "string" })

    const renamed = await api.renameParameter("scratch", "scratchier")
    expect(renamed.spec.xrd.parameters["scratch"]).toBeUndefined()
    expect(renamed.spec.xrd.parameters["scratchier"]).toEqual({ type: "string" })

    const set = await api.setParameter("scratchier", { type: "boolean" })
    expect(set.spec.xrd.parameters["scratchier"]).toEqual({ type: "boolean" })

    // DELETE answers 200 with the document — the old mock's empty 204 made
    // this resolve to undefined.
    const deleted = await api.deleteParameter("scratchier")
    expect(deleted.metadata.name).toBe("xqueue")
    expect(deleted.spec.xrd.parameters["scratchier"]).toBeUndefined()
    expect(deleted.spec.xrd.parameters["providerName"]).toBeDefined()
  })

  it("C8: a kinds limit that is not an integer is a 400 with the server-verbatim message", async () => {
    try {
      await api.kinds("queue", 1.5)
      expect.unreachable("non-integer limit must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe('invalid limit: "1.5"')
    }
  })

  it("C8: negative fields max_depth/limit are accepted as unlimited, exactly like the server", async () => {
    // strconv.Atoi parses a negative integer fine; FieldQuery documents
    // MaxDepth/Limit <= 0 as unlimited. The old mock 400'd both.
    const result = await api.fields("sqs.aws.m.upbound.io/v1beta1", "Queue", { maxDepth: -1, limit: -3 })
    expect(result.fields).toHaveLength(18)
    expect(result.total).toBe(18)
  })

  it("C8: a fields max_depth that is not an integer is a 400 with the server-verbatim message", async () => {
    try {
      await api.fields("sqs.aws.m.upbound.io/v1beta1", "Queue", { maxDepth: 1.5 })
      expect.unreachable("non-integer max_depth must reject")
    } catch (e) {
      expect((e as ApiError).status).toBe(400)
      expect((e as ApiError).message).toBe('invalid max_depth: "1.5"')
    }
  })
})
