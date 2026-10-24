import { expect, test } from "@playwright/test";

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

  test("the library renders a window of rows and follows the Admin body scroll", async ({ page }) => {
    await page.goto("/admin/maintenance");
    const list = page.getByRole("list", { name: "Movie library" });
    const rows = list.getByRole("listitem");
    await expect(rows.first()).toBeVisible();

    const total = Number(await rows.first().getAttribute("aria-setsize"));
    const rendered = await rows.count();
    expect(total).toBeGreaterThan(100);
    expect(rendered).toBeLessThan(total);

    // At 901px and up the Admin body owns the scroll, not the document.
    await page.locator(".admin-layout__body").evaluate((body) => {
      body.scrollTop = body.scrollHeight;
    });
    await expect(list.locator(`[aria-posinset="${total}"]`)).toBeVisible();
  });
});
