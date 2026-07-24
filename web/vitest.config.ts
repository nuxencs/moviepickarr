import path from "path";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Two projects, kept apart on purpose (see issue #140).
//
// "node" is the default and stays the first choice: pure-TS tests (state
// machines, codecs) in a plain node environment, no DOM stubbed, so modules
// under test take environment inputs as data (see drawMachine's DrawEnv)
// rather than reading the DOM themselves.
//
// "dom" is the fallback for behaviour that only exists once a component
// renders and can't be pulled into a pure function without contorting the
// code, remount and lifecycle wiring above all. It runs *.render.test.tsx in
// jsdom with Testing Library. Tests there drive the DOM the way a member
// would (role/text queries, clicks) and never assert on internal state.
const alias = { "@": path.resolve(__dirname, "./src") };

export default defineConfig({
  resolve: { alias },
  test: {
    projects: [
      {
        resolve: { alias },
        test: {
          name: "node",
          include: ["src/**/*.test.ts"],
          environment: "node",
        },
      },
      {
        plugins: [react()],
        resolve: { alias },
        test: {
          name: "dom",
          include: ["src/**/*.render.test.tsx"],
          environment: "jsdom",
          setupFiles: ["./src/test/setupDom.ts"],
        },
      },
    ],
  },
});
