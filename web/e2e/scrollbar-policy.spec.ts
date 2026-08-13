import { expect, test, type Page } from "@playwright/test";

const me = {
  id: 1,
  displayName: "Ada",
  username: "ada",
  role: "admin",
  hasLocalLogin: true,
  hasLinkedIdentity: false,
};

const watchedMovie = {
  movieID: 22,
  title: "Back to the Future",
  link: "",
  addedAt: "2026-07-13T10:00:00Z",
  addedByID: 1,
  addedByName: "Ada",
  watchedAt: "2026-08-08T10:00:00Z",
  tmdbId: 105,
  imdbId: "tt0088763",
  releaseDate: "1985-07-03",
  runtime: 116,
  genres: ["Adventure"],
  voteAverage: 8.3,
};

const movieDetail = {
  ...watchedMovie,
  status: "watched",
  tagline: "He was never in time for his classes.",
  overview: "Marty McFly is accidentally sent back in time.",
  cast: [],
  crew: [],
};

const replies = new Map<string, unknown>([
  ["/api/v1/auth/me", me],
  ["/api/v1/integrations/radarr/attention", { count: 0 }],
  ["/api/v1/settings/pool-lock", { poolLocked: false, drawInProgress: false }],
  ["/api/v1/settings/next-up", { id: 1, name: "Ada" }],
  ["/api/v1/movies/current", null],
  ["/api/v1/movies/pool", []],
  ["/api/v1/movies/watched", [watchedMovie]],
  ["/api/v1/movies/22", movieDetail],
  ["/api/v1/members", []],
  ["/api/v1/members/roster", []],
  ["/api/v1/invites", { serverNow: "2026-08-13T10:00:00Z", items: [] }],
]);

async function mockApp(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/events") {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
      return;
    }

    if (!replies.has(path)) {
      await route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
      return;
    }

    await route.fulfill({ json: replies.get(path) });
  });
}

async function profileX(page: Page) {
  return page.getByRole("button", { name: "Your profile" }).evaluate((element) =>
    element.getBoundingClientRect().x,
  );
}

test.beforeEach(async ({ page }) => {
  await mockApp(page);
  await page.goto("/");
  await page.addStyleTag({
    content: `
      :root { --document-scrollbar-width: 15px !important; }
    `,
  });
  await expect(page.getByRole("button", { name: "Your profile" })).toBeVisible();
});

test("top-level tabs keep shared navigation at one horizontal coordinate", async ({ page }) => {
  const moviesX = await profileX(page);
  for (const [tab, ready] of [
    ["Members", ".mem"],
    ["Stats", ".stats-head"],
    ["Admin", ".shell--admin"],
  ] as const) {
    await page.getByRole("link", { name: new RegExp(`^${tab}`) }).click();
    await expect(page.locator(ready)).toBeVisible();
    expect(Math.abs((await profileX(page)) - moviesX), `${tab} navigation shift`).toBeLessThanOrEqual(0.5);
  }
});

test("movie backdrop reaches beneath the native scrollbar gutter", async ({ page }) => {
  await page.getByRole("button", { name: /Back to the Future/ }).click();
  await expect(page.getByRole("dialog", { name: "Back to the Future" })).toBeVisible();

  const geometry = await page.locator(".modal--movie").evaluate((modal) => {
    const scroller = modal.querySelector<HTMLElement>(".moviemodal__scroll");
    const backdrop = modal.querySelector<HTMLElement>(".moviemodal__backdrop");
    if (!scroller || !backdrop) return null;

    const scrollerBox = scroller.getBoundingClientRect();
    const backdropBox = backdrop.getBoundingClientRect();
    const gutter = Number.parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue("--document-scrollbar-width"),
    );
    return {
      scrollerRight: scrollerBox.right,
      safeContentRight: scrollerBox.right - gutter,
      backdropRight: backdropBox.right,
    };
  });

  expect(geometry).not.toBeNull();
  expect(Math.abs(geometry!.backdropRight - geometry!.scrollerRight)).toBeLessThanOrEqual(0.5);
  expect(geometry!.backdropRight - geometry!.safeContentRight).toBeGreaterThan(0);
});

test("opening and closing a movie modal keeps shared navigation fixed", async ({ page }) => {
  const before = await profileX(page);

  await page.getByRole("button", { name: /Back to the Future/ }).click();
  await expect(page.getByRole("dialog", { name: "Back to the Future" })).toBeVisible();
  expect(Math.abs((await profileX(page)) - before), "modal open shift").toBeLessThanOrEqual(0.5);

  await page.getByRole("button", { name: "Close" }).click();
  await expect(page.getByRole("dialog", { name: "Back to the Future" })).toBeHidden();
  expect(Math.abs((await profileX(page)) - before), "modal close shift").toBeLessThanOrEqual(0.5);
});

test("top-level routes do not overflow a 320 px viewport", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });

  for (const [tab, ready] of [
    ["Movies", ".watch-body"],
    ["Members", ".mem"],
    ["Stats", ".stats-head"],
    ["Admin", ".shell--admin"],
  ] as const) {
    await page.getByRole("link", { name: new RegExp(`^${tab}`) }).click();
    await expect(page.locator(ready)).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, `${tab} horizontal overflow`).toBeLessThanOrEqual(0);
  }
});
