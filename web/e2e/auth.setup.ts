import { expect, test as setup, type Browser } from "@playwright/test";

async function signIn(browser: Browser, username: string, statePath: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto("/login");
  await page.getByRole("textbox", { name: "Username" }).fill(username);
  await page.getByRole("textbox", { name: "Password" }).fill("devpassword");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("button", { name: "Your profile" })).toBeVisible();
  await context.storageState({ path: statePath });
  await context.close();
}

setup("sign in as the fixture admin and member", async ({ browser, browserName }) => {
  await signIn(browser, "ada", `e2e/.auth/admin-${browserName}.json`);
  await signIn(browser, "ben", `e2e/.auth/member-${browserName}.json`);
});
