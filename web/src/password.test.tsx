import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AccountPage, InvitationLanding } from "./Account";
import { MemberPasswordRecovery, PasswordRecoveryLanding } from "./PasswordRecovery";
import { api, APIError } from "./api";

const expires = "2030-01-01T00:00:00Z";
const account = { id: "member", email: "member@example.test", system_admin: false, created_at: expires };
const session = { account, access_token: "access", expires_at: expires };
const unicodePassword = "🔐".repeat(128);

beforeEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
  history.replaceState(null, "", "/");
  vi.spyOn(api, "previewInvitation").mockResolvedValue({
    id: "invite", email: account.email, tenant_id: "tenant", tenant_name: "Team",
    account_exists: false, expires_at: expires,
  });
  vi.spyOn(api, "previewPasswordRecovery").mockResolvedValue({
    email: account.email, tenant_name: "Team", expires_at: expires,
  });
});
afterEach(cleanup);

it.each(["a".repeat(11), "a".repeat(129), "🔐".repeat(11), "🔐".repeat(129)])(
  "rejects an out-of-range new invitation password before making a request: %s",
  async (password) => {
    const accept = vi.spyOn(api, "acceptInvitation");
    const login = vi.spyOn(api, "login");
    render(<InvitationLanding token="invite" onLogin={vi.fn()} />);
    const field = await screen.findByLabelText("Choose a password");
    fireEvent.change(field, { target: { value: password } });
    fireEvent.submit(field.closest("form")!);
    await screen.findByText("Use 12–128 characters for your password.");
    expect(accept).not.toHaveBeenCalled();
    expect(login).not.toHaveBeenCalled();
  },
);

it.each(["a".repeat(11), "a".repeat(129), "🔐".repeat(11), "🔐".repeat(129)])(
  "rejects an out-of-range recovery password before consuming the link: %s",
  async (password) => {
    const complete = vi.spyOn(api, "completePasswordRecovery");
    render(<PasswordRecoveryLanding token="recovery" />);
    const field = await screen.findByLabelText("New password", { exact: true });
    fireEvent.change(field, { target: { value: password } });
    fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: password } });
    fireEvent.submit(field.closest("form")!);
    await screen.findByText("Use 12–128 characters for your password.");
    expect(complete).not.toHaveBeenCalled();
  },
);

it("accepts and signs in with all 128 supplementary Unicode characters", async () => {
  const accept = vi.spyOn(api, "acceptInvitation").mockResolvedValue(account);
  const login = vi.spyOn(api, "login").mockResolvedValue(session);
  const onLogin = vi.fn();
  render(<InvitationLanding token="invite" onLogin={onLogin} />);
  const field = await screen.findByLabelText("Choose a password");
  fireEvent.change(field, { target: { value: unicodePassword } });
  fireEvent.submit(field.closest("form")!);
  await waitFor(() => expect(onLogin).toHaveBeenCalledWith(account));
  expect(accept).toHaveBeenCalledWith("invite", unicodePassword);
  expect(login).toHaveBeenCalledWith(account.email, unicodePassword);
});

it("does not apply new-password policy to an existing invited account", async () => {
  vi.mocked(api.previewInvitation).mockResolvedValue({
    id: "invite", email: account.email, tenant_id: "tenant", tenant_name: "Team",
    account_exists: true, expires_at: expires,
  });
  const accept = vi.spyOn(api, "acceptInvitation").mockResolvedValue(account);
  vi.spyOn(api, "login").mockResolvedValue(session);
  const onLogin = vi.fn();
  render(<InvitationLanding token="invite" onLogin={onLogin} />);
  const field = await screen.findByLabelText("Account password");
  fireEvent.change(field, { target: { value: "legacy" } });
  fireEvent.submit(field.closest("form")!);
  await waitFor(() => expect(onLogin).toHaveBeenCalledWith(account));
  expect(accept).toHaveBeenCalledWith("invite", "legacy");
});

it("offers sign-in after a lost acceptance response and a consumed link", async () => {
  const accept = vi.spyOn(api, "acceptInvitation")
    .mockRejectedValueOnce(new TypeError("Network request failed"))
    .mockRejectedValueOnce(new APIError(404, "not_found", "Invitation unavailable."));
  const login = vi.spyOn(api, "login");
  history.replaceState(null, "", "/console?org=team#invite=invite");
  render(<InvitationLanding token="invite" onLogin={vi.fn()} />);
  const field = await screen.findByLabelText("Choose a password");
  fireEvent.change(field, { target: { value: "new-password-123" } });
  fireEvent.submit(field.closest("form")!);
  await screen.findByText(/If you already joined, go back to sign in/);
  fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));
  await screen.findByText("Invitation unavailable.");
  expect(screen.getByRole("link", { name: "Back to sign in" }).getAttribute("href"))
    .toBe("/console?org=team");
  expect(accept).toHaveBeenCalledTimes(2);
  expect(login).not.toHaveBeenCalled();
});

it("completes recovery with an unchanged 128-character Unicode password", async () => {
  const complete = vi.spyOn(api, "completePasswordRecovery").mockResolvedValue(undefined);
  render(<PasswordRecoveryLanding token="recovery" />);
  const field = await screen.findByLabelText("New password", { exact: true });
  fireEvent.change(field, { target: { value: unicodePassword } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: unicodePassword } });
  fireEvent.submit(field.closest("form")!);
  await screen.findByRole("heading", { name: "Your password is updated" });
  expect(complete).toHaveBeenCalledWith("recovery", unicodePassword);
});

it("uses the same Unicode limit for account password changes", async () => {
  history.replaceState(null, "", "/#/account/password");
  const change = vi.spyOn(api, "changePassword").mockResolvedValue(undefined);
  const onLogout = vi.fn();
  render(<AccountPage onLogout={onLogout} email={account.email} />);
  const previous = "🌿".repeat(128);
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: previous } });
  for (const label of ["New password", "Confirm new password"])
    fireEvent.change(screen.getByLabelText(label), { target: { value: unicodePassword } });
  fireEvent.submit(screen.getByLabelText("New password").closest("form")!);
  await waitFor(() => expect(onLogout).toHaveBeenCalledOnce());
  expect(change).toHaveBeenCalledWith(previous, unicodePassword);
});

it("submits the full administrator verification password when issuing a recovery link", async () => {
  const create = vi.spyOn(api, "createPasswordRecovery").mockResolvedValue({
    id: "recovery", token: "secret", email: account.email, expires_at: expires,
  });
  render(<MemberPasswordRecovery tenant="tenant" id="member" email={account.email} />);
  const field = screen.getByLabelText("Your administrator password");
  fireEvent.change(field, { target: { value: unicodePassword } });
  fireEvent.submit(field.closest("form")!);
  await screen.findByLabelText("Private recovery link");
  expect(create).toHaveBeenCalledWith("tenant", "member", unicodePassword);
});
