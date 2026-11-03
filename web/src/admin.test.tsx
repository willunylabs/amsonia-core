import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { App } from "./App";
import { AccountPage, InvitationLanding } from "./Account";
import { api, APIError, setToken } from "./api";
import type { Account } from "./types";
vi.mock("./api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./api")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      me: vi.fn(),
      tenants: vi.fn(),
      permissions: vi.fn(),
      capabilities: vi.fn(),
      members: vi.fn(),
      roles: vi.fn(),
      audit: vi.fn(),
      profile: vi.fn(),
      sessions: vi.fn(),
      updateProfile: vi.fn(),
      changePassword: vi.fn(),
      revokeSession: vi.fn(),
      previewInvitation: vi.fn(),
      acceptInvitation: vi.fn(),
      login: vi.fn(),
      logout: vi.fn(),
    },
  };
});
const account: Account = {
  id: "account",
  email: "member@example.test",
  system_admin: false,
  created_at: "2026-01-01T00:00:00Z",
};
beforeEach(() => {
  vi.resetAllMocks();
  sessionStorage.clear();
  history.replaceState(null, "", "/");
});
afterEach(cleanup);
it("opens a zero-permission tenant without requesting protected collections", async () => {
  setToken("session");
  vi.mocked(api.me).mockResolvedValue(account);
  vi.mocked(api.tenants).mockResolvedValue([
    {
      id: "tenant",
      name: "Invited tenant",
      state: "active",
      created_at: account.created_at,
    },
  ]);
  vi.mocked(api.permissions).mockResolvedValue([]);
  vi.mocked(api.capabilities).mockResolvedValue({ permissions: {} });
  render(<App />);
  await screen.findByText("Invited tenant");
  await waitFor(() => expect(api.capabilities).toHaveBeenCalledWith("tenant"));
  await screen.findByText("Boundaries are explicit.");
  expect(api.members).not.toHaveBeenCalled();
  expect(api.roles).not.toHaveBeenCalled();
  expect(api.audit).not.toHaveBeenCalled();
  expect(screen.queryByText("Read-only demo.")).toBeNull();
});
it("accepts existing-account invitations with the supplied current password then signs in", async () => {
  vi.mocked(api.previewInvitation).mockResolvedValue({
    id: "invite",
    tenant_id: "tenant",
    tenant_name: "Team",
    email: account.email,
    expires_at: "2030-01-01T00:00:00Z",
    account_exists: true,
  });
  vi.mocked(api.acceptInvitation).mockResolvedValue(account);
  vi.mocked(api.login).mockResolvedValue({
    account,
    access_token: "new",
    expires_at: "2030-01-01T00:00:00Z",
  });
  const onLogin = vi.fn();
  history.replaceState(null, "", "/#invite=secret");
  render(<InvitationLanding token="secret" onLogin={onLogin} />);
  await screen.findByText("Join Team");
  fireEvent.change(screen.getByLabelText("Account password"), {
    target: { value: "existing-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));
  await waitFor(() => expect(onLogin).toHaveBeenCalledWith(account));
  expect(api.acceptInvitation).toHaveBeenCalledWith(
    "secret",
    "existing-password",
  );
  expect(api.login).toHaveBeenCalledWith(account.email, "existing-password");
  expect(location.hash).toBe("");
});
it("does not consume an invitation twice if the subsequent login needs retrying", async () => {
  vi.mocked(api.previewInvitation).mockResolvedValue({
    id: "invite",
    tenant_id: "tenant",
    tenant_name: "Team",
    email: account.email,
    expires_at: "2030-01-01T00:00:00Z",
    account_exists: false,
  });
  vi.mocked(api.acceptInvitation).mockResolvedValue(account);
  vi.mocked(api.login)
    .mockRejectedValueOnce(
      new APIError(429, "login_busy", "Try again shortly."),
    )
    .mockResolvedValueOnce({
      account,
      access_token: "new",
      expires_at: "2030-01-01T00:00:00Z",
    });
  render(<InvitationLanding token="secret" onLogin={vi.fn()} />);
  await screen.findByText("Join Team");
  fireEvent.change(screen.getByLabelText("Choose a password"), {
    target: { value: "new-password-123" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await waitFor(() => expect(api.login).toHaveBeenCalledTimes(2));
  expect(api.acceptInvitation).toHaveBeenCalledTimes(1);
});
it("changes the account password and drops the local access credential after success", async () => {
  vi.mocked(api.profile).mockResolvedValue({
    id: account.id,
    email: account.email,
    display_name: "Member",
    created_at: account.created_at,
  });
  vi.mocked(api.sessions).mockResolvedValue([]);
  vi.mocked(api.changePassword).mockResolvedValue();
  setToken("old-session");
  const onLogout = vi.fn();
  render(<AccountPage onLogout={onLogout} />);
  await screen.findByDisplayValue("Member");
  fireEvent.change(screen.getByLabelText("Current password"), {
    target: { value: "current-password" },
  });
  fireEvent.change(screen.getByLabelText("New password"), {
    target: { value: "replacement-password" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Change password and sign out" }),
  );
  await waitFor(() => expect(onLogout).toHaveBeenCalled());
  expect(api.changePassword).toHaveBeenCalledWith(
    "current-password",
    "replacement-password",
  );
  expect(sessionStorage.length).toBe(0);
});
