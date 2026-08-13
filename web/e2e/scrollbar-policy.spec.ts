import { expect, test, type Page } from "@playwright/test";

async function profileX(page: Page) {
  return page.getByRole("button", { name: "Your profile" }).evaluate((element) =>
    element.getBoundingClientRect().x,
  );
}

const castMembers = Array.from({ length: 24 }, (_, index) => ({
  id: 90_000 + index,
  name: `Cast Member ${index + 1}`,
  character: `Character ${index + 1}`,
}));

async function openScrollableMovie(page: Page) {
  await page.route(/\/api\/v1\/movies\/\d+$/, async (route) => {
    const response = await route.fetch();
    const detail = await response.json();
    await route.fulfill({
      response,
      json: {
        ...detail,
        overview: `${detail.overview ?? ""} ${"Long movie overview. ".repeat(80)}`,
        cast: castMembers,
      },
    });
  });
  await page.locator('.tile-grid--pool article[role="button"]').first().click();
  await expect(page.getByRole("dialog")).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Your profile" })).toBeVisible();
  // Geometry baselines must not straddle the display-font swap when the full
  // three-engine matrix is competing for CPU and network time.
  await page.evaluate(() => document.fonts.ready);
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

test("navigation divider spans the viewport outside a reserved gutter", async ({ page }) => {
  const geometry = await page.locator(".nav").evaluate((nav) => ({
    dividerWidth: Number.parseFloat(getComputedStyle(nav, "::after").width),
    viewportWidth: window.innerWidth,
  }));

  expect(Math.abs(geometry.dividerWidth - geometry.viewportWidth)).toBeLessThanOrEqual(0.5);
});

test("movie backdrop spans the custom scrollbar without a reserved gutter", async ({ page }) => {
  await openScrollableMovie(page);

  const geometry = await page.locator(".modal--movie").evaluate((modal) => {
    const scroller = modal.querySelector<HTMLElement>(".moviemodal__scroll");
    const backdrop = modal.querySelector<HTMLElement>(".moviemodal__backdrop");
    if (!scroller || !backdrop) return null;

    const scrollerBox = scroller.getBoundingClientRect();
    const backdropBox = backdrop.getBoundingClientRect();
    const track = modal.querySelector<HTMLElement>(".movie-scrollbar__track");
    if (!track) return null;
    const trackBox = track.getBoundingClientRect();
    return {
      scrollerRight: scrollerBox.right,
      backdropRight: backdropBox.right,
      nativeGutter: scroller.offsetWidth - scroller.clientWidth,
      trackRight: trackBox.right,
    };
  });

  expect(geometry).not.toBeNull();
  expect(Math.abs(geometry!.backdropRight - geometry!.scrollerRight)).toBeLessThanOrEqual(0.5);
  expect(geometry!.nativeGutter).toBe(0);
  expect(geometry!.trackRight).toBeLessThanOrEqual(geometry!.scrollerRight);
});

test("loading and loaded cast keep the same custom horizontal owner", async ({ page }) => {
  let releaseDetail!: () => void;
  const detailGate = new Promise<void>((resolve) => {
    releaseDetail = resolve;
  });
  await page.route(/\/api\/v1\/movies\/\d+$/, async (route) => {
    await detailGate;
    const response = await route.fetch();
    const detail = await response.json();
    await route.fulfill({ response, json: { ...detail, cast: castMembers } });
  });

  await page.locator('.tile-grid--pool article[role="button"]').first().click();
  await expect(page.getByRole("dialog")).toBeVisible();
  const castOwner = page.locator(".movie-cast-scrollbar > .castrow");
  await expect(castOwner).toBeVisible();
  await expect(page.getByRole("scrollbar", { name: "Cast position" })).toHaveCount(0);
  expect(
    await castOwner.evaluate((element) => element.offsetHeight - element.clientHeight),
    "loading cast native gutter",
  ).toBe(0);

  releaseDetail();
  await expect(page.getByRole("scrollbar", { name: "Cast position" })).toBeVisible();
  expect(
    await castOwner.evaluate((element) => element.offsetHeight - element.clientHeight),
    "loaded cast native gutter",
  ).toBe(0);
});

test("movie details scrollbar has a forgiving drag target and complete controls", async ({ page }) => {
  await openScrollableMovie(page);

  const viewport = page.locator(".moviemodal__scroll");
  const scrollbar = page.getByRole("scrollbar", { name: "Movie details position" });
  const thumb = scrollbar.locator(".movie-scrollbar__thumb");
  await expect(scrollbar).toBeVisible();

  const thumbBox = await thumb.boundingBox();
  expect(thumbBox).not.toBeNull();
  const nearThumbX = thumbBox!.x - 2;
  const thumbMiddleY = thumbBox!.y + thumbBox!.height / 2;

  await page.mouse.move(nearThumbX, thumbMiddleY);
  await page.mouse.down();
  expect(await viewport.evaluate((element) => element.scrollTop)).toBe(0);
  await page.mouse.move(nearThumbX, thumbMiddleY + 40);
  await page.mouse.up();
  await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);

  await scrollbar.press("Home");
  await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBe(0);
  await expect(scrollbar).toHaveAttribute("aria-valuenow", "0");
  await scrollbar.press("End");
  await expect
    .poll(() =>
      viewport.evaluate((element) => element.scrollHeight - element.clientHeight - element.scrollTop),
    )
    .toBeLessThanOrEqual(1);

  await scrollbar.press("Home");
  await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBe(0);
  await expect(scrollbar).toHaveAttribute("aria-valuenow", "0");
  const trackBox = await scrollbar.boundingBox();
  expect(trackBox).not.toBeNull();
  await page.mouse.click(trackBox!.x + 1, trackBox!.y + trackBox!.height - 2);
  const trackDelta = await viewport.evaluate((element) => element.scrollTop);
  const pageStep = await viewport.evaluate((element) => element.clientHeight * 0.88);
  expect(trackDelta).toBeGreaterThan(0);
  expect(trackDelta).toBeLessThanOrEqual(pageStep + 1);
});

