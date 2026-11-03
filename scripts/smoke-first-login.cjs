// First login through the same-origin console in the released archive.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");

async function main() {
  const [modulePath, url, screenshots] = process.argv.slice(2);
  const { chromium } = require(modulePath);
  await fs.mkdir(screenshots, { recursive: true });
  const browser = await chromium.launch();
  try {
    for (const [name, viewport] of [
      ["desktop", { width: 1440, height: 900 }],
      ["mobile", { width: 390, height: 844 }],
    ]) {
      const context = await browser.newContext({ viewport });
      try {
        const page = await context.newPage();
        page.setDefaultTimeout(15000);
        const failures = [];
        page.on("pageerror", (error) => failures.push(error.message));
        page.on("console", (message) => {
          if (message.type() === "error") failures.push(message.text());
        });
        page.on("response", (response) => {
          if (response.status() >= 400 && response.url().startsWith(url + "/")) {
            failures.push(`${response.status()} ${new URL(response.url()).pathname}`);
          }
        });

        await page.goto(url);
        await page.getByLabel("Email", { exact: true }).fill(process.env.SMOKE_LOGIN_EMAIL);
        await page.getByLabel("Password", { exact: true }).fill(process.env.SMOKE_LOGIN_PASSWORD);
        await page.getByRole("button", { name: "Enter the console" }).click();
        await page.getByRole("heading", { name: "Boundaries are explicit." }).waitFor();
        await page.getByText("My organization", { exact: false }).first().waitFor();
        assert.equal(await page.getByRole("button", { name: "Create tenant", exact: true }).count(), 0);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        assert.deepEqual(failures, []);
        await page.screenshot({ path: path.join(screenshots, `first-login-${name}.png`) });
        console.log(`First login passed: ${name} ${viewport.width}x${viewport.height}`);
      } finally {
        await context.close();
      }
    }
  } finally {
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
