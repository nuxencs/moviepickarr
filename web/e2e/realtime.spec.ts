import { expect, test } from "@playwright/test";

test("a server broadcast refreshes a movie modal that is already open", async ({ page }) => {
  await page.goto("/users");
  const opener = page.locator(".mem-pane .mem-open").first();
  await expect(opener).toBeVisible();
  const detailRequestPromise = page.waitForRequest((request) =>
    /\/api\/v1\/movies\/\d+$/.test(request.url()),
  );
  await opener.click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  const movieID = Number(
    new URL((await detailRequestPromise).url()).pathname.split("/").at(-1),
  );
  const original = await page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/movies/${id}`);
    if (!response.ok) throw new Error(`detail request failed: ${response.status}`);
    return response.json();
  }, movieID);
  const changedTitle = `${original.title} (live)`;
  const link = `https://www.themoviedb.org/movie/${original.tmdbId}`;
  const update = (title: string) =>
    page.evaluate(
      async ({ id, nextTitle, nextLink }) => {
        const response = await fetch(`/api/v1/movies/${id}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ title: nextTitle, link: nextLink }),
        });
        if (!response.ok) throw new Error(`movie update failed: ${response.status}`);
      },
      { id: movieID, nextTitle: title, nextLink: link },
    );

  await update(changedTitle);
  await expect(dialog.getByRole("heading", { name: changedTitle })).toBeVisible();

  await update(original.title);
  await expect(dialog.getByRole("heading", { name: original.title })).toBeVisible();
});

test("enrichment bursts coalesce and a reconnect resyncs the open modal", async ({ page }) => {
  let releaseFirst!: () => void;
  const firstConnection = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  let connections = 0;
  const now = new Date().toISOString();

  await page.route("**/api/v1/events", async (route) => {
    connections++;
    if (connections === 1) {
      await firstConnection;
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body:
          `event: connected\ndata: ${JSON.stringify({ seq: 0, epoch: "e2e", serverNow: now })}\n\n` +
          `data: ${JSON.stringify({ seq: 1, type: "movies:enriched-batch" })}\n\n` +
          `data: ${JSON.stringify({ seq: 2, type: "movies:enriched-batch" })}\n\n`,
      });
      return;
    }
    if (connections === 2) {
      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: `event: connected\ndata: ${JSON.stringify({ seq: 2, epoch: "e2e", serverNow: now })}\n\n`,
      });
      return;
    }
    await new Promise(() => {});
  });

  await page.goto("/");
  const detailResponsePromise = page.waitForResponse((response) =>
    /\/api\/v1\/movies\/\d+$/.test(response.url()),
  );
  await page.locator('.tile-grid--pool article[role="button"]').first().click();
  const detailResponse = await detailResponsePromise;
  await expect(page.getByRole("dialog")).toBeVisible();

  const detailURL = detailResponse.url();
  let detailRequests = 0;
  page.on("request", (request) => {
    if (request.url() === detailURL) detailRequests++;
  });

  releaseFirst();
  await expect.poll(() => detailRequests).toBe(1);
  await page.waitForTimeout(200);
  expect(detailRequests, "the two enrichment events caused duplicate detail refetches").toBe(1);

  await expect.poll(() => connections, { timeout: 5_000 }).toBeGreaterThanOrEqual(2);
  await expect.poll(() => detailRequests, { timeout: 5_000 }).toBe(2);
});
