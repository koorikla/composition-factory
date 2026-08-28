import { defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  // The dev loop: `cf serve` listens on 127.0.0.1:8080 and owns /api; the
  // vite dev server proxies every /api request there, so `cf serve` +
  // `npm run dev` forms a working loop against the real Go server with no
  // MSW browser worker involved (MSW stays a node-side test double only).
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/setupTests.ts"],
  },
})
