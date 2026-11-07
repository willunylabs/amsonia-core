// First login through the same-origin console in the released archive.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");

async function holdRequest(page, endpoint, trigger) {
  let receive;
  const intercepted = new Promise(resolve => { receive = resolve; });
  await page.route(endpoint, receive, { times: 1 });
  const deadline = AbortSignal.timeout(15000);
  try {
    const [route] = await Promise.all([
      Promise.race([
        intercepted,
        new Promise((_, reject) => deadline.addEventListener("abort", () => reject(deadline.reason), { once: true })),
      ]),
      trigger(),
    ]);
    return route;
  } catch (error) {
    await page.unroute(endpoint, receive);
    throw error;
  }
}

async function checkPasswordCookieOrdering(memberPage, context, request, url, email, oldPassword) {
  const endpoint = "**/api/v1/auth/password";
  const nextPassword = "replacement-after-password-change";
  const loginPage = await context.newPage();
  loginPage.setDefaultTimeout(15000);
  // Show sign-in in a second tab while preserving the first tab's cookie.
  await loginPage.route("**/api/v1/auth/restore", route => route.fulfill({ status: 204 }), { times: 1 });
  await loginPage.goto(url);
  await loginPage.getByLabel("Email", { exact: true }).fill(email);
  await loginPage.getByLabel("Password", { exact: true }).fill(nextPassword);

  // An independent request context keeps Set-Cookie out of the browser until
  // the held response is actually delivered to the password-change tab.
  const transport = await request.newContext({ timeout: 15000 });
  const oldAccess = await memberPage.evaluate(() => sessionStorage.getItem("amsonia.core.access.v1"));
  let held;
  let delivered = false;
  try {
    await memberPage.getByRole("button", { name: "Account settings", exact: true }).click();
    await memberPage.getByRole("link", { name: "Password", exact: true }).click();
    await memberPage.getByLabel("Current password", { exact: true }).fill(oldPassword);
    await memberPage.getByLabel("New password", { exact: true }).fill(nextPassword);
    await memberPage.getByLabel("Confirm new password", { exact: true }).fill(nextPassword);
    held = await holdRequest(memberPage, endpoint, () =>
      memberPage.getByRole("button", { name: "Change password and sign out", exact: true }).click());
    const response = await transport.fetch(held.request());
    assert.equal(response.status(), 204);
    assert.match(response.headers()["set-cookie"], /Max-Age=0/);
    let loginDispatched = false;
    loginPage.on("request", req => {
      if (new URL(req.url()).pathname === "/api/v1/auth/login") loginDispatched = true;
    });
    await loginPage.getByRole("button", { name: "Enter the console", exact: true }).click();
    await loginPage.waitForFunction(async () =>
      (await navigator.locks.query()).pending.some(lock => lock.name === "amsonia.core.auth:"),
      null, { timeout: 10000 });
    assert.equal(loginDispatched, false);
    await held.fulfill({ status: 204, headers: response.headers() });
    delivered = true;
    await memberPage.getByRole("button", { name: "Enter the console", exact: true }).waitFor();
    await loginPage.getByRole("heading", { name: "Organization overview", exact: true }).waitFor();
    const freshAccess = await loginPage.evaluate(() => sessionStorage.getItem("amsonia.core.access.v1"));
    assert.ok(freshAccess && freshAccess !== oldAccess);
    for (const [access, status] of [[oldAccess, 401], [freshAccess, 200]]) {
      const me = await transport.get(url + "/api/v1/auth/me", { headers: { Authorization: `Bearer ${access}` } });
      assert.equal(me.status(), status);
    }
    assert.ok((await context.cookies()).some(cookie => cookie.name === "amsonia_refresh" && cookie.value));
    const restored = await context.newPage();
    await restored.goto(url);
    await restored.getByRole("heading", { name: "Organization overview", exact: true }).waitFor();
  } finally {
    if (held && !delivered) await held.abort();
    await transport.dispose();
  }
}

