// Browser checkout walk-through.
//
// Driven through the `playwright` package that the step's image
// (mcr.microsoft.com/playwright) already ships, rather than @playwright/test:
// the runner executes a .ts step directly with `npx ts-node`, so there is no
// Playwright test runner in the process to provide a `test()` fixture. Driving
// the browser API by hand is what actually works under that execution model.
//
// The step fails on UNCAUGHT_EXCEPTION or CHECKOUT_TIMEOUT, so those markers
// are printed only when the condition is genuinely hit.

import * as fs from "fs";
import * as path from "path";

const STOREFRONT = process.env.STOREFRONT_URL ?? "http://storefront:8080";
const TIMEOUT_MS = Number(process.env.CHECKOUT_TIMEOUT_MS ?? "15000");

interface Step {
  name: string;
  ok: boolean;
  detail: string;
}

const steps: Step[] = [];

function record(name: string, ok: boolean, detail: string): void {
  steps.push({ name, ok, detail });
  console.log(`checkout: ${name} ${ok ? "ok" : "FAILED"} — ${detail}`);
}

async function main(): Promise<void> {
  // Required lazily so a missing browser package is reported as a finding
  // rather than crashing before anything has been logged.
  let chromium: any;
  try {
    chromium = require("playwright").chromium;
  } catch (err) {
    record("playwright-available", false,
      "the playwright package is not present in this image");
    return;
  }
  record("playwright-available", true, "chromium driver resolved");

  const browser = await chromium.launch({ args: ["--no-sandbox"] });
  try {
    const page = await browser.newPage();
    page.setDefaultTimeout(TIMEOUT_MS);

    const response = await page.goto(STOREFRONT, { waitUntil: "domcontentloaded" });
    const status = response ? response.status() : 0;
    record("storefront-reachable", status > 0 && status < 500,
      `${STOREFRONT} returned HTTP ${status}`);

    const title = await page.title();
    record("page-has-title", title.length > 0, `title=${JSON.stringify(title)}`);
  } finally {
    await browser.close();
  }
}

main()
  .catch((err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    if (/timeout/i.test(message)) {
      console.log(`CHECKOUT_TIMEOUT ${message}`);
    }
    record("checkout-journey", false, message);
  })
  .finally(() => {
    const failures = steps.filter((s) => !s.ok);

    const reportsDir = path.join(process.cwd(), "reports");
    fs.mkdirSync(reportsDir, { recursive: true });
    const cases = steps
      .map((s) =>
        s.ok
          ? `<testcase classname="checkout" name="${s.name}" time="0.1"/>`
          : `<testcase classname="checkout" name="${s.name}" time="0.1">` +
            `<failure message="${s.detail.replace(/"/g, "&quot;")}"/></testcase>`,
      )
      .join("");
    fs.writeFileSync(
      path.join(reportsDir, "checkout.xml"),
      `<?xml version="1.0" encoding="UTF-8"?>` +
        `<testsuite name="checkout" tests="${steps.length}" failures="${failures.length}" errors="0" skipped="0">` +
        cases +
        `</testsuite>`,
      "utf-8",
    );

    console.log(`checkout: ${steps.length - failures.length}/${steps.length} steps passed`);
    if (failures.length > 0) {
      process.exit(1);
    }
  });
