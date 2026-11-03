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
      application: vi.fn(),
      me: vi.fn(),
      restoreSession: vi.fn(),
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
  vi.mocked(api.application).mockResolvedValue({ tenancy_mode: "multi" });
  sessionStorage.clear();
  localStorage.clear();
  history.replaceState(null, "", "/");
});
afterEach(cleanup);
it("opens a zero-permission tenant without requesting protected collections", async () => {
  setToken("session");
  vi.mocked(api.restoreSession).mockResolvedValue(account);
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
  history.replaceState(null, "", "/#/account/password");
  render(<AccountPage onLogout={onLogout} />);
  fireEvent.change(screen.getByLabelText("Current password"), {
    target: { value: "current-password" },
  });
  fireEvent.change(screen.getByLabelText("New password"), {
    target: { value: "replacement-password" },
  });
  fireEvent.change(screen.getByLabelText("Confirm new password"), {
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

const tenantA = {
  id: "tenant-a",
  name: "Tenant A",
  state: "active",
  created_at: account.created_at,
};
const tenantB = { ...tenantA, id: "tenant-b", name: "Tenant B" };
function mockWorkspace() {
  vi.mocked(api.restoreSession).mockResolvedValue(account);
  vi.mocked(api.tenants).mockResolvedValue([tenantA, tenantB]);
  vi.mocked(api.permissions).mockResolvedValue([]);
  vi.mocked(api.capabilities).mockResolvedValue({
    permissions: { "iam:member:read": true },
  });
  vi.mocked(api.members).mockImplementation(async (tenant) => ({
    total: 1,
    items: [
      {
        account_id: tenant,
        email: `${tenant}@example.test`,
        status: "active",
        joined_at: account.created_at,
      },
    ],
  }));
}

it("restores startup without a tab token and preserves it when retrying a network failure", async () => {
  mockWorkspace();
  setToken("preserved-token");
  vi.mocked(api.restoreSession).mockRejectedValueOnce(new TypeError("offline"));
  render(<App />);
  await screen.findByRole("heading", {
    name: "Unable to restore your session",
  });
  expect(api.logout).not.toHaveBeenCalled();
  expect(sessionStorage.getItem("amsonia.core.access.v1")).toBe(
    "preserved-token",
  );
  fireEvent.click(screen.getByRole("button", { name: "Retry connection" }));
  await screen.findByText("Boundaries are explicit.");
  expect(api.restoreSession).toHaveBeenCalledTimes(2);
});
it("recovers a session even when sessionStorage has no access credential", async () => {
  mockWorkspace();
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  expect(api.restoreSession).toHaveBeenCalledOnce();
  expect(api.me).not.toHaveBeenCalled();
});
it("remembers a tenant per account and validates the preference against current membership", async () => {
  mockWorkspace();
  localStorage.setItem("amsonia.core.tenant.v1:account", tenantB.id);
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  expect(api.capabilities).toHaveBeenCalledWith(tenantB.id);
  expect(
    (
      screen.getByRole("combobox", {
        name: "Current tenant",
      }) as HTMLSelectElement
    ).value,
  ).toBe(tenantB.id);
  fireEvent.change(screen.getByRole("combobox", { name: "Current tenant" }), {
    target: { value: tenantA.id },
  });
  await waitFor(() =>
    expect(api.members).toHaveBeenCalledWith(tenantA.id, {
      status: "active",
      limit: 1,
    }),
  );
  expect(localStorage.getItem("amsonia.core.tenant.v1:account")).toBe(
    tenantA.id,
  );
});
it("ignores an obsolete tenant response before fetching its protected collections", async () => {
  mockWorkspace();
  let resolve!: (value: { permissions: Record<string, boolean> }) => void;
  const pending = new Promise<{ permissions: Record<string, boolean> }>(
    (done) => {
      resolve = done;
    },
  );
  vi.mocked(api.capabilities).mockImplementation((tenant) =>
    tenant === tenantA.id
      ? pending
      : Promise.resolve({ permissions: { "iam:member:read": true } }),
  );
  render(<App />);
  await waitFor(() =>
    expect(api.capabilities).toHaveBeenCalledWith(tenantA.id),
  );
  fireEvent.change(screen.getByRole("combobox", { name: "Current tenant" }), {
    target: { value: tenantB.id },
  });
  await waitFor(() =>
    expect(api.members).toHaveBeenCalledWith(tenantB.id, {
      status: "active",
      limit: 1,
    }),
  );
  resolve({ permissions: { "iam:member:read": true } });
  await waitFor(() =>
    expect(screen.getByText("Boundaries are explicit.")).toBeTruthy(),
  );
  expect(api.members).not.toHaveBeenCalledWith(tenantA.id, expect.anything());
});
it("falls back to an accessible tenant when the selected membership disappears", async () => {
  mockWorkspace();
  vi.mocked(api.tenants)
    .mockResolvedValueOnce([tenantA, tenantB])
    .mockResolvedValue([tenantB]);
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  fireEvent.click(screen.getByRole("button", { name: "Members" }));
  await screen.findByText("tenant-a@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Refresh workspace" }));
  await screen.findByText("tenant-b@example.test");
  expect(screen.queryByText("tenant-a@example.test")).toBeNull();
  expect(localStorage.getItem("amsonia.core.tenant.v1:account")).toBe(
    tenantB.id,
  );
});
it("clears tenant data when the last membership disappears after focus returns", async () => {
  mockWorkspace();
  vi.mocked(api.tenants).mockResolvedValueOnce([tenantA]).mockResolvedValue([]);
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  fireEvent.click(screen.getByRole("button", { name: "Members" }));
  await screen.findByText("tenant-a@example.test");
  fireEvent.focus(window);
  await screen.findByRole("heading", { name: "No tenant assigned" });
  expect(screen.queryByText("tenant-a@example.test")).toBeNull();
  expect(localStorage.getItem("amsonia.core.tenant.v1:account")).toBeNull();
});
it("rechecks capabilities once after a collection permission changes during refresh", async () => {
  mockWorkspace();
  vi.mocked(api.capabilities)
    .mockResolvedValueOnce({ permissions: { "iam:member:read": true } })
    .mockResolvedValueOnce({ permissions: { "iam:member:read": true } })
    .mockResolvedValue({ permissions: {} });
  vi.mocked(api.members)
    .mockResolvedValueOnce({
      total: 1,
      items: [
        {
          account_id: "a",
          email: "old-member@example.test",
          status: "active",
          joined_at: account.created_at,
        },
      ],
    })
    .mockResolvedValueOnce({
      total: 1,
      items: [
        {
          account_id: "a",
          email: "old-member@example.test",
          status: "active",
          joined_at: account.created_at,
        },
      ],
    })
    .mockRejectedValue(new APIError(403, "forbidden", "Permission changed"));
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  fireEvent.click(screen.getByRole("button", { name: "Members" }));
  await screen.findByText("old-member@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Refresh workspace" }));
  await screen.findByText("Your role does not include member read access.");
  expect(screen.queryByText("old-member@example.test")).toBeNull();
  expect(api.capabilities).toHaveBeenCalledTimes(3);
  expect(api.members).toHaveBeenCalledTimes(3);
});
it("keeps the sign-out control available after a failed sign-out", async () => {
  mockWorkspace();
  vi.mocked(api.logout)
    .mockRejectedValueOnce(
      new APIError(503, "unavailable", "Try sign-out again."),
    )
    .mockResolvedValueOnce();
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
  await screen.findByRole("alert");
  expect(
    screen.getByRole("button", { name: "Sign out" }).hasAttribute("disabled"),
  ).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
  await screen.findByRole("heading", { name: "Welcome back" });
});

