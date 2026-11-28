import { test, expect } from "@playwright/test";

test.use({ baseURL: process.env.BASE_URL });

test("a shopper checks out an item in stock", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Storefront Browser Lab" })).toBeVisible();
  await expect(page.getByText("Starter Keyboard $49.00")).toBeVisible();

  await page.getByRole("button", { name: "Add Starter Keyboard" }).click();
  await expect(page.getByText("Total: $49.00")).toBeVisible();
  await page.getByRole("button", { name: "Check out" }).click();

  await expect(page.getByRole("status")).toHaveText("Order confirmed ord_7001");
  await expect(page.getByText("Total: $0.00")).toBeVisible();
});

test("a shopper cannot buy the sold-out item", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Add Sold-out Mouse" }).click();
  await page.getByRole("button", { name: "Check out" }).click();

  await expect(page.getByRole("status")).toHaveText("Sold-out Mouse: Out of stock");
  await expect(page.getByText("Order confirmed")).toHaveCount(0);
});
