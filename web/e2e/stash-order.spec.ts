import { expect, test, type Page } from "@playwright/test";

// Every order gives a different wall: title A to Z is Arrival, Brazil, Cure.
const STASH = {
  "9001": { title: "Brazil", addedAt: "2026-01-03T12:00:00Z" },
  "9002": { title: "Arrival", addedAt: "2026-01-01T12:00:00Z" },
  "9003": { title: "Cure", addedAt: "2026-01-02T12:00:00Z" },
};

async function stockAdaStash(page: Page) {
  await page.route("**/api/v1/members", async (route) => {
    const response = await route.fetch();
    const members = await response.json();
    const stocked = members.map((member: { userID: number; name: string }) =>
      member.name === "Ada"
        ? {
            ...member,
            stash: Object.fromEntries(
              Object.entries(STASH).map(([id, movie]) => [
                id,
                {
                  ...movie,
                  movieID: Number(id),
                  link: "",
                  addedByID: member.userID,
                  addedByName: member.name,
                },
              ]),
            ),
          }
        : member,
    );
    await route.fulfill({ response, json: stocked });
  });
}

const wallTitles = (page: Page) =>
  page.locator(".mem-wall .mem-open").evaluateAll((cells) => cells.map((c) => c.getAttribute("aria-label")));

async function chooseOrder(page: Page, from: string, to: string) {
  await page.getByRole("button", { name: `Sort stash, ${from}` }).click();
  await expect(page.getByRole("menuitemradio", { name: from })).toHaveAttribute("aria-checked", "true");
  await page.getByRole("menuitemradio", { name: to }).click();
  await expect(page.getByRole("menu")).toHaveCount(0);
  await expect(page.getByRole("button", { name: `Sort stash, ${to}` })).toBeFocused();
}

test("the sort menu reorders the stash wall and the order outlives a reload", async ({ page }) => {
  await stockAdaStash(page);
  await page.goto("/users");
  await expect(page.getByRole("heading", { name: "Your stash" })).toBeVisible();

  await expect.poll(() => wallTitles(page)).toEqual(["Arrival", "Brazil", "Cure"]);

  await chooseOrder(page, "Title A–Z", "Newest added");
  await expect.poll(() => wallTitles(page)).toEqual(["Brazil", "Cure", "Arrival"]);

  await chooseOrder(page, "Newest added", "Title Z–A");
  await expect.poll(() => wallTitles(page)).toEqual(["Cure", "Brazil", "Arrival"]);

  await chooseOrder(page, "Title Z–A", "Oldest added");
  await expect.poll(() => wallTitles(page)).toEqual(["Arrival", "Cure", "Brazil"]);

  await page.reload();
  await expect.poll(() => wallTitles(page)).toEqual(["Arrival", "Cure", "Brazil"]);

  // A viewer preference: another member's wall opens in the same order.
  await page.getByRole("link", { name: /^Cleo/ }).click();
  await expect(page.getByRole("heading", { name: "Cleo's stash" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Sort stash, Oldest added" })).toBeVisible();
});

async function expectSortBesideSearch(page: Page, headHeight: number) {
  await expect(page.getByRole("button", { name: /^Sort stash, / })).toBeInViewport();
  // One snapshot after entrances settle: separate reads can straddle animation frames.
  const [head, field, sort] = await page.evaluate(async () => {
    await Promise.all(document.getAnimations().map((animation) => animation.finished));
    const head = document.querySelector(".mem-pane:not([inert]) .mem-stash__head")!;
    return [head, head.querySelector(".field")!, head.querySelector(".mem-sort")!].map((el) =>
      el.getBoundingClientRect().toJSON(),
    ) as DOMRect[];
  });

  expect(Math.abs(head.height - headHeight)).toBeLessThan(0.5);
  expect(Math.abs(sort.height - field.height)).toBeLessThan(0.5);
  expect(Math.abs(sort.width - field.height)).toBeLessThan(0.5);
  expect(Math.abs(sort.y - field.y)).toBeLessThan(0.5);
  expect(sort.x).toBeGreaterThan(field.x + field.width);
  expect(sort.x + sort.width).toBeLessThanOrEqual(head.x + head.width + 0.5);
}

test.describe("sort button geometry", () => {
  test("one row with the search field, wide and stacked", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 640 });
    await page.goto("/users");
    await expectSortBesideSearch(page, 34);

    await page.setViewportSize({ width: 820, height: 640 });
    await expectSortBesideSearch(page, 65);
  });

  test.describe("pushed, on a coarse pointer", () => {
    test.use({ viewport: { width: 390, height: 640 }, hasTouch: true, isMobile: true });

    test("keeps the stacked head height", async ({ page }) => {
      await page.goto("/users");
      expect(await page.evaluate(() => matchMedia("(pointer: coarse)").matches)).toBe(true);
      await page.locator('.mem-row[data-active="true"] .mem-tostash').click();
      await expect(page.getByRole("heading", { name: "Your stash" })).toBeInViewport();
      await expectSortBesideSearch(page, 65);
    });
  });
});