async function main() {
  const [modulePath, url, screenshots] = process.argv.slice(2);
  const { chromium, request } = require(modulePath);
  // Published archives keep their original UI contract. Candidates always
  // exercise the current Console, including Unicode and pending dialog checks.
  const contract = process.env.SMOKE_CONSOLE_CONTRACT ?? "current";
  assert.ok(["current", "published-preview-2-3", "published-preview-4", "published-preview-5", "published-preview-6"].includes(contract), "Unknown Console contract");
  const historical = contract === "published-preview-2-3";
  const edgeCases = contract === "current";
  const deepReview = edgeCases || contract === "published-preview-6";
  const current = deepReview || contract === "published-preview-5";
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
        if (deepReview) {
          await page.getByRole("button", {name: "Account settings", exact: true}).click();
          const displayName = "🌿".repeat(80);
          await page.getByLabel("Display name", {exact: true}).fill(displayName);
          await page.getByRole("button", {name: "Save profile", exact: true}).click();
          await page.getByText("Profile saved.", {exact: true}).waitFor();
          assert.equal(await page.getByLabel("Display name", {exact: true}).inputValue(), displayName);
          await page.getByLabel("Display name", {exact: true}).fill("🌿".repeat(81));
          await page.getByRole("button", {name: "Save profile", exact: true}).click();
          await page.getByRole("alert").filter({hasText: "Use at most 80 characters"}).waitFor();
          await page.getByRole("button", {name: "Members", exact: true}).click();
          await page.getByLabel("Find a member", {exact: true}).fill("🌿".repeat(128));
          const searched = page.waitForResponse(response => new URL(response.url()).pathname.endsWith("/members") && new URL(response.url()).searchParams.has("q"));
          await page.getByRole("button", {name: "Search", exact: true}).click();
          assert.equal((await searched).status(), 200);
          assert.equal(await page.getByLabel("Find a member", {exact: true}).inputValue(), "🌿".repeat(128));
          await page.getByLabel("Find a member", {exact: true}).fill("🌿".repeat(129));
          await page.getByRole("button", {name: "Search", exact: true}).click();
          await page.getByRole("alert").filter({hasText: "Use at most 128 characters"}).waitFor();
          await page.getByRole("button", {name: "Policy lab", exact: true}).click();
          await page.getByRole("combobox", {name: "Permission", exact: true}).locator("option").nth(1).waitFor({state: "attached"});
          const options = await page.getByRole("combobox", {name: "Permission", exact: true}).locator("option").evaluateAll(items => items.map(item => item.value));
          assert.ok(options.length >= 2);
          let firstRoute;
          let firstHandled = false;
          let firstSeen;
          const seen = new Promise(resolve => { firstSeen = resolve; });
          let checks = 0;
          const route = async intercepted => {
            checks++;
            if (!firstRoute) { firstRoute = intercepted; firstSeen(); return; }
            await intercepted.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({allowed: false, reason: "not_granted"})});
          };
          await page.route("**/api/v1/authorization/check", route);
          try {
            await page.getByRole("button", {name: "Evaluate policy", exact: true}).click();
            await seen;
            await page.getByRole("combobox", {name: "Permission", exact: true}).selectOption(options[1]);
            await page.getByRole("button", {name: "Evaluate policy", exact: true}).click();
            await page.getByRole("heading", {name: "Denied", exact: true}).waitFor();
            const late = page.waitForResponse(response => new URL(response.url()).pathname.endsWith("/authorization/check"));
            firstHandled = true;
            await firstRoute.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({allowed: true, reason: "granted", effective_scope: "tenant"})});
            await late;
            await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
            await page.getByRole("heading", {name: "Denied", exact: true}).waitFor();
            assert.equal(await page.locator(".decision-card code").first().textContent(), options[1]);
            assert.equal(checks, 2);
            await page.screenshot({path: path.join(screenshots, `policy-lab-${name}.png`)});
          } finally {
            if (firstRoute && !firstHandled) await firstRoute.abort();
            await page.unrouteAll({behavior: "wait"});
          }
        }
        if (!historical) {
          await page.getByRole("button", {name: "Invitations", exact: true}).click();
          await page.getByRole("link", {name: "Invite member", exact: true}).click();
          const memberEmail = `recovery-${name}@example.test`;
          const oldPassword = current ? "🌿".repeat(128) : "preview-member-original-password";
          const newPassword = current ? "🔑".repeat(128) : "preview-member-recovered-password";
          await page.getByLabel("Email", {exact: true}).fill(memberEmail);
          const inviteButton = page.getByRole("button", {name: "Create invitation", exact: true});
          if (edgeCases) {
            const endpoint = "**/api/v1/tenants/*/invitations";
            const held = await holdRequest(page, endpoint, () => inviteButton.click());
            try {
              assert.equal(held.request().method(), "POST");
              assert.equal(held.request().postDataJSON().email, memberEmail);
              assert.equal(await page.getByLabel("Email", { exact: true }).isDisabled(), true);
            } finally {
              await held.continue();
            }
            await page.getByText(`Invitation created for ${memberEmail}.`, { exact: true }).waitFor();
          } else {
            await inviteButton.click();
          }
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
            if (current) {
              await memberPage.getByLabel("Choose a password", {exact: true}).fill("🌿".repeat(11));
              await memberPage.getByRole("button", {name: "Accept invitation", exact: true}).click();
              await memberPage.getByRole("alert").filter({hasText: "Use 12–128 characters"}).waitFor();
            }
            await memberPage.getByLabel("Choose a password", {exact: true}).fill(oldPassword);
            assert.equal(await memberPage.getByLabel("Choose a password", {exact: true}).inputValue(), oldPassword);
            if (edgeCases && name === "desktop") {
              const endpoint = "**/api/v1/invitations/accept";
              const failureCount = failures.length;
              let acceptedStatus;
              await memberPage.route(endpoint, async route => {
                acceptedStatus = (await route.fetch()).status();
                await route.abort("failed");
              }, { times: 1 });
              await memberPage.getByRole("button", { name: "Accept invitation", exact: true }).click();
              await memberPage.getByText(/If you already joined, go back to sign in/).waitFor();
              assert.equal(acceptedStatus, 200);
              const induced = failures.splice(failureCount);
              assert.ok(induced.every(error => error.includes("net::ERR_FAILED")), JSON.stringify(induced));
              await memberPage.getByRole("link", { name: "Back to sign in", exact: true }).click();
              await memberPage.getByLabel("Email", { exact: true }).fill(memberEmail);
              await memberPage.getByLabel("Password", { exact: true }).fill(oldPassword);
              await memberPage.getByRole("button", { name: "Enter the console", exact: true }).click();
            } else {
              await memberPage.getByRole("button", {name: "Accept invitation", exact: true}).click();
            }
            await memberPage.getByRole("heading", {name: "Organization overview", exact: true}).waitFor();
            const oldAccess = await memberPage.evaluate(() => sessionStorage.getItem("amsonia.core.access.v1"));
            await page.getByRole("button", {name: "Members", exact: true}).click();
            await page.getByLabel("Find a member", {exact: true}).fill(memberEmail);
            await page.getByRole("button", {name: "Search", exact: true}).click();
            const row = page.getByRole("row").filter({hasText: memberEmail});
            await row.getByRole("button", {name: "Manage member", exact: true}).click();
            if (edgeCases) {
              const failureCount = failures.length;
              await page.route("**/api/v1/tenants/*/members/*/roles", route => route.fulfill({
                status: 503, json: { error: { code: "unavailable", message: "Assignments temporarily unavailable." } },
              }), { times: 1 });
              await page.getByRole("link", { name: "Roles", exact: true }).click();
              await page.getByText("Assignments temporarily unavailable.", { exact: true }).waitFor();
              assert.equal(await page.getByText("Loading assignments…", { exact: true }).count(), 0);
              const induced = failures.splice(failureCount);
              assert.ok(induced.some(error => error.startsWith("503 ") && error.endsWith("/roles")));
              assert.ok(induced.every(error => error.includes("503")), JSON.stringify(induced));
              await page.getByRole("button", { name: "Retry assignments", exact: true }).click();
              await page.getByText("0 assigned roles across all pages.", { exact: true }).waitFor();
              const roles = page.getByRole("checkbox");
              await roles.first().waitFor();
              assert.equal(await roles.first().isEnabled(), true);
            }
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
            if (current) {
              for (const invalid of ["🔑".repeat(11), "🔑".repeat(129)]) {
                await memberPage.getByLabel("New password", {exact: true}).fill(invalid);
                await memberPage.getByLabel("Confirm new password", {exact: true}).fill(invalid);
                await memberPage.getByRole("button", {name: "Set new password", exact: true}).click();
                await memberPage.getByRole("alert").filter({hasText: "Use 12–128 characters"}).waitFor();
              }
            }
            if (deepReview) {
              await memberPage.getByRole("link", {name: "Back to sign in", exact: true}).waitFor();
              // Simulate a successful one-use mutation whose response is lost.
              const lostResponse = async route => { await route.fetch(); await route.abort("failed"); };
              await memberPage.route("**/api/v1/password-recovery/complete", lostResponse);
              await memberPage.getByLabel("New password", {exact: true}).fill(newPassword);
              await memberPage.getByLabel("Confirm new password", {exact: true}).fill(newPassword);
              const expectedNetworkFailure = error => error.includes("net::ERR_FAILED");
              const failureCount = failures.length;
              await memberPage.getByRole("button", {name: "Set new password", exact: true}).click();
              await memberPage.getByText(/If your password was updated but the response was lost/).waitFor();
              const induced = failures.splice(failureCount);
              assert.ok(induced.every(expectedNetworkFailure), JSON.stringify(induced));
              await memberPage.unroute("**/api/v1/password-recovery/complete", lostResponse);
              await memberPage.screenshot({path: path.join(screenshots, `recovery-response-lost-${name}.png`)});
              await memberPage.getByRole("link", {name: "Back to sign in", exact: true}).click();
              await memberPage.getByLabel("Email", {exact: true}).fill(memberEmail);
              await memberPage.getByLabel("Password", {exact: true}).fill(newPassword);
              await memberPage.getByRole("button", {name: "Enter the console", exact: true}).click();
              await memberPage.getByRole("heading", {name: "Organization overview", exact: true}).waitFor();
              // Issue a replacement link to retain the ordinary success flow.
              await page.getByRole("button", {name: "Create a replacement link", exact: true}).click();
              await page.getByLabel("Your administrator password", {exact: true}).fill(process.env.SMOKE_LOGIN_PASSWORD);
              await page.getByRole("button", {name: "Create recovery link", exact: true}).click();
              const replacementLink = await page.getByLabel("Private recovery link", {exact: true}).inputValue();
              // As with the first link, use a fresh tab without the access
              // credential that this second recovery is about to revoke.
              await memberPage.close();
              memberPage = await memberContext.newPage();
              observeMemberPage(memberPage);
              await memberPage.goto(replacementLink);
              await memberPage.getByRole("heading", {name: "Choose a new password", exact: true}).waitFor();
            }
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
            if (edgeCases) await checkPasswordCookieOrdering(memberPage, memberContext, request, url, memberEmail, newPassword);
          } finally { await memberContext.close(); }
          await page.getByRole("button", {name: "Account settings", exact: true}).click();
          await page.getByRole("link", {name: "Devices", exact: true}).click();
          await page.getByText("This device", {exact: true}).waitFor();
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
          assert.deepEqual(failures, []);
          await page.screenshot({path: path.join(screenshots, `account-devices-${name}.png`)});
          if (current) {
            // Use a real second login so the confirmation affects an actual
            // session. Pause only the request dispatch while testing busy focus.
            const other = await browser.newContext();
            let release;
            const gate = new Promise(resolve => { release = resolve; });
            const endpoint = "**/api/v1/auth/sessions/revoke-others";
            try {
              const login = await other.request.post(url + "/api/v1/auth/login", {
                data: {email: process.env.SMOKE_LOGIN_EMAIL, password: process.env.SMOKE_LOGIN_PASSWORD},
              });
              assert.equal(login.status(), 200);
              const otherToken = (await login.json()).access_token;
              await page.getByRole("button", {name: "Refresh devices", exact: true}).click();
              await page.getByRole("button", {name: "Sign out other devices", exact: true}).click();
              const dialog = page.getByRole("dialog", {name: "Sign out other devices?"});
              await dialog.getByRole("button", {name: "Cancel", exact: true}).click();
              await page.getByRole("button", {name: "Sign out other devices", exact: true}).click();
              await page.route(endpoint, async route => { await gate; await route.continue(); });
              await Promise.all([
                page.waitForRequest(url + "/api/v1/auth/sessions/revoke-others"),
                dialog.getByRole("button", {name: "Confirm sign out", exact: true}).click(),
              ]);
              for (const key of ["Tab", "Shift+Tab"]) {
                await page.keyboard.press(key);
                assert.equal(await dialog.evaluate(element => element.contains(document.activeElement)), true);
              }
              await page.keyboard.press("Escape");
              assert.equal(await dialog.isVisible(), true);
              await page.screenshot({path: path.join(screenshots, `pending-confirmation-${name}.png`)});
              release();
              await dialog.waitFor({state: "hidden"});
              await page.getByText("Other devices signed out. This device remains signed in.", {exact: true}).waitFor();
              const revoked = await other.request.get(url + "/api/v1/auth/me", {headers: {Authorization: `Bearer ${otherToken}`}});
              assert.equal(revoked.status(), 401);
              assert.deepEqual(failures, []);
            } finally {
              release();
              await page.unroute(endpoint);
              await other.close();
            }
          }
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
