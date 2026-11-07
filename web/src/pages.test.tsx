import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { AccountPage } from "./Account";
import { MembersPage } from "./Members";
import { InvitationsPage } from "./Invitations";
import { RolesPage } from "./Roles";
import { api, APIError, setToken, getToken } from "./api";
import type { RoleDetail } from "./types";
const role = {
  tenant_id: "tenant",
  role_id: "support/一",
  name: "Support",
  description: "Team support",
  version: 4,
};
const detail: RoleDetail = {
  role,
  grants: [
    { permission: "iam:audit:read", scope: "tenant", workspace_roles: [] },
  ],
  subjects: [],
  subject_count: 2,
  versions: [],
};
const capabilities = {
  permissions: {
    "iam:member:read": true,
    "iam:member:manage": true,
    "iam:role:read": true,
    "iam:role:manage": true,
    "iam:role:assign": true,
    "iam:grant:manage": true,
  },
};
const changed = vi.fn().mockResolvedValue(undefined);
function go(path: string) {
  history.replaceState(null, "", `/#/${path}`);
}
beforeEach(() => {
  vi.restoreAllMocks();
  changed.mockClear();
  go("overview");
  setToken("token");
});
afterEach(cleanup);
it("opens an encoded role deep link and renders only its requested task", async () => {
  go("roles/support%2F%E4%B8%80/details");
  const load = vi.spyOn(api, "role").mockResolvedValue(detail);
  const memberLoad = vi
    .spyOn(api, "roleMembers")
    .mockResolvedValue({ items: [], total: 0 });
  render(
    <RolesPage
      tenant="tenant"
      revision={1}
      permissions={[]}
      capabilities={capabilities}
      onChanged={changed}
      onCreate={vi.fn()}
    />,
  );
  await screen.findByDisplayValue("Team support");
  expect(load).toHaveBeenCalledWith("tenant", "support/一");
  expect(memberLoad).not.toHaveBeenCalled();
  expect(screen.queryByText("Permission grants")).toBeNull();
  expect(screen.queryByText("Version history")).toBeNull();
  fireEvent.click(screen.getByRole("link", { name: "Permissions" }));
  await screen.findByText("Permission grants");
  expect(screen.queryByLabelText("Name")).toBeNull();
  expect(screen.queryByLabelText("Scope")).toBeNull();
  expect(screen.queryByText("Role members")).toBeNull();
});
it("opens a dedicated grant form and saves the displayed role version", async () => {
  go("roles/support%2F%E4%B8%80/permissions/new");
  vi.spyOn(api, "role").mockResolvedValue(detail);
  const grant = vi.spyOn(api, "changeGrant").mockResolvedValue(undefined);
  render(
    <RolesPage
      tenant="tenant"
      revision={1}
      permissions={[{ key: "iam:audit:read", description: "Audit" }]}
      capabilities={capabilities}
      onChanged={changed}
      onCreate={vi.fn()}
    />,
  );
  await screen.findByRole("button", { name: "Add permission grant" });
  expect(screen.queryByRole("button", { name: "Revoke grant" })).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Save role details" }),
  ).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Add permission grant" }));
  await waitFor(() =>
    expect(location.hash).toBe("#/roles/support%2F%E4%B8%80/permissions"),
  );
  expect(grant).toHaveBeenCalledWith(
    "tenant",
    role,
    { permission: "iam:audit:read", scope: "tenant", workspace_roles: [] },
    true,
  );
});
it("shows a membership status form on an exact deep link without requesting invitations", async () => {
  go("members/person/details");
  vi.spyOn(api, "member").mockResolvedValue({
    account_id: "person",
    email: "p@example.test",
    status: "active",
    joined_at: "2026-01-01T00:00:00Z",
  });
  const invites = vi.spyOn(api, "invitations");
  const save = vi.spyOn(api, "manageMember").mockResolvedValue(undefined);
  render(
    <MembersPage
      tenant="tenant"
      revision={1}
      capabilities={capabilities}
      onChanged={changed}
    />,
  );
  await screen.findByDisplayValue("p@example.test");
  fireEvent.change(screen.getByLabelText("Membership status"), {
    target: { value: "suspended" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save membership" }));
  await screen.findByText("Membership saved.");
  expect(save).toHaveBeenCalledWith("tenant", "person", "suspended");
  expect(invites).not.toHaveBeenCalled();
  expect(
    screen.queryByText("Remove membership", { selector: "h2" }),
  ).toBeNull();
});
it("does not create an invitation when cancelling the full-page form", async () => {
  go("invitations/new");
  const create = vi.spyOn(api, "createInvitation");
  vi.spyOn(api, "invitations").mockResolvedValue({ items: [], total: 0 });
  render(
    <InvitationsPage
      tenant="tenant"
      revision={1}
      capabilities={capabilities}
      onChanged={changed}
    />,
  );
  fireEvent.change(screen.getByLabelText("Email"), {
    target: { value: "new@example.test" },
  });
  expect(screen.queryByRole("dialog")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await screen.findByRole("heading", { name: "Invitations" });
  expect(create).not.toHaveBeenCalled();
});
it("freezes the invite email during submission and confirms the issued address", async () => {
  go("invitations/new");
  let finish!: (value: Awaited<ReturnType<typeof api.createInvitation>>) => void;
  const create = vi.spyOn(api, "createInvitation")
    .mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  render(<InvitationsPage tenant="tenant" revision={1} capabilities={capabilities} onChanged={changed} />);
  const email = screen.getByLabelText<HTMLInputElement>("Email");
  fireEvent.change(email, { target: { value: "ORIGINAL@example.test" } });
  fireEvent.submit(email.closest("form")!);
  expect(email.disabled).toBe(true);
  fireEvent.submit(email.closest("form")!);
  expect(create).toHaveBeenCalledExactlyOnceWith("tenant", "ORIGINAL@example.test");
  await act(async () => finish({
    id: "invite", token: "private", email: "original@example.test",
    expires_at: "2030-01-01T00:00:00Z", created_at: "2026-01-01T00:00:00Z",
  }));
  expect(screen.getByRole("status").textContent).toBe("Invitation created for original@example.test.");
  expect(screen.getByLabelText<HTMLInputElement>("Invitation link").value).toContain("#invite=private");
});

it("recovers member assignments after an independent read fails", async () => {
  go("members/person/roles");
  vi.spyOn(api, "member").mockResolvedValue({
    account_id: "person", email: "person@example.test", status: "active",
    joined_at: "2026-01-01T00:00:00Z",
  });
  vi.spyOn(api, "roles").mockResolvedValue({ items: [role], total: 1 });
  const assignments = vi.spyOn(api, "memberRoles")
    .mockRejectedValueOnce(new APIError(503, "unavailable", "Assignments temporarily unavailable."))
    .mockResolvedValueOnce([role.role_id]);
  render(<MembersPage tenant="tenant" revision={1} capabilities={capabilities} onChanged={changed} />);
  const retry = await screen.findByRole("button", { name: "Retry assignments" });
  const checkbox = await screen.findByRole<HTMLInputElement>("checkbox", { name: /Support/ });
  expect(checkbox.disabled).toBe(true);
  expect(screen.queryByText("Loading assignments…")).toBeNull();
  fireEvent.click(retry);
  await waitFor(() => expect(checkbox.checked && !checkbox.disabled).toBe(true));
  expect(assignments).toHaveBeenCalledTimes(2);
});

it("validates role edits by Unicode characters before submitting", async () => {
  go("roles/support%2F%E4%B8%80/details");
  vi.spyOn(api, "role").mockResolvedValue(detail);
  const update = vi.spyOn(api, "updateRole").mockResolvedValue(role);
  render(<RolesPage tenant="tenant" revision={1} permissions={[]} capabilities={capabilities}
    onChanged={changed} onCreate={vi.fn()} />);
  await screen.findByDisplayValue("Support");
  const name = screen.getByLabelText("Name");
  const description = screen.getByLabelText("Description");
  const form = name.closest("form")!;
  fireEvent.change(name, { target: { value: "🌿".repeat(65) } });
  fireEvent.submit(form);
  await screen.findByText(/Enter a role name with 1–64 characters/);
  fireEvent.change(name, { target: { value: ` ${"🌿".repeat(64)} ` } });
  fireEvent.change(description, { target: { value: "🔑".repeat(501) } });
  fireEvent.submit(form);
  await screen.findByText(/Enter a description with up to 500 characters/);
  expect(update).not.toHaveBeenCalled();
  fireEvent.change(description, { target: { value: "🔑".repeat(500) } });
  fireEvent.submit(form);
  await waitFor(() => expect(update).toHaveBeenCalledExactlyOnceWith(
    "tenant", role, "🌿".repeat(64), "🔑".repeat(500),
  ));
});
it("blocks mismatched password confirmation without fetching profile or devices", async () => {
  go("account/password");
  const password = vi.spyOn(api, "changePassword");
  const profile = vi.spyOn(api, "profile"),
    sessions = vi.spyOn(api, "sessions");
  render(<AccountPage onLogout={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("Current password"), {
    target: { value: "current-password" },
  });
  fireEvent.change(screen.getByLabelText("New password"), {
    target: { value: "next-password-123" },
  });
  fireEvent.change(screen.getByLabelText("Confirm new password"), {
    target: { value: "different-password" },
  });
  fireEvent.submit(screen.getByLabelText("New password").closest("form")!);
  await screen.findByText("The new passwords do not match.");
  expect(password).not.toHaveBeenCalled();
  expect(profile).not.toHaveBeenCalled();
  expect(sessions).not.toHaveBeenCalled();
});
it("does not clear a newer account after an old password form unmounts", async () => {
  go("account/password");
  let resolve!: () => void;
  const pending = new Promise<void>((r) => {
    resolve = r;
  });
  vi.spyOn(api, "changePassword").mockReturnValue(pending);
  const logout = vi.fn();
  const view = render(<AccountPage onLogout={logout} />);
  for (const name of [
    "Current password",
    "New password",
    "Confirm new password",
  ])
    fireEvent.change(screen.getByLabelText(name), {
      target: { value: "valid-password-123" },
    });
  fireEvent.submit(screen.getByLabelText("New password").closest("form")!);
  view.unmount();
  setToken("new-account-token");
  await act(async () => resolve());
  expect(logout).not.toHaveBeenCalled();
  expect(getToken()).toBe("new-account-token");
});
it("denies member management pages before requesting protected details", () => {
  go("members/person/details");
  const load = vi.spyOn(api, "member");
  render(
    <MembersPage
      tenant="tenant"
      revision={1}
      capabilities={{ permissions: {} }}
      onChanged={changed}
    />,
  );
  expect(load).not.toHaveBeenCalled();
  expect(
    screen.getByText("Your role does not include member read access."),
  ).toBeTruthy();
});

it("requires the exact email on the separate member removal form", async () => {
  go("members/person/remove");
  vi.spyOn(api, "member").mockResolvedValue({
    account_id: "person",
    email: "p@example.test",
    status: "active",
    joined_at: "2026-01-01T00:00:00Z",
  });
  vi.spyOn(api, "members").mockResolvedValue({ items: [], total: 0 });
  const remove = vi.spyOn(api, "manageMember").mockResolvedValue(undefined);
  render(
    <MembersPage
      tenant="tenant"
      revision={1}
      capabilities={capabilities}
      onChanged={changed}
    />,
  );
  await screen.findByLabelText("Confirm member email");
  fireEvent.change(screen.getByLabelText("Confirm member email"), {
    target: { value: "someone@example.test" },
  });
  fireEvent.submit(
    screen.getByLabelText("Confirm member email").closest("form")!,
  );
  await screen.findByText("Enter the member's email to confirm removal.");
  expect(remove).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Confirm member email"), {
    target: { value: "p@example.test" },
  });
  fireEvent.submit(
    screen.getByLabelText("Confirm member email").closest("form")!,
  );
  await waitFor(() => expect(location.hash).toBe("#/members"));
  expect(remove).toHaveBeenCalledWith("tenant", "person", "remove");
});
it("denies a direct grant creation address without grant management access", async () => {
  go("roles/support/permissions/new");
  vi.spyOn(api, "role").mockResolvedValue(detail);
  const grant = vi.spyOn(api, "changeGrant");
  render(
    <RolesPage
      tenant="tenant"
      revision={1}
      permissions={[]}
      capabilities={{ permissions: { "iam:role:read": true } }}
      onChanged={changed}
      onCreate={vi.fn()}
    />,
  );
  await screen.findByText(
    "Your role does not include grant management access.",
  );
  expect(
    screen.queryByRole("button", { name: "Add permission grant" }),
  ).toBeNull();
  expect(grant).not.toHaveBeenCalled();
});
