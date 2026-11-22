// End-to-end test of the main user journey: explain a schedule in the browser,
// then add a monitored job, ping it like a cron job would, and delete it.
// Builds the real binary and runs "cronplain serve" on 127.0.0.1:4234.
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { chromium } from "playwright";

const port = process.env.CRONPLAIN_E2E_PORT || "4234";
const base = `http://127.0.0.1:${port}`;
let dir, server, browser, page;

before(async () => {
  dir = mkdtempSync(join(tmpdir(), "cronplain-e2e-"));
  const bin = join(dir, "cronplain");
  execFileSync("go", ["build", "-o", bin, "."], { cwd: join(import.meta.dirname, ".."), stdio: "inherit" });
  server = spawn(bin, ["serve", "-addr", `127.0.0.1:${port}`, "-db", join(dir, "e2e.db")], { stdio: ["ignore", "pipe", "inherit"] });
  await new Promise((resolve, reject) => {
    server.on("exit", (code) => reject(new Error(`cronplain serve exited with ${code}`)));
    server.stdout.on("data", (b) => b.toString().includes("cronplain dashboard:") && resolve());
  });
  browser = await chromium.launch();
  const ctx = await browser.newContext({ timezoneId: "Europe/Istanbul", locale: "en-GB", permissions: ["clipboard-read", "clipboard-write"] });
  page = await ctx.newPage();
  page.setDefaultTimeout(5000);
});

after(async () => {
  await browser?.close();
  server?.kill();
  if (dir) rmSync(dir, { recursive: true, force: true });
});

const text = (sel) => page.locator(sel).textContent();

test("explain: description, runs, wrapped range and an invalid field", async () => {
  await page.goto(base + "/");
  await page.waitForFunction(() => document.querySelector("#sentence").textContent.includes("02:30"));
  assert.equal(await text("#sentence"), "At 02:30 on Monday through Friday.");
  assert.equal(await text("#runs-title"), "Next 20 runs");
  assert.equal(await page.locator("#runs tr").count(), 20);
  assert.match(await text("#meta"), /evaluated in Europe\/Istanbul/);

  await page.fill("#expr", "cron(0 9 ? * FRI-MON *)");
  await page.waitForFunction(() => document.querySelector("#sentence").textContent.includes("09:00"));
  assert.equal(await text("#sentence"), "At 09:00 on Friday through Monday (UTC time).");
  assert.match(await text("#lints"), /EventBridge rules run in UTC/);

  await page.fill("#expr", "61 * * * *");
  await page.waitForFunction(() => document.querySelector("#expr").getAttribute("aria-invalid") === "true");
  assert.match(await text("#sentence"), /minute: 61 is out of range 0-59/);
  assert.equal(await page.locator("#results").isHidden(), true);

  await page.fill("#expr", "0 0 30 2 *");
  await page.waitForFunction(() => document.querySelector("#lints").textContent.includes("never fires"));
  assert.equal(await text("#runs-title"), "Next runs");
  assert.match(await text("#runs"), /No upcoming runs/);
});

test("explain: pasting a workflow snippet keeps its cron: line", async () => {
  await page.evaluate(() => navigator.clipboard.writeText("on:\n  schedule:\n    - cron: '17 6 * * 1'  # Mondays\n  workflow_dispatch:\n"));
  await page.fill("#expr", "");
  await page.focus("#expr");
  await page.keyboard.press("ControlOrMeta+V");
  await page.waitForFunction(() => document.querySelector("#sentence").textContent.includes("06:17"));
  assert.equal(await page.inputValue("#expr"), "- cron: '17 6 * * 1'  # Mondays");
  assert.equal(await text("#sentence"), "At 06:17 on Monday (UTC time).");
});

test("monitor: add a job, ping it, see it on time, delete it", async () => {
  await page.click('nav a[href="#monitor"]');
  await page.waitForSelector("#jobs .empty");
  assert.match(await text("#jobs"), /No jobs yet/);

  await page.fill('#add [name="id"]', "nightly-backup");
  await page.fill('#add [name="schedule"]', "0 3 * * *");
  await page.click('#add button[type="submit"]');
  await page.waitForSelector("#jobs table");
  assert.match(await text("#msg"), /^Added nightly-backup\. Ping http:\/\/127\.0\.0\.1:\d+\/ping\/nightly-backup/);
  assert.equal(await text("#jobs tbody .state"), "Waiting");

  // What the cron job does on success (curl -fsS .../ping/ID).
  const ping = await fetch(`${base}/ping/nightly-backup`);
  assert.equal(ping.status, 200);
  assert.equal((await fetch(`${base}/ping/no-such-job`)).status, 404);

  await page.reload();
  await page.waitForSelector("#jobs table");
  assert.equal(await text("#jobs tbody .state"), "On time");

  page.once("dialog", (d) => d.accept());
  await page.click('button[data-del="nightly-backup"]');
  await page.waitForSelector("#jobs .empty");
  assert.equal(await text("#msg"), "Deleted nightly-backup.");
});

test("monitor: a bad schedule is refused with the parser's message", async () => {
  await page.fill('#add [name="id"]', "broken");
  await page.fill('#add [name="schedule"]', "0 25 * * *");
  await page.click('#add button[type="submit"]');
  await page.waitForSelector("#msg .error");
  assert.equal(await text("#msg"), "hour: 25 is out of range 0-23");
  assert.match(await text("#jobs"), /No jobs yet/);
});
