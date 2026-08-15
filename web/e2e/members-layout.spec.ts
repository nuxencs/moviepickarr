import { expect, test, type Page } from "@playwright/test";

const BASE_URL = "http://127.0.0.1:3030";

async function expectProfileInsideViewport(page: Page, widths: number[], role: string) {
  for (const width of widths) {
    await page.setViewportSize({ width, height: 640 });
    const trigger = page.getByRole("button", { name: "Your profile" });
    await expect(trigger, `${role} profile trigger at ${width}px`).toBeVisible();
    const box = await trigger.boundingBox();
    expect(box, `${role} profile trigger box at ${width}px`).not.toBeNull();
    expect(box!.x, `${role} profile trigger left edge at ${width}px`).toBeGreaterThanOrEqual(0);
    expect(
      box!.x + box!.width,
      `${role} profile trigger right edge at ${width}px`,
    ).toBeLessThanOrEqual(width);
  }
}

test("profile trigger stays reachable at every navigation breakpoint for both roles", async ({
  browser,
  browserName,
  page,
}) => {
  const widths = [320, 640, 641, 676, 677, 760, 761, 793, 794, 899, 900, 901, 1280];

  await page.goto("/");
  await expectProfileInsideViewport(page, widths, "admin");

  const memberContext = await browser.newContext({
    baseURL: BASE_URL,
    storageState: `e2e/.auth/member-${browserName}.json`,
  });
  const memberPage = await memberContext.newPage();
  await memberPage.goto("/");
  await expectProfileInsideViewport(memberPage, widths, "member");
  await memberContext.close();
});

test.describe("Members desktop geometry", () => {
  test.use({ viewport: { width: 1280, height: 640 } });

  const expectSameRenderedHeight = (actual: number, expected: number) => {
    expect(Math.abs(actual - expected)).toBeLessThan(0.5);
  };

  test("member changes, filtering, and empty stashes keep fixed board geometry", async ({ page }) => {
    await page.route("**/api/v1/members", async (route) => {
      const response = await route.fetch();
      const members = await response.json();
      const withEmptyMember = members.map((member: { name: string; stash: unknown }) =>
        member.name === "Cleo" ? { ...member, stash: {} } : member,
      );
      await route.fulfill({ response, json: withEmptyMember });
    });

    await page.goto("/users");
    await expect(page.locator(".mem")).toBeVisible();
    await page.evaluate(() => document.fonts.ready);

    const rail = page.locator(".mem-rail");
    const wall = page.locator(".mem-wallbox");
    const railHeight = (await rail.boundingBox())!.height;
    const stockedHeight = (await wall.boundingBox())!.height;

    await page.getByRole("textbox", { name: "Search Ada's stash" }).fill("no such movie");
    await expect(page.getByText('Nothing matches "no such movie"')).toBeVisible();
    expectSameRenderedHeight((await rail.boundingBox())!.height, railHeight);
    expectSameRenderedHeight((await wall.boundingBox())!.height, stockedHeight);

    await page.getByRole("link", { name: /^Cleo/ }).click();
    await expect(page.getByRole("heading", { name: "Cleo's stash" })).toBeVisible();
    await expect(page.getByText("This stash is empty")).toBeVisible();
    expectSameRenderedHeight((await rail.boundingBox())!.height, railHeight);
    expectSameRenderedHeight((await wall.boundingBox())!.height, stockedHeight);
  });

  test("the document stays fixed while the roster and stash wall own scrolling", async ({
    page,
  }) => {
    await page.goto("/users");
    await expect(page.locator(".mem")).toBeVisible();

    const geometry = await page.evaluate(() => {
      const measure = (selector: string) => {
        const element = document.querySelector<HTMLElement>(selector)!;
        return {
          clientHeight: element.clientHeight,
          overflowY: getComputedStyle(element).overflowY,
          scrollHeight: element.scrollHeight,
        };
      };
      return {
        body: measure("body"),
        rail: measure(".mem-rail"),
        wall: measure(".mem-wallbox"),
      };
    });

    expect(geometry.body.overflowY).toBe("hidden");
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
    expect(geometry.rail.overflowY).toBe("auto");
    expect(geometry.wall.overflowY).toBe("auto");
  });
});

test.describe("Members pushed layout", () => {
  test.use({ viewport: { width: 390, height: 640 } });

  test("each pushed screen has exactly one vertical scroller", async ({ page }) => {
    await page.goto("/users");
    await expect(page.locator(".mem")).toBeVisible();

    const scrollOwners = async () =>
      page.evaluate(() => {
        const candidates = [document.body, ...document.querySelectorAll<HTMLElement>("[data-page-scroll-owner]")];
        return candidates
          .filter((element) => {
            const overflow = getComputedStyle(element).overflowY;
            return (overflow === "auto" || overflow === "scroll") &&
              element.scrollHeight > element.clientHeight + 1;
          })
          .map((element) => element === document.body ? "body" : element.className);
      });

    expect(await scrollOwners()).toEqual(["body"]);

    await page.locator('.mem-row[data-active="true"] .mem-tostash').click();
    await expect(page.locator('.mem[data-pushed="true"] .mem-pane')).toBeVisible();
    expect(await scrollOwners()).toEqual(["body"]);
  });
});
