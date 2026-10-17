const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const i18n = require("./i18n.js");

const zh = i18n.dictionaries["zh-CN"];
const read = (name) => fs.readFileSync(path.join(__dirname, name), "utf8");
const placeholders = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

test("translations keep every placeholder", () => {
  for (const [source, target] of Object.entries(zh))
    assert.deepEqual(placeholders(target), placeholders(source), source);
});

test("every t9n literal in the interface scripts is translated", () => {
  for (const name of ["app.js", "experiments.js"]) {
    const literals = [...read(name).matchAll(/t9n\(\s*(["'])((?:(?!\1)[^\\]|\\.)*)\1/g)].map((m) => m[2]);
    assert.ok(literals.length > 0, name);
    for (const text of literals) assert.ok(Object.hasOwn(zh, text), `${name}: ${text}`);
  }
});

test("every validation message from the experiment model is translated", () => {
  for (const [, text] of read("experiment-model.js").matchAll(/'([A-Z][^']+\.)'/g))
    assert.ok(Object.hasOwn(zh, text), text);
});

test("static page text and labels are translated", () => {
  const page = read("index.html").replace(/<(script|style|pre)\b[\s\S]*?<\/\1>/g, "");
  const decode = (text) => text.replaceAll("&amp;", "&");
  const texts = [...page.matchAll(/>([^<>]+)</g)].map((m) => decode(m[1]).replace(/\s+/g, " ").trim());
  const labels = [...page.matchAll(/\s(?:placeholder|aria-label|title)="([^"]+)"/g)].map((m) => m[1]);
  // Brand, symbols, units and numbers are not prose.
  const prose = (text) => /[A-Za-z]{2,}/.test(text) && !/^(TraceX|Trace|s|ARIES|tracex .*|Fig\. \w+|§.*)$/.test(text);
  for (const text of [...texts, ...labels].filter(prose)) assert.ok(Object.hasOwn(zh, text), text);
});

test("t fills values, falls back to English and matches backend patterns", () => {
  i18n.use("zh-CN");
  try {
    assert.equal(i18n.t("Import failed: {error}", { error: "x" }), "导入失败：x");
    assert.equal(i18n.t("A string with no translation"), "A string with no translation");
    assert.equal(i18n.t("Skipped 3 invalid or unsupported evidence records."), "已跳过 3 条无效或不受支持的证据记录。");
    assert.match(
      i18n.t("Task a-1 ran 71× the run median duration with sparse monitor coverage (0.5% of expected samples); its resource profile is incomplete."),
      /^任务 a-1 的耗时是本次运行中位数的 71×/,
    );
  } finally {
    i18n.use("en");
  }
  assert.equal(i18n.t("Import failed: {error}", { error: "x" }), "Import failed: x");
});
