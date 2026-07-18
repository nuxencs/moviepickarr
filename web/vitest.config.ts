import path from "path";

import { defineConfig } from "vitest/config";

// Pure-TS tests (state machines, codecs) run in a plain node environment; no
// DOM is stubbed on purpose: modules under test must take environment inputs
// as data (see drawMachine's DrawEnv) rather than reading the DOM themselves.
export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
