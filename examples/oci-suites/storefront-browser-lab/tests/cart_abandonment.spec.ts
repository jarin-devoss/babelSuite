import { test, expect } from "@playwright/test";

test.use({ baseURL: process.env.BASE_URL });

test("an empty cart cannot be checked out", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Check out" })).toBeDisabled();
  await expect(page.getByText("Total: $0.00")).toBeVisible();
});

test("a cart the shopper walks away from places no order", async ({ page }) => {
  const orders: string[] = [];
  page.on("request", (request) => {
    if (request.url().endsWith("/api/orders")) orders.push(request.url());
  });
  await page.goto("/");
  await page.getByRole("button", { name: "Add Launch Headset" }).click();
  await expect(page.getByText("Total: $129.00")).toBeVisible();
  await page.goto("/");
  await expect(page.getByText("Total: $0.00")).toBeVisible();
  expect(orders).toHaveLength(0);
});
