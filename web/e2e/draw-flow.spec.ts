import { expect, type Page, test } from "@playwright/test";

const BASE_URL = "http://127.0.0.1:3030";

/** Admins get no turn exception, so the spec moves Next up to the signed-in
 *  admin with Turn skips before it runs the turn. */
async function takeTurn(page: Page) {
  const me = await (await page.request.get("/api/v1/auth/me")).json() as { id: number };
  for (let step = 0; step < 10; step++) {
    const nextUp = await (await page.request.get("/api/v1/settings/next-up")).json() as { id: number };
    if (nextUp.id === me.id) return;
    const skip = await page.request.post("/api/v1/settings/next-up/skip", {
      data: { memberId: nextUp.id },
      headers: { Origin: BASE_URL },
    });
    expect(skip.ok(), await skip.text()).toBe(true);
  }
  throw new Error("Next up never reached the signed-in admin");
}

/** Hero height and where its actions sit; a pick must change neither (#305). Desktop only until #357. */
async function heroGeometry(page: Page) {
  const [hero, actions] = await Promise.all([
    page.locator(".hero").boundingBox(),
    page.locator(".hero__actions").boundingBox(),
  ]);
  expect(hero).not.toBeNull();
  expect(actions).not.toBeNull();
  return { height: Math.round(hero!.height), actionsTop: Math.round(actions!.y - hero!.y) };
}

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
  await takeTurn(page);

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
  const heroAtRest = await heroGeometry(page);

  // A Wildcard is watched without replacing this draw or moving Next up.
  const currentTitle = await page.locator(".hero__title").textContent();
  // Reveal passes the turn on; wait for that handoff before reading the label.
  await expect(page.locator(".hero__nextup .nm")).not.toHaveText("Your turn");
  const nextUp = await page.locator(".hero__nextup").textContent();
  await page.getByRole("button", { name: "Choose wildcard" }).click();
  const picker = page.getByRole("dialog", { name: "Choose a wildcard" });
  await expect(picker).toBeVisible();
  const firstResult = picker.locator(".result").first();
  const firstWildcardTitle = await firstResult.locator(".r-title").textContent();
  expect(firstWildcardTitle).toBeTruthy();
  await firstResult.hover();
  await firstResult.getByRole("button", { name: "Choose" }).click();

  const activeWildcard = page.getByText(/Active wildcard · added by/);
  await expect(activeWildcard).toBeVisible();
  await expect(page.locator(".hero__title")).toHaveText(firstWildcardTitle ?? "");
  await expect(page.locator(".hero__nextup")).toHaveText(nextUp ?? "");
  await expect(page.getByRole("button", { name: "Mark as watched" })).toBeEnabled();
  await expect(page.getByRole("button", { name: "Cancel wildcard" })).toBeVisible();
  expect(await heroGeometry(page), "the wildcard takeover resized the hero").toEqual(heroAtRest);

  const heldDraw = page.locator(".hero__held-draw");
  await heldDraw.getByRole("button", { name: currentTitle ?? "" }).click();
  await expect(page.getByRole("dialog", { name: currentTitle ?? "" })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("dialog", { name: currentTitle ?? "" })).toBeHidden();
  await expect(page.locator(".hero__title")).toHaveText(firstWildcardTitle ?? "");

  await page.getByRole("button", { name: "Mark as watched" }).click();
  await expect(activeWildcard).toBeHidden();
  await expect(page.locator(".hero__title")).toHaveText(currentTitle ?? "");
  await expect(page.locator(".hero__nextup")).toHaveText(nextUp ?? "");

  await page.getByRole("button", { name: "Choose wildcard" }).click();
  const secondResult = picker.locator(".result").first();
  await secondResult.hover();
  await secondResult.getByRole("button", { name: "Choose" }).click();
  await expect(activeWildcard).toBeVisible();
  await page.getByRole("button", { name: "Cancel wildcard" }).click();
  const cancellation = page.getByRole("dialog", { name: "Cancel this wildcard?" });
  await cancellation.getByRole("button", { name: "Cancel wildcard" }).click();
  await expect(activeWildcard).toBeHidden();
  await expect(page.locator(".hero__title")).toHaveText(currentTitle ?? "");
  await expect(page.locator(".hero__nextup")).toHaveText(nextUp ?? "");

  // Skip the member the reveal passed the turn to, then take it back to mark watched.
  await page.getByRole("button", { name: /^Skip .+'s turn$/ }).click();
  const skipDialog = page.getByRole("dialog", { name: /^Skip .+ turn\?$/ });
  await skipDialog.getByRole("button", { name: "Skip turn" }).click();
  await expect(skipDialog).toBeHidden();
  await expect(page.locator(".hero__nextup")).not.toHaveText(nextUp ?? "");
  await takeTurn(page);
  await expect(page.locator(".hero__nextup .nm")).toHaveText("Your turn");

  // Restore a no-current-draw baseline for the next browser project.
  await page.getByRole("button", { name: "Mark as watched" }).click();
  await expect(page.getByRole("button", { name: "Draw random movie" })).toBeVisible();
  expect(await heroGeometry(page), "the empty hero changed height").toEqual(heroAtRest);

  const restoreResponse = await page.request.post(`/api/v1/movies/${protectedMovie!.movieID}/move`, {
    data: { target: "pool" },
    headers: { Origin: BASE_URL },
  });
  expect(restoreResponse.ok(), await restoreResponse.text()).toBe(true);
});
