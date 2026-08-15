import { expect, test } from "@playwright/test";

const BASE_URL = "http://127.0.0.1:3030";

test("draw spins, survives a tab remount, reveals on its deadline, and confirms", async ({ page }) => {
  const membersResponse = await page.request.get("/api/v1/members");
  expect(membersResponse.ok()).toBe(true);
  const members = await membersResponse.json() as Array<{
    currentPool: Record<string, { movieID: number; title: string }>;
  }>;
  const protectedMovie = members
    .flatMap((member) => Object.values(member.currentPool))
    .find((movie) => movie.title === "American Beauty");
  expect(protectedMovie, "the shared modal-layout fixture is missing").toBeTruthy();
  const protectResponse = await page.request.post(`/api/v1/movies/${protectedMovie!.movieID}/move`, {
    data: { target: "stash" },
    headers: { Origin: BASE_URL },
  });
  expect(protectResponse.ok(), await protectResponse.text()).toBe(true);

  await page.goto("/users");
  await expect(page.locator(".mem")).toBeVisible();
  await page.evaluate(() => document.fonts.ready);

  const poolTile = page.locator('.mem-row[data-active="true"] .pslot--filled').first();
  const movieButton = poolTile.locator(".mem-open");
  const movieTitle = await movieButton.getAttribute("aria-label");
  expect(movieTitle).toBeTruthy();
  const openTile = await poolTile.screenshot({ animations: "disabled" });

  await page.getByRole("link", { name: /^Movies/ }).click();
  const drawResponsePromise = page.waitForResponse(
    (response) => response.url().endsWith("/api/v1/movies/random") && response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Draw random movie" }).click();
  const drawResponse = await drawResponsePromise;
  const draw = await drawResponse.json();
  const responseAt = Date.now();

  const reel = page.getByRole("dialog", { name: "Drawing a random movie" });
  await expect(reel).toBeVisible();
  const track = reel.locator(".drawreel__track");
  const firstTransform = await track.evaluate((element) => getComputedStyle(element).transform);
  await expect
    .poll(() => track.evaluate((element) => getComputedStyle(element).transform))
    .not.toBe(firstTransform);

  await page.getByRole("link", { name: /^Members/ }).click();
  await expect(page.locator(".mem")).toBeVisible();
  const heldTile = page
    .locator('.mem-row[data-active="true"] .pslot--filled')
    .filter({ has: page.locator(`.mem-open[aria-label=${JSON.stringify(movieTitle)}]`) });
  await expect(heldTile.getByRole("button", { name: /draw is in progress/ })).toBeAttached();
  const lockedTile = await heldTile.screenshot({ animations: "disabled" });
  expect(lockedTile.equals(openTile), "a refused tile changed at rest during the draw").toBe(true);

  await page.getByRole("link", { name: /^Movies/ }).click();
  await expect(reel).toBeVisible();
  await expect(reel.getByRole("button", { name: "Skip" })).toBeFocused();
  await reel.getByRole("button", { name: "Skip" }).click();

  const confirm = reel.getByRole("button", { name: "OK" });
  await expect(confirm).toBeVisible();
  await expect(confirm).toBeFocused();
  const durationMs = await reel.locator(".drawreel__ok-fill").evaluate((element) =>
    Number.parseFloat(getComputedStyle(element).animationDuration) * 1000,
  );
  const expectedRemaining =
    Date.parse(draw.revealAt) - Date.parse(draw.serverNow) - (Date.now() - responseAt);
  expect(Math.abs(durationMs - expectedRemaining)).toBeLessThan(1_500);

  await confirm.click();
  await expect(reel).toBeHidden();
  await expect(page.getByRole("button", { name: "Mark as watched" })).toBeVisible();

  // Restore a no-current-draw baseline for the next browser project.
  await page.getByRole("button", { name: "Mark as watched" }).click();
  await expect(page.getByRole("button", { name: "Draw random movie" })).toBeVisible();

  const restoreResponse = await page.request.post(`/api/v1/movies/${protectedMovie!.movieID}/move`, {
    data: { target: "pool" },
    headers: { Origin: BASE_URL },
  });
  expect(restoreResponse.ok(), await restoreResponse.text()).toBe(true);
});
