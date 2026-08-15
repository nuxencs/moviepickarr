import { expect, test } from "@playwright/test";

test("movie detail loads lazily and its genre deep-link persists through history", async ({ page }) => {
  let releaseDetail!: () => void;
  const detailGate = new Promise<void>((resolve) => {
    releaseDetail = resolve;
  });

  await page.route(/\/api\/v1\/movies\/\d+$/, async (route) => {
    await detailGate;
    const response = await route.fetch();
    const detail = await response.json();
    await route.fulfill({
      response,
      json: {
        ...detail,
        genres: ["Drama", "Mystery"],
        overview: "A browser-loaded overview that is absent from the lean watched tile.",
      },
    });
  });

  await page.goto("/");
  const opener = page.locator(".watch-body article[role=button]").first();
  await expect(opener).toBeVisible();
  const title = (await opener.locator(".t-title").textContent())?.trim();
  expect(title).toBeTruthy();
  await opener.click();

  const dialog = page.getByRole("dialog", { name: title });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("heading", { name: title })).toBeVisible();
  await expect(dialog.locator(".moviemodal__overview[aria-hidden=true]")).toBeVisible();
  await expect(dialog.getByText("browser-loaded overview")).toHaveCount(0);

  releaseDetail();
  await expect(dialog.getByText(/browser-loaded overview/)).toBeVisible();
  await dialog.getByRole("link", { name: "Drama" }).click();

  await expect(page).toHaveURL(/\/stats\?genre=Drama$/);
  await expect(page.getByRole("button", { name: /Genre · Drama/ })).toBeVisible();
  await expect(dialog).toBeHidden();

  await page.reload();
  await expect(page).toHaveURL(/\/stats\?genre=Drama$/);
  await expect(page.getByRole("button", { name: /Genre · Drama/ })).toBeVisible();

  await page.getByRole("button", { name: "All", exact: true }).click();
  await expect(page).toHaveURL(/win=all-time/);
  await expect(page).toHaveURL(/genre=Drama/);

  await page.goBack();
  await expect(page).toHaveURL(/\/stats\?genre=Drama$/);
  await expect(page.getByRole("button", { name: /Genre · Drama/ })).toBeVisible();

  await page.goForward();
  await expect(page).toHaveURL(/win=all-time/);
  await expect(page.getByRole("button", { name: /Genre · Drama/ })).toBeVisible();
});
