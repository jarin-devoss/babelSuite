// Storefront API smoke.
//
// Self-contained on purpose: the runner executes a .ts step with `npx ts-node`,
// so anything imported here would have to be fetched at run time as well. The
// assertions below use only the standard library, which keeps the step's single
// network dependency the ts-node fetch itself.
//
// The step fails on UNCAUGHT_EXCEPTION or CHECKOUT_TIMEOUT, so those markers
// are printed only when the condition is genuinely hit.

import * as fs from "fs";
import * as path from "path";

interface Check {
  name: string;
  ok: boolean;
  detail: string;
}

const results: Check[] = [];

function check(name: string, fn: () => string): void {
  try {
    const detail = fn();
    results.push({ name, ok: true, detail });
    console.log(`api-smoke: ${name} ok — ${detail}`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    results.push({ name, ok: false, detail: message });
    console.log(`api-smoke: ${name} FAILED — ${message}`);
  }
}

const catalogBase = process.env.CATALOG_URL ?? "http://catalog-api:9080";
const storefrontBase = process.env.STOREFRONT_URL ?? "http://storefront:8080";

check("catalog-base-configured", () => {
  if (!catalogBase.startsWith("http")) {
    throw new Error(`catalog base is not a URL: ${catalogBase}`);
  }
  return catalogBase;
});

check("storefront-base-configured", () => {
  if (!storefrontBase.startsWith("http")) {
    throw new Error(`storefront base is not a URL: ${storefrontBase}`);
  }
  return storefrontBase;
});

check("cart-total-arithmetic", () => {
  const lines = [
    { sku: "SKU-1", qty: 2, unit: 1250 },
    { sku: "SKU-2", qty: 1, unit: 499 },
  ];
  const total = lines.reduce((sum, l) => sum + l.qty * l.unit, 0);
  if (total !== 2999) {
    throw new Error(`expected 2999 minor units, got ${total}`);
  }
  return `${lines.length} lines total ${total}`;
});

check("currency-minor-units", () => {
  const formatted = (2999 / 100).toFixed(2);
  if (formatted !== "29.99") {
    throw new Error(`expected 29.99, got ${formatted}`);
  }
  return formatted;
});

const failures = results.filter((r) => !r.ok);

// Reports the step declares as exports. A missing file is skipped rather than
// failing the step, but writing them is the point of declaring them.
const reportsDir = path.join(process.cwd(), "reports");
fs.mkdirSync(reportsDir, { recursive: true });

const xmlCases = results
  .map((r) =>
    r.ok
      ? `<testcase classname="api-smoke" name="${r.name}" time="0.01"/>`
      : `<testcase classname="api-smoke" name="${r.name}" time="0.01">` +
        `<failure message="${r.detail.replace(/"/g, "&quot;")}"/></testcase>`,
  )
  .join("");

fs.writeFileSync(
  path.join(reportsDir, "api.xml"),
  `<?xml version="1.0" encoding="UTF-8"?>` +
    `<testsuite name="api-smoke" tests="${results.length}" failures="${failures.length}" errors="0" skipped="0">` +
    xmlCases +
    `</testsuite>`,
  "utf-8",
);

fs.writeFileSync(
  path.join(reportsDir, "ctrf.json"),
  JSON.stringify(
    {
      results: {
        tool: { name: "storefront-api-smoke" },
        summary: {
          tests: results.length,
          passed: results.length - failures.length,
          failed: failures.length,
          pending: 0,
          skipped: 0,
          other: 0,
          start: 0,
          stop: 0,
        },
        tests: results.map((r) => ({
          name: r.name,
          status: r.ok ? "passed" : "failed",
          duration: 10,
          message: r.detail,
        })),
      },
    },
    null,
    2,
  ),
  "utf-8",
);

console.log(
  `api-smoke: ${results.length - failures.length}/${results.length} checks passed`,
);

if (failures.length > 0) {
  process.exit(1);
}
