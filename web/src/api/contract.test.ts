import { describe, it, expect, beforeAll, afterAll, afterEach } from "vitest"
import { setupServer } from "msw/node"
import { handlers } from "./mocks"
import { api, ApiError } from "./contract"

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: "error" }))
afterEach(() => server.resetHandlers())
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
