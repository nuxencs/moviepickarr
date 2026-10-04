import path from "path";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// "node" is the default; "dom" (jsdom) is only for behaviour that needs a render
// (#140, docs/DEVELOPMENT.md).
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
