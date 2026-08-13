import { expect, test, type Page } from "@playwright/test";

const layouts = [1280, 390].flatMap((width) =>
  (["dark", "light"] as const).map((theme) => ({
    name: `${width}px ${theme}`,
    width,
    height: 640,
    theme,
  })),
);

const tmdbResults = Array.from({ length: 30 }, (_, index) => ({
  id: 90_000 + index,
  title: `Layout Test Movie ${index + 1}`,
  poster_path: null,
  release_date: "2026-01-01",
  overview: "A deterministic result used to make the real search modal taller than the viewport.",
}));

async function openMembers(page: Page) {
  await page.goto("/");
  await page.getByRole("link", { name: /^Members/ }).click();
  await expect(page.locator(".mem")).toBeVisible();
}

async function openAdaStash(page: Page, width: number) {
  await openMembers(page);
  if (width <= 760) {
    await page.locator('.mem-row[data-active="true"] .mem-tostash').click();
  }
  await expect(page.getByRole("button", { name: "Add to Ada's stash" })).toBeVisible();
}

async function pageScrollTop(page: Page) {
  return page.evaluate(() => document.scrollingElement?.scrollTop ?? 0);
}

for (const layout of layouts) {
  test.describe(layout.name, () => {
    test.use({ viewport: { width: layout.width, height: layout.height } });

    test.beforeEach(async ({ page }) => {
      await page.addInitScript((theme) => localStorage.setItem("vite-ui-theme", theme), layout.theme);
    });

    test("capped dialog stays centered and owns scrolling", async ({ page }) => {
      await page.goto("/admin/integrations/radarr/webhooks");
      const opener = page.getByRole("button", { name: "Add destination" });
      await expect(opener).toBeVisible();
      await opener.click();

      const dialog = page.getByRole("dialog", { name: "Add webhook destination" });
      const surface = page.locator(".modal--radarr-webhook.modal--capped");
      const scroller = surface.locator(".modal__scroll");
      await expect(dialog).toBeVisible();

      const box = await surface.boundingBox();
      expect(box).not.toBeNull();
      expect(Math.abs(box!.y - (layout.height - box!.y - box!.height))).toBeLessThanOrEqual(0.5);

      const documentBefore = await pageScrollTop(page);
      await scroller.hover();
      await page.mouse.wheel(0, 10_000);
      await expect.poll(() => scroller.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
      expect(await pageScrollTop(page)).toBe(documentBefore);

      await page.getByRole("button", { name: "Cancel" }).click();
      await expect(dialog).toBeHidden();
    });

    test("uncapped search modal keeps 56 px after tall content", async ({ page }) => {
      await page.route("**/api/v1/tmdb/search?*", (route) => route.fulfill({ json: tmdbResults }));
      await openAdaStash(page, layout.width);
      const opener = page.getByRole("button", { name: "Add to Ada's stash" });
      await opener.click();

      const dialog = page.getByRole("dialog", { name: "Search & add movies" });
      await page.getByRole("textbox", { name: "Search movies by title" }).fill("layout");
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await expect(dialog.getByText('30 results for "layout"')).toBeVisible();

      const veil = page.locator(".modal-veil");
      const surface = page.locator(".modal:not(.modal--capped)");
      const documentBefore = await pageScrollTop(page);
      await surface.hover();
      await page.mouse.wheel(0, 10_000);
      await expect.poll(() => veil.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
      await veil.evaluate((element) => element.scrollTo(0, element.scrollHeight));

      await expect
        .poll(() =>
          surface.evaluate((element) => window.innerHeight - element.getBoundingClientRect().bottom),
        )
        .toBeGreaterThanOrEqual(55.5);
      const bottomGap = await surface.evaluate((element) =>
        window.innerHeight - element.getBoundingClientRect().bottom,
      );
      expect(bottomGap).toBeLessThanOrEqual(56.5);
      expect(await pageScrollTop(page)).toBe(documentBefore);

      await page.getByRole("button", { name: "Close" }).click();
      await expect(dialog).toBeHidden();
      await expect(opener).toBeFocused();
    });
  });
}
