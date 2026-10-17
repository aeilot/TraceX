// Optional end-to-end check. Requires a local TraceX default demo server and Playwright.
const assert = require("node:assert/strict");
async function smoke(page, baseURL = "http://127.0.0.1:4318") {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(baseURL);
  await page.waitForSelector(".dag-node");
  assert.equal(await page.locator("#run-picker option").count(), 2);
  assert.equal(await page.locator(".dag-node").count(), 18);
  assert.equal(await page.locator(".edge-join").count(), 3);
  await page.locator("#highlight-critical").click();
  assert.ok((await page.locator(".dag-node.critical").count()) > 2);
  await page.locator('.dag-node[aria-label*="Plan evidence"]').click();
  await page.waitForSelector("#inspector:not([hidden])");
  assert.match(
    await page.locator("#inspector-content").innerText(),
    /Input tokens/,
  );
  assert.match(
    await page.locator("#inspector-content").innerText(),
    /runtime context/,
  );
  await page.keyboard.press("Escape");
  await page.locator('[data-view="timeline"]').click();
  assert.equal(await page.locator("#dag-panel").isHidden(), true);
  await page.locator("#range-start").fill("12");
  await page.locator("#range-end").fill("28");
  await page.locator("#apply-range").click();
  await page.waitForFunction(() =>
    document.querySelector("#time-window").textContent.startsWith("12.0 s"),
  );
  assert.match(await page.locator("#time-window").innerText(), /28.0 s/);
  await page.locator("#full-range").click();
  await page.waitForFunction(() =>
    document.querySelector("#time-window").textContent.startsWith("0 ms"),
  );
  await page.locator("#search").fill("researcher-b");
  await page.waitForFunction(
    () => document.querySelectorAll(".timeline-bar").length === 4,
  );
  await page.locator("#search").fill("");
  await page.locator("#kind-filter").selectOption("lifecycle");
  await page.waitForFunction(
    () => document.querySelectorAll(".timeline-bar").length === 2,
  );
  await page.locator("#kind-filter").selectOption("all");
  await page.waitForFunction(
    () => document.querySelectorAll(".timeline-bar").length === 18,
  );
  await page.locator("#comparison-details summary").click();
  await page.locator("#compare-left").selectOption("run-1");
  await page.locator("#compare-right").selectOption("run-2");
  await page.locator("#compare-button").click();
  await page.waitForSelector("#comparison-result table");
  assert.match(await page.locator("#comparison-result").innerText(), /25.0%/);
  assert.match(
    await page.locator("#comparison-result").innerText(),
    /conditions differ: model/,
  );
  const p = await page.evaluate(() =>
    fetch("/api/profile?run=run-1").then((r) => r.json()),
  );
  const large = structuredClone(p);
  large.name = "Synthetic pagination check";
  large.edges = [];
  large.spans = Array.from({ length: 630 }, (_, i) => ({
    id: "page-span-" + String(i).padStart(4, "0"),
    task_id: p.tasks[0].id,
    agent_id: "pagination-agent",
    name: "Synthetic page span " + i,
    kind: "tool",
    status: "succeeded",
    start_ms: i * 50,
    duration_ms: 20,
    source: "Synthetic browser check",
  }));
  await page.locator("#import-file").setInputFiles({
    name: "pagination.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(large)),
  });
  await page.waitForFunction(
    () =>
      document.querySelector("#run-name").textContent ===
      "Synthetic pagination check",
  );
  assert.match(await page.locator("#page-label").innerText(), /1–250 of 630/);
  await page.locator("#page-next").click();
  await page.waitForFunction(() =>
    document.querySelector("#page-label").textContent.includes("251–500"),
  );
  await page.locator("#page-size").selectOption("50");
  await page.waitForFunction(() =>
    document.querySelector("#page-label").textContent.includes("1–50"),
  );
  await page.locator("#search").fill("page-span-0629");
  await page.waitForFunction(
    () => document.querySelectorAll(".timeline-bar").length === 1,
  );
  const downloaded = page.waitForEvent("download");
  await page.locator("#export-button").click();
  const download = await downloaded;
  const exported = JSON.parse(
    require("node:fs").readFileSync(await download.path(), "utf8"),
  );
  assert.equal(
    exported.spans.length,
    630,
    "export must include off-page spans",
  );
  const invalid = structuredClone(large);
  invalid.spans[0].parent_id = invalid.spans[1].id;
  invalid.spans[1].parent_id = invalid.spans[0].id;
  await page.locator("#import-file").setInputFiles({
    name: "cyclic.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(invalid)),
  });
  await page.waitForFunction(() =>
    document.querySelector("#toast").textContent.includes("cycle"),
  );
  assert.equal(
    await page.locator("#run-name").innerText(),
    "Synthetic pagination check",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(200);
  await page.locator("#run-picker-mobile").selectOption("run-2");
  await page.waitForFunction(() =>
    document.querySelector("#run-name").textContent.includes("slower model"),
  );
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
    "mobile overflow",
  );
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.waitForTimeout(200);
  await page.locator("#run-picker").selectOption("run-1");
  await page.waitForFunction(() =>
    document
      .querySelector("#run-name")
      .textContent.includes("Browser research"),
  );
  await page.locator('[data-view="split"]').click();
  if ((await page.locator("#comparison-details").getAttribute("open")) !== null)
    await page.locator("#comparison-details summary").click();
  assert.deepEqual(errors, []);
  return {
    checks:
      "joins, critical path, resource context, range, search, kinds, comparison, pagination, full export, invalid import, mobile run switch",
    consoleErrors: errors.length,
  };
}
module.exports = smoke;
if (require.main === module) {
  (async () => {
    const { chromium } = require("playwright");
    const browser = await chromium.launch({ headless: true });
    try {
      console.log(
        await smoke(
          await browser.newPage({ viewport: { width: 1440, height: 1000 } }),
          process.env.TRACEX_URL,
        ),
      );
    } finally {
      await browser.close();
    }
  })().catch((e) => {
    console.error(e);
    process.exitCode = 1;
  });
}
