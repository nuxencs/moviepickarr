import { expect, test, type Page } from "@playwright/test";

// Baselines must not straddle the mg-rise and modal entrance animations.
async function settle(page: Page) {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((animation) => Number.isFinite(animation.effect?.getComputedTiming().endTime))
        .map((animation) => animation.finished.catch(() => undefined)),
    ),
  );
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("the Admin indicator lands on Maintenance", async ({ page }) => {
    await page.goto("/admin/maintenance");
    const link = page.getByRole("navigation", { name: "Admin sections" }).getByRole("link", {
      name: "Maintenance",
    });
    const indicator = page.locator(".admin-layout__indicator");

    await expect(link).toHaveAttribute("aria-current", "page");
    await expect
      .poll(async () => {
        const [line, row] = await Promise.all([indicator.boundingBox(), link.boundingBox()]);
        return line && row ? Math.abs(line.y - row.y) : Number.POSITIVE_INFINITY;
      })
      .toBeLessThan(1);
  });

  test("the entrance never makes the Admin body scrollable", async ({ page }) => {
    await page.addInitScript(() => {
      const samples: number[] = [];
      Object.assign(window, { __bodyOverflow: samples });
      const sample = () => {
        const body = document.querySelector(".admin-layout__body");
        if (body) samples.push(body.scrollHeight - body.clientHeight);
        if (samples.length < 90) requestAnimationFrame(sample);
      };
      requestAnimationFrame(sample);
    });
    await page.goto("/admin/maintenance");
    await expect(page.getByRole("list", { name: "Movie library" }).getByRole("listitem").first()).toBeVisible();

    const samples = () => page.evaluate(() => (window as unknown as { __bodyOverflow: number[] }).__bodyOverflow);
    await expect.poll(async () => (await samples()).length).toBeGreaterThanOrEqual(90);
    expect(Math.max(...(await samples()))).toBeLessThanOrEqual(0);
  });

  test("only the rows scroll, with the scrollbar beside them", async ({ page }) => {
    await page.goto("/admin/maintenance");
    const list = page.getByRole("list", { name: "Movie library" });
    const rows = list.getByRole("listitem");
    await expect(rows.first()).toBeVisible();

    const total = Number(await rows.first().getAttribute("aria-setsize"));
    expect(total).toBeGreaterThan(100);
    expect(await rows.count()).toBeLessThan(total);

    const body = page.locator(".admin-layout__body");
    const head = page.locator(".library-register__head");
    const viewport = page.locator(".library-register__viewport");
    const still = [
      page.getByRole("heading", { name: "Movie library" }),
      page.getByRole("searchbox", { name: "Search the movie library" }),
      head,
    ];
    await settle(page);
    const before = await Promise.all(still.map(async (locator) => (await locator.boundingBox())!.y));

    // The scrollbar starts under the column head, and the head reserves the same gutter.
    const headBox = (await head.boundingBox())!;
    const viewportBox = (await viewport.boundingBox())!;
    expect(viewportBox.y).toBeGreaterThanOrEqual(headBox.y + headBox.height - 0.5);
    expect(viewportBox.y + viewportBox.height).toBeLessThanOrEqual(page.viewportSize()!.height);
    const gutter = (locator: typeof head) => locator.evaluate((element) => element.offsetWidth - element.clientWidth);
    expect(await gutter(head)).toBe(await gutter(viewport));
    const tmdbLabel = (await head.getByText("TMDB", { exact: true }).boundingBox())!;
    const tmdbCell = (await rows.first().locator(".library-row__tmdb").boundingBox())!;
    expect(Math.abs(tmdbLabel.x - tmdbCell.x)).toBeLessThanOrEqual(0.5);

    await rows.first().hover();
    await page.mouse.wheel(0, 2_000);
    await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
    expect(await body.evaluate((element) => element.scrollTop)).toBe(0);
    for (const [index, locator] of still.entries()) {
      expect(Math.abs((await locator.boundingBox())!.y - before[index])).toBeLessThanOrEqual(0.5);
    }

    await viewport.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(list.locator(`[aria-posinset="${total}"]`)).toBeInViewport();
  });

  test("correction results own their scroll and keep the search and date in place", async ({ page }) => {
    const { dialog, group } = await searchCorrectionResults(page);
    const search = dialog.getByRole("searchbox", { name: "TMDB title" });
    const date = dialog.getByLabel("Watched date and time");
    const outer = dialog.locator(".correction__scroll");
    await settle(page);
    const searchTop = (await search.boundingBox())!.y;
    await expect(date).toBeInViewport();

    await group.hover();
    await page.mouse.wheel(0, 2_000);
    await expect.poll(() => group.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
    expect(await outer.evaluate((element) => element.scrollTop)).toBe(0);
    expect(Math.abs((await search.boundingBox())!.y - searchTop)).toBeLessThanOrEqual(0.5);
    await expect(date).toBeInViewport();
  });
});

test.describe("short desktop", () => {
  test.use({ viewport: { width: 1280, height: 600 } });

  test("correction results stop at their floor above the watched date", async ({ page }) => {
    const { dialog, group } = await searchCorrectionResults(page);
    await settle(page);

    const results = (await group.boundingBox())!;
    const dateSection = (await dialog.locator(".correction__section").nth(1).boundingBox())!;
    expect(results.height).toBeGreaterThanOrEqual(160);
    expect(results.y + results.height).toBeLessThanOrEqual(dateSection.y + 0.5);

    // The results chain to the dialog body at their end, so the date stays reachable by wheel.
    // Set the end directly: Firefox caps one wheel event at about a page.
    const outer = dialog.locator(".correction__scroll");
    await group.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await group.hover();
    await page.mouse.wheel(0, 1_000);
    await expect.poll(() => outer.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  });

  test("the library fits without scrolling the Admin body", async ({ page }) => {
    await page.goto("/admin/maintenance");
    await expect(page.getByRole("list", { name: "Movie library" }).getByRole("listitem").first()).toBeVisible();
    await settle(page);
    const body = (await page.locator(".admin-layout__body").boundingBox())!;
    const section = (await page.locator(".admin-maintenance").boundingBox())!;
    expect(section.y + section.height).toBeLessThanOrEqual(body.y + body.height + 0.5);
  });
});

async function searchCorrectionResults(page: Page) {
  const results = Array.from({ length: 30 }, (_, index) => ({
    id: 90_000 + index,
    title: `Correction Test Movie ${index + 1}`,
    poster_path: null,
    release_date: "2026-01-01",
    overview: "",
  }));
  await page.route("**/api/v1/tmdb/search?*", (route) => route.fulfill({ json: results }));
  await page.goto("/admin/maintenance");
  await page.locator(".library-row:has(.library-row__status[data-status='watched'])").first().click();

  const dialog = page.getByRole("dialog", { name: "Correct movie" });
  await dialog.getByRole("searchbox", { name: "TMDB title" }).fill("correction");
  await dialog.getByRole("button", { name: "Search TMDB" }).click();
  const group = dialog.getByRole("radiogroup", { name: "TMDB results" });
  await expect(group.getByText("Correction Test Movie 1", { exact: true })).toBeVisible();
  return { dialog, group };
}
