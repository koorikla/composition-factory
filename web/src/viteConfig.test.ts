// Guards the dev-loop wiring (fix wave D1): `cf serve` listens on
// 127.0.0.1:8080, and the vite dev server must proxy /api there or
// `npm run dev` serves a frontend whose every fetch 404s (there is no MSW
// browser worker to catch them — MSW is a node-side test double only).
import { describe, it, expect } from "vitest"
import config from "../vite.config"

describe("vite dev server (fix wave D1)", () => {
  it("proxies /api to cf serve's 127.0.0.1:8080", () => {
    const server = (config as { server?: { proxy?: Record<string, unknown> } }).server
    expect(server?.proxy?.["/api"]).toBe("http://127.0.0.1:8080")
  })
})
