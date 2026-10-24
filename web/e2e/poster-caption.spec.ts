import { expect, test, type Locator, type Page } from "@playwright/test";

const surfaces = [
  // Dev fixtures carry no TMDB art, so pool and watched posters paint their own title.
  { name: "pool", path: "/", tile: ".tile-grid--pool .tile", paintsTitle: true },
  { name: "watched grid", path: "/", tile: ".watch-body .tile", paintsTitle: true },
  { name: "stats rail", path: "/stats", tile: ".movierail .movietile", paintsTitle: false },
];

async function firstTile(page: Page, path: string, selector: string): Promise<Locator> {
  await page.goto(path);
  const tile = page.locator(selector).first();
  await expect(tile).toBeVisible();
  return tile;
}

for (const surface of surfaces) {
  test.describe(surface.name, () => {
    test("caption sits on the poster and shows only on hover and keyboard focus", async ({ page }) => {
      const tile = await firstTile(page, surface.path, surface.tile);
      const caption = tile.locator(".postercap");
      await expect(caption.locator(".postercap__title")).not.toBeEmpty();
      await expect(caption.locator(".postercap__adder")).not.toBeEmpty();

      // The tile is the poster: nothing hangs below it, and the caption is inside it.
      const tileBox = (await tile.boundingBox())!;
      const posterBox = (await tile.locator(".poster").boundingBox())!;
      const captionBox = (await caption.boundingBox())!;
      expect(Math.abs(tileBox.height - posterBox.height)).toBeLessThanOrEqual(1);
      expect(Math.abs(captionBox.y + captionBox.height - (posterBox.y + posterBox.height))).toBeLessThanOrEqual(1);
      expect(captionBox.width).toBeCloseTo(posterBox.width, 0);

      await expect(caption).toHaveCSS("opacity", "0");
      const painted = tile.locator(".poster__title");
      await expect(painted).toHaveCount(surface.paintsTitle ? 1 : 0);
      await tile.hover();
      await expect(caption).toHaveCSS("opacity", "1");
      if (surface.paintsTitle) await expect(painted).toHaveCSS("opacity", "0");

      await page.mouse.move(0, 0);
      await expect(caption).toHaveCSS("opacity", "0");
      await tile.focus();
      await page.keyboard.press("Shift+Tab");
      await page.keyboard.press("Tab");
      await expect(tile).toBeFocused();
      await expect(caption).toHaveCSS("opacity", "1");
    });
  });
}

test.describe("touch", () => {
  test.use({ hasTouch: true, isMobile: true });
  test.skip(({ browserName }) => browserName === "firefox", "Firefox has no isMobile emulation");

  test("caption never shows without hover", async ({ page }) => {
    const tile = await firstTile(page, "/", ".tile-grid--pool .tile");
    expect(await page.evaluate(() => matchMedia("(hover: none)").matches)).toBe(true);
    await tile.hover();
    // Long enough for the reveal transition to have finished had it started.
    await page.waitForTimeout(300);
    await expect(tile.locator(".postercap")).toHaveCSS("opacity", "0");
  });
});
