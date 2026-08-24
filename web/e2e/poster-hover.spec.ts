import { expect, test } from "@playwright/test";

const posterPath = "/poster-hover-regression.svg";
const posterImage = `
  <svg xmlns="http://www.w3.org/2000/svg" width="200" height="300" viewBox="0 0 200 300">
    <rect width="200" height="300" fill="#152238" />
    <rect x="199" width="1" height="300" fill="#fff" />
  </svg>
`;

test.use({ viewport: { width: 2560, height: 1440 } });

test("poster images cover fractional pool columns after hover", async ({ page }) => {
  await page.route("**/api/v1/movies/pool", async (route) => {
    const response = await route.fetch();
    const movies = await response.json();
    await route.fulfill({
      response,
      json: movies.map((movie: object) => ({ ...movie, posterPath })),
    });
  });
  await page.route("https://image.tmdb.org/t/p/**/poster-hover-regression.svg", (route) =>
    route.fulfill({ contentType: "image/svg+xml", body: posterImage }),
  );

  await page.goto("/");
  const tiles = page.locator(".tile-grid--pool .tile");
  await expect(tiles.first().locator(".poster__img")).toBeVisible();

  const secondTile = tiles.nth(1);
  await secondTile.hover();
  await page.mouse.move(2500, 1200);
  await page.waitForTimeout(200);

  const overlaps = await tiles.evaluateAll((elements) =>
    elements.slice(0, 6).map((tile) => {
      const frame = tile.querySelector<HTMLElement>(".poster")!.getBoundingClientRect();
      const image = tile.querySelector<HTMLImageElement>(".poster__img")!.getBoundingClientRect();
      return {
        left: frame.left - image.left,
        right: image.right - frame.right,
      };
    }),
  );

  for (const [index, overlap] of overlaps.entries()) {
    expect(overlap.left, `poster ${index + 1} left overlap`).toBeGreaterThanOrEqual(0.9);
    expect(overlap.right, `poster ${index + 1} right overlap`).toBeGreaterThanOrEqual(0.9);
  }
});
