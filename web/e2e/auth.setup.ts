import { expect, test as setup } from "@playwright/test";

setup("sign in as the fixture admin", async ({ browserName, page }) => {
  await page.goto("/login");
  await page.getByRole("textbox", { name: "Username" }).fill("ada");
  await page.getByRole("textbox", { name: "Password" }).fill("devpassword");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("button", { name: "Your profile" })).toBeVisible();
  await page.context().storageState({ path: `e2e/.auth/admin-${browserName}.json` });
});
