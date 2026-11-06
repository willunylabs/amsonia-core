// First login through the same-origin console in the released archive.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");

async function main() {
  const [modulePath, url, screenshots] = process.argv.slice(2);
  const { chromium } = require(modulePath);
  // Published preview 2/3 archives retain their historical UI. The candidate
  // bundle always exercises the current Console and its recovery workflow.
  const contract = process.env.SMOKE_CONSOLE_CONTRACT ?? "current";
  assert.ok(["current", "published-preview-2-3"].includes(contract), "Unknown Console contract");
  const historical = contract === "published-preview-2-3";
  await fs.mkdir(screenshots, { recursive: true });
  const browser = await chromium.launch();
  try {
    for (const [name, viewport] of [
      ["desktop", { width: 1440, height: 900 }],
      ["mobile", { width: 390, height: 844 }],
    ]) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const failures = [];
      try {
        page.setDefaultTimeout(15000);
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
        await page.getByRole("heading", { name: historical ? "Boundaries are explicit." : "Organization overview" }).waitFor();
        const organization = process.env.SMOKE_ORGANIZATION ?? "My organization";
        await page.getByText(organization, { exact: false }).first().waitFor();
        assert.equal(await page.getByRole("button", { name: "Create tenant", exact: true }).count(), 0);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        assert.deepEqual(failures, []);
        await page.screenshot({ path: path.join(screenshots, `first-login-${name}.png`) });
        if (!historical) {
          await page.getByRole("button", {name: "Invitations", exact: true}).click();
          await page.getByRole("link", {name: "Invite member", exact: true}).click();
          const memberEmail = `recovery-${name}@example.test`;
          const oldPassword = "preview-member-original-password";
          const newPassword = "preview-member-recovered-password";
          await page.getByLabel("Email", {exact: true}).fill(memberEmail);
          await page.getByRole("button", {name: "Create invitation", exact: true}).click();
          const invitation = await page.getByLabel("Invitation link", {exact: true}).inputValue();
          const memberContext = await browser.newContext({viewport});
          try {
            let memberPage = await memberContext.newPage();
            function observeMemberPage(target) {
              target.on("pageerror", (error) => failures.push(error.message));
              target.on("console", (message) => { if (message.type() === "error") failures.push(message.text()); });
              target.on("response", (response) => { if (response.status() >= 400 && response.url().startsWith(url + "/")) failures.push(`${response.status()} ${new URL(response.url()).pathname}`); });
            }
            observeMemberPage(memberPage);
            await memberPage.goto(invitation);
            await memberPage.getByLabel("Choose a password", {exact: true}).fill(oldPassword);
            await memberPage.getByRole("button", {name: "Accept invitation", exact: true}).click();
            await memberPage.getByRole("heading", {name: "Organization overview", exact: true}).waitFor();
            const oldAccess = await memberPage.evaluate(() => sessionStorage.getItem("amsonia.core.access.v1"));
            await page.getByRole("button", {name: "Members", exact: true}).click();
            await page.getByLabel("Find a member", {exact: true}).fill(memberEmail);
            await page.getByRole("button", {name: "Search", exact: true}).click();
            const row = page.getByRole("row").filter({hasText: memberEmail});
            await row.getByRole("button", {name: "Manage member", exact: true}).click();
            await page.getByRole("link", {name: "Password recovery", exact: true}).click();
            await page.getByLabel("Your administrator password", {exact: true}).fill(process.env.SMOKE_LOGIN_PASSWORD);
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
            await page.screenshot({path: path.join(screenshots, `member-recovery-${name}.png`)});
            await page.getByRole("button", {name: "Create recovery link", exact: true}).click();
            const recovery = await page.getByLabel("Private recovery link", {exact: true}).inputValue();
            // Recovery is opened in a fresh tab. The original tab's access token
            // remains available below only for the explicit revocation probe.
            await memberPage.close();
            memberPage = await memberContext.newPage();
            observeMemberPage(memberPage);
            await memberPage.goto(recovery);
            await memberPage.getByRole("heading", {name: "Choose a new password", exact: true}).waitFor();
            await memberPage.getByLabel("New password", {exact: true}).fill(newPassword);
            await memberPage.getByLabel("Confirm new password", {exact: true}).fill(newPassword);
            assert.equal(await memberPage.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
            await memberPage.screenshot({path: path.join(screenshots, `reset-password-${name}.png`)});
            await memberPage.getByRole("button", {name: "Set new password", exact: true}).click();
            await memberPage.getByRole("heading", {name: "Your password is updated", exact: true}).waitFor();
            assert.equal(new URL(memberPage.url()).hash, "");
            const revoked = await memberContext.request.get(url + "/api/v1/auth/me", {headers: {Authorization: `Bearer ${oldAccess}`}});
            assert.equal(revoked.status(), 401);
            await memberPage.getByRole("link", {name: "Back to sign in", exact: true}).click();
            await memberPage.getByLabel("Email", {exact: true}).fill(memberEmail);
            await memberPage.getByLabel("Password", {exact: true}).fill(newPassword);
            await memberPage.getByRole("button", {name: "Enter the console"}).click();
            await memberPage.getByRole("heading", {name: "Organization overview", exact: true}).waitFor();
          } finally { await memberContext.close(); }
          await page.getByRole("button", {name: "Account settings", exact: true}).click();
          await page.getByRole("link", {name: "Devices", exact: true}).click();
          await page.getByText("This device", {exact: true}).waitFor();
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
          assert.deepEqual(failures, []);
          await page.screenshot({path: path.join(screenshots, `account-devices-${name}.png`)});
        }
        console.log(`First login passed: ${name} ${viewport.width}x${viewport.height}`);
      } catch (error) {
        console.error(`Browser failures (${name}):`, failures);
        console.error(await page.locator("body").innerText({ timeout: 1000 }).catch(() => ""));
        await page.screenshot({ path: path.join(screenshots, `failed-login-${name}.png`) }).catch(() => {});
        throw error;
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
