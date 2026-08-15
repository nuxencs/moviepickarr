import { defineConfig, devices } from "@playwright/test";

const authFile = (browser: "chromium" | "firefox" | "webkit") =>
  `e2e/.auth/admin-${browser}.json`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  // Every project exercises one production server and its throwaway database.
  // Mutation flows restore their logical baseline, but they must not race a
  // geometry read or another lifecycle transition in a second worker.
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: "http://127.0.0.1:3030",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    viewport: { width: 1280, height: 720 },
  },
  projects: [
    {
      name: "setup-chromium",
      testMatch: /auth\.setup\.ts/,
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "setup-firefox",
      testMatch: /auth\.setup\.ts/,
      use: { ...devices["Desktop Firefox"] },
    },
    {
      name: "setup-webkit",
      testMatch: /auth\.setup\.ts/,
      use: { ...devices["Desktop Safari"] },
    },
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"], storageState: authFile("chromium") },
      dependencies: ["setup-chromium"],
      testIgnore: /auth\.setup\.ts/,
    },
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"], storageState: authFile("firefox") },
      dependencies: ["setup-firefox"],
      testIgnore: /auth\.setup\.ts/,
    },
    {
      name: "webkit",
      use: { ...devices["Desktop Safari"], storageState: authFile("webkit") },
      dependencies: ["setup-webkit"],
      testIgnore: /auth\.setup\.ts/,
    },
  ],
  webServer: {
    command: "bun run e2e:server",
    url: "http://127.0.0.1:3030/api/v1/auth/config",
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