it("opens another invitation when its fragment changes in an already open console", async () => {
  mockWorkspace();
  vi.mocked(api.previewInvitation).mockResolvedValue({
    id: "invite",
    tenant_id: tenantB.id,
    tenant_name: "Another invitation",
    email: account.email,
    expires_at: "2030-01-01T00:00:00Z",
    account_exists: true,
  });
  render(<App />);
  await screen.findByText("Boundaries are explicit.");
  history.replaceState(null, "", "/#invite=next-invitation");
  fireEvent(window, new HashChangeEvent("hashchange"));
  await screen.findByRole("heading", { name: "Join Another invitation" });
  expect(api.previewInvitation).toHaveBeenCalledWith("next-invitation");
});

it("uses one organization without a tenant picker or creation route", async () => {
  mockWorkspace();
  vi.mocked(api.restoreSession).mockResolvedValue({
    ...account,
    system_admin: true,
  });
  vi.mocked(api.application).mockResolvedValue({ tenancy_mode: "single" });
  vi.mocked(api.tenants).mockResolvedValue([tenantA]);
  history.replaceState(null, "", "/#/tenants/new");
  render(<App />);
  await screen.findByText(
    "This deployment uses one organization. Tenant creation is unavailable.",
  );
  await screen.findByText("Tenant A");
  expect(screen.queryByRole("button", { name: "Create tenant" })).toBeNull();
  expect(screen.queryByRole("combobox", { name: "Current tenant" })).toBeNull();
  expect(screen.queryByRole("textbox", { name: "Tenant name" })).toBeNull();
  expect(screen.getByText("Organization console")).toBeTruthy();
});
it("guides a single-organization account without membership to its administrator", async () => {
  mockWorkspace();
  vi.mocked(api.profile).mockResolvedValue({
    id: account.id,
    email: account.email,
    display_name: "Member",
    created_at: account.created_at,
  });
  vi.mocked(api.application).mockResolvedValue({ tenancy_mode: "single" });
  vi.mocked(api.tenants).mockResolvedValue([]);
  render(<App />);
  await screen.findByRole("heading", { name: "No organization access" });
  expect(screen.queryByRole("button", { name: "Create tenant" })).toBeNull();
  expect(api.capabilities).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Account settings" }));
  await screen.findByRole("heading", { name: "Account profile" });
});