test("cast scrollbar has a forgiving drag target and complete controls", async ({ page }) => {
  await openScrollableMovie(page);

  const viewport = page.locator(".castrow");
  await viewport.scrollIntoViewIfNeeded();
  const scrollbar = page.getByRole("scrollbar", { name: "Cast position" });
  const thumb = scrollbar.locator(".movie-cast-scrollbar__thumb");
  await expect(scrollbar).toBeVisible();

  const thumbBox = await thumb.boundingBox();
  expect(thumbBox).not.toBeNull();
  const thumbMiddleX = thumbBox!.x + thumbBox!.width / 2;
  const nearThumbY = thumbBox!.y - 2;

  await page.mouse.move(thumbMiddleX, nearThumbY);
  await page.mouse.down();
  expect(await viewport.evaluate((element) => element.scrollLeft)).toBe(0);
  await page.mouse.move(thumbMiddleX + 40, nearThumbY);
  await page.mouse.up();
  await expect.poll(() => viewport.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);

  await scrollbar.press("Home");
  await expect.poll(() => viewport.evaluate((element) => element.scrollLeft)).toBe(0);
  await expect(scrollbar).toHaveAttribute("aria-valuenow", "0");
  await scrollbar.press("End");
  await expect
    .poll(() =>
      viewport.evaluate((element) => element.scrollWidth - element.clientWidth - element.scrollLeft),
    )
    .toBeLessThanOrEqual(1);

  await scrollbar.press("Home");
  await expect.poll(() => viewport.evaluate((element) => element.scrollLeft)).toBe(0);
  await expect(scrollbar).toHaveAttribute("aria-valuenow", "0");
  const trackBox = await scrollbar.boundingBox();
  expect(trackBox).not.toBeNull();
  await page.mouse.click(trackBox!.x + trackBox!.width - 2, trackBox!.y + trackBox!.height / 2);
  const trackDelta = await viewport.evaluate((element) => element.scrollLeft);
  const pageStep = await viewport.evaluate((element) => element.clientWidth * 0.88);
  expect(trackDelta).toBeGreaterThan(0);
  expect(trackDelta).toBeLessThanOrEqual(pageStep + 1);
});

test("opening and closing a movie modal keeps shared navigation fixed", async ({ page }) => {
  const before = await profileX(page);

  const opener = page.locator('.tile-grid--pool article[role="button"]').first();
  await expect(opener).toBeInViewport();
  const openerBox = await opener.boundingBox();
  expect(openerBox).not.toBeNull();
  // Use the pointer at the visible tile coordinates. WebKit's locator click
  // can scroll the body horizontally while it performs scrollIntoView(),
  // which is outside the modal transition measured by this test.
  const viewport = page.viewportSize();
  expect(viewport).not.toBeNull();
  await page.mouse.click(
    (Math.max(0, openerBox!.x) + Math.min(viewport!.width, openerBox!.x + openerBox!.width)) / 2,
    (Math.max(0, openerBox!.y) + Math.min(viewport!.height, openerBox!.y + openerBox!.height)) / 2,
  );
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  expect(Math.abs((await profileX(page)) - before), "modal open shift").toBeLessThanOrEqual(0.5);

  await page.getByRole("button", { name: "Close" }).click();
  await expect(dialog).toBeHidden();
  expect(Math.abs((await profileX(page)) - before), "modal close shift").toBeLessThanOrEqual(0.5);
});

test("filtering across the document overflow threshold keeps shared navigation fixed", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 2200 });
  const before = await profileX(page);
  const search = page.getByRole("textbox", { name: "Search watched movies by title or adder" });
  expect(await page.evaluate(() => document.body.scrollHeight > document.body.clientHeight)).toBe(true);

  await search.fill("no fixture movie has this title");
  await expect(page.getByText("No movies match your search")).toBeVisible();
  await expect(page.locator(".watch-body")).toHaveCount(0);
  expect(await page.evaluate(() => document.body.scrollHeight <= document.body.clientHeight)).toBe(true);
  expect(Math.abs((await profileX(page)) - before), "filtered navigation shift").toBeLessThanOrEqual(0.5);

  await search.clear();
  await expect(page.locator(".watch-body")).toBeVisible();
  expect(Math.abs((await profileX(page)) - before), "restored navigation shift").toBeLessThanOrEqual(0.5);
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
