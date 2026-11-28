// Storefront API smoke.
//
// Runs with the image's own Node (types are stripped, nothing is installed), so it
// uses only global fetch. The storefront answers from the catalog mock: these
// checks fail if the storefront stops asking the catalog, or confirms an order the
// catalog refused.
//
// The step fails on ORDER_CONFIRMED_FOR_SOLD_OUT, which is printed only when that
// is what happened.

import * as fs from "fs";
import * as path from "path";

const base = process.env.STOREFRONT_URL ?? "http://storefront:8080";
const results: { name: string; error: string }[] = [];

async function call(method: string, route: string, body?: unknown): Promise<{ status: number; json: any }> {
  const reply = await fetch(base + route, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return { status: reply.status, json: await reply.json().catch(() => ({})) };
}

async function check(name: string, fn: () => Promise<void>): Promise<void> {
  let error = "";
  try {
    await fn();
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  }
  results.push({ name, error });
  console.log(`${error === "" ? "ok  " : "FAIL"} ${name}${error === "" ? "" : " - " + error}`);
}

function expect(condition: boolean, message: string): void {
  if (!condition) throw new Error(message);
}

await check("the product grid is the catalog's", async () => {
  const { status, json } = await call("GET", "/api/products");
  expect(status === 200, `GET /api/products gave ${status}`);
  const keyboard = json.products.find((p: any) => p.sku === "sku_1001");
  expect(keyboard !== undefined && keyboard.name === "Starter Keyboard" && keyboard.price === 4900, `the catalog's keyboard is missing: ${JSON.stringify(json)}`);
});

await check("an order for an item in stock is confirmed", async () => {
  const { status, json } = await call("POST", "/api/orders", { email: "shopper@demo.test", sku: "sku_1001", quantity: 1 });
  expect(status === 201 && json.orderId === "ord_7001", `an order gave ${status} ${JSON.stringify(json)}`);
});

await check("an order the catalog refuses is not confirmed", async () => {
  const { status, json } = await call("POST", "/api/orders", { email: "shopper@demo.test", sku: "sku_4040", quantity: 1 });
  if (status === 201) console.log("ORDER_CONFIRMED_FOR_SOLD_OUT sku_4040");
  expect(status === 409 && json.error === "Out of stock", `the sold-out item gave ${status} ${JSON.stringify(json)}`);
});

await check("an invalid order is refused", async () => {
  for (const bad of [{}, { email: "shopper@demo.test", sku: "sku_1001", quantity: 0 }, { email: "nobody", sku: "sku_1001", quantity: 1 }]) {
    const { status } = await call("POST", "/api/orders", bad);
    expect(status === 400, `${JSON.stringify(bad)} gave ${status}, want 400`);
  }
});

const failed = results.filter((r) => r.error !== "");
const escapeXml = (text: string) => text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
fs.mkdirSync("reports", { recursive: true });
fs.writeFileSync(
  path.join("reports", "api.xml"),
  `<?xml version="1.0" encoding="UTF-8"?><testsuite name="api-smoke" tests="${results.length}" failures="${failed.length}" errors="0" skipped="0">` +
    results.map((r) => (r.error === "" ? `<testcase classname="api-smoke" name="${escapeXml(r.name)}"/>` : `<testcase classname="api-smoke" name="${escapeXml(r.name)}"><failure message="${escapeXml(r.error)}"/></testcase>`)).join("") +
    `</testsuite>`,
);
fs.writeFileSync(
  path.join("reports", "ctrf.json"),
  JSON.stringify({
    results: {
      tool: { name: "storefront-api-smoke" },
      summary: { tests: results.length, passed: results.length - failed.length, failed: failed.length, pending: 0, skipped: 0, other: 0, start: 0, stop: 0 },
      tests: results.map((r) => ({ name: r.name, status: r.error === "" ? "passed" : "failed", duration: 10, message: r.error })),
    },
  }),
);
console.log(`api-smoke: ${results.length - failed.length}/${results.length} checks passed`);
process.exit(failed.length > 0 ? 1 : 0);
