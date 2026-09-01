import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 390, height: 640 } });

test("the stacked invite controls keep one usable height", async ({ page }) => {
  await page.goto("/admin");

  const name = page.getByRole("textbox", { name: "New member name" }).locator("..");
  const role = page.getByRole("combobox", { name: "Starting role" }).locator("..");
  const submit = page.getByRole("button", { name: "Add & create link" });

  await expect(name).toBeVisible();
  const [nameBox, roleBox, submitBox] = await Promise.all([
    name.boundingBox(),
    role.boundingBox(),
    submit.boundingBox(),
  ]);
  expect(nameBox).not.toBeNull();
  expect(roleBox).not.toBeNull();
  expect(submitBox).not.toBeNull();

  expect(nameBox!.height).toBeGreaterThanOrEqual(40);
  expect(Math.abs(nameBox!.height - roleBox!.height)).toBeLessThan(0.5);
  expect(Math.abs(nameBox!.height - submitBox!.height)).toBeLessThan(0.5);
});
