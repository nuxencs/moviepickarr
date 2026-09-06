import { expect, test, type Page } from "@playwright/test";

async function warmMembers(page: Page) {
  await page.goto("/users");
  await expect(page.locator(".mem")).toBeVisible();
  await page.locator(".nav__tabs").getByRole("link", { name: "Movies", exact: true }).click();
  await expect(page.locator(".watch-body")).toBeVisible();
  await page.mouse.move(1, 1);
}

test("cached page switches do not wait for session revalidation", async ({ page }) => {
  await warmMembers(page);
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  let requested!: () => void;
  const requestSeen = new Promise<void>((resolve) => { requested = resolve; });
  await page.route("**/api/v1/auth/me", async (route) => {
    const response = await route.fetch();
    requested();
    await gate;
    await route.fulfill({ response });
  });

  try {
    await page.locator(".nav__tabs").getByRole("link", { name: "Members", exact: true }).click();
    await requestSeen;
    await expect(page.locator(".mem")).toBeVisible();
    await expect(page.locator(".watch-body")).toHaveCount(0);
    // A second navigation must also remain usable while that same check waits.
    await page.locator(".nav__tabs").getByRole("link", { name: "Movies", exact: true }).click();
    await expect(page.locator(".watch-body")).toBeVisible();
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("an expired background session returns to login after a cached page switch", async ({ page }) => {
  await warmMembers(page);
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/v1/auth/me", async (route) => {
    await gate;
    await route.fulfill({ status: 401, contentType: "application/problem+json", body: '{"detail":"Session expired"}' });
  });

  try {
    await page.locator(".nav__tabs").getByRole("link", { name: "Members", exact: true }).click();
    await expect(page.locator(".mem")).toBeVisible();
    release();
    await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
    await expect(page.locator(".mem")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Your profile" })).toHaveCount(0);
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("a cold load still waits for the session before showing private pages", async ({ page }) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  let requested!: () => void;
  const requestSeen = new Promise<void>((resolve) => { requested = resolve; });
  await page.route("**/api/v1/auth/me", async (route) => {
    requested();
    await gate;
    await route.fulfill({ status: 401, contentType: "application/problem+json", body: '{"detail":"Session expired"}' });
  });

  try {
    await page.goto("/users");
    await requestSeen;
    await expect(page.locator(".mem")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Your profile" })).toHaveCount(0);
    release();
    await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible();
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
});
