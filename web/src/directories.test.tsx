import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { InvitationsPage } from "./Invitations";
import { MembersPage } from "./Members";
import { RolesPage } from "./Roles";
import { api } from "./api";
import type { DirectoryPage, Member, Role } from "./types";

const member = (id: string): Member => ({
  account_id: id,
  email: `${id}@example.test`,
  status: "active",
  joined_at: "2026-01-01T00:00:00Z",
});
const role = (id: string): Role => ({
  tenant_id: "tenant",
  role_id: id,
  name: id,
  description: "",
  version: 4,
});
const changed = vi.fn().mockResolvedValue(undefined);
beforeEach(() => {
  vi.restoreAllMocks();
  history.replaceState(null, "", "/");
  changed.mockClear();
});
afterEach(cleanup);

it("searches the server and keeps status filters on next and previous pages", async () => {
  const load = vi
    .spyOn(api, "members")
    .mockImplementation(async (_tenant, filter) => ({
      items: [member(filter?.after ? "last" : "first")],
      total: 61,
      next_cursor: filter?.after ? undefined : "cursor",
    }));
  render(
    <MembersPage
      tenant="tenant"
      revision={1}
      capabilities={{ permissions: { "iam:member:read": true } }}
      onChanged={changed}
    />,
  );
  await screen.findByText("first@example.test");
  fireEvent.change(screen.getByLabelText("Find a member"), {
    target: { value: " Support " },
  });
  fireEvent.change(screen.getByLabelText("Find a member status"), {
    target: { value: "active" },
  });
  fireEvent.submit(screen.getByLabelText("Find a member").closest("form")!);
  await waitFor(() =>
    expect(load).toHaveBeenLastCalledWith("tenant", {
      q: "Support",
      status: "active",
      limit: 30,
    }),
  );
  await screen.findByText("first@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Next members" }));
  await screen.findByText("last@example.test");
  expect(load).toHaveBeenLastCalledWith("tenant", {
    q: "Support",
    status: "active",
    limit: 30,
    after: "cursor",
  });
  expect(screen.queryByText("first@example.test")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Previous members" }));
  await screen.findByText("first@example.test");
  expect(load).toHaveBeenLastCalledWith("tenant", {
    q: "Support",
    status: "active",
    limit: 30,
    after: undefined,
  });
});

it("ignores an old search response and retries a failed page without showing obsolete rows", async () => {
  let resolve!: (page: DirectoryPage<Member>) => void;
  const pending = new Promise<DirectoryPage<Member>>((done) => {
    resolve = done;
  });
  const load = vi
    .spyOn(api, "members")
    .mockReturnValueOnce(pending)
    .mockRejectedValueOnce(new TypeError("offline"))
    .mockResolvedValue({ items: [member("new")], total: 1 });
  render(
    <MembersPage
      tenant="tenant"
      revision={1}
      capabilities={{ permissions: { "iam:member:read": true } }}
      onChanged={changed}
    />,
  );
  fireEvent.change(screen.getByLabelText("Find a member"), {
    target: { value: "new" },
  });
  fireEvent.submit(screen.getByLabelText("Find a member").closest("form")!);
  await screen.findByRole("button", { name: "Retry members" });
  await act(async () => resolve({ items: [member("old")], total: 100 }));
  expect(screen.queryByText("old@example.test")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry members" }));
  await screen.findByText("new@example.test");
  expect(load).toHaveBeenLastCalledWith("tenant", {
    q: "new",
    status: "",
    limit: 30,
  });
});

it("returns to the first filtered page when a refreshed last page becomes empty", async () => {
  let removed = false;
  const load = vi
    .spyOn(api, "members")
    .mockImplementation(async (_tenant, filter) => ({
      items: filter?.after
        ? removed
          ? []
          : [member("last")]
        : [member("first")],
      total: removed ? 30 : 31,
      next_cursor: !filter?.after && !removed ? "cursor" : undefined,
    }));
  const props = {
    tenant: "tenant",
    capabilities: { permissions: { "iam:member:read": true } },
    onChanged: changed,
  };
  const view = render(<MembersPage {...props} revision={1} />);
  await screen.findByText("first@example.test");
  fireEvent.change(screen.getByLabelText("Find a member status"), {
    target: { value: "active" },
  });
  fireEvent.submit(screen.getByLabelText("Find a member").closest("form")!);
  await screen.findByText("first@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Next members" }));
  await screen.findByText("last@example.test");
  removed = true;
  view.rerender(<MembersPage {...props} revision={2} />);
  await screen.findByText("first@example.test");
  expect(load).toHaveBeenLastCalledWith("tenant", {
    q: "",
    status: "active",
    limit: 30,
    after: undefined,
  });
  expect(screen.queryByText("last@example.test")).toBeNull();
  expect(
    (
      screen.getByRole("button", {
        name: "Previous members",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

it("reaches older pending invitations and refreshes their status after revocation", async () => {
  let revoked = false;
  const invitation = {
    id: "old",
    email: "old@example.test",
    status: "pending",
    created_at: "2026-01-01",
    expires_at: "2030-01-01",
  };
  vi.spyOn(api, "invitations").mockImplementation(async (_tenant, filter) => ({
    items: filter?.after
      ? [invitation]
      : revoked
        ? []
        : [{ ...invitation, id: "new", email: "new@example.test" }],
    total: revoked ? 0 : 245,
    next_cursor: filter?.after || revoked ? undefined : "old-cursor",
  }));
  const revoke = vi
    .spyOn(api, "revokeInvitation")
    .mockImplementation(async () => {
      revoked = true;
    });
  render(
    <InvitationsPage
      tenant="tenant"
      revision={1}
      capabilities={{ permissions: { "iam:member:manage": true } }}
      onChanged={changed}
    />,
  );
  await screen.findByText("new@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Next invitations" }));
  await screen.findByText("old@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Revoke invitation" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Confirm revocation" }),
  );
  await screen.findByText("Invitation revoked.");
  await screen.findByText("No invitations match these filters.");
  expect(revoke).toHaveBeenCalledWith("tenant", "old");
});

it("preserves assignments across role pages and saves with the displayed role version", async () => {
  vi.spyOn(api, "member").mockResolvedValue(member("person"));
  vi.spyOn(api, "members").mockResolvedValue({
    items: [member("person")],
    total: 1,
  });
  vi.spyOn(api, "memberRoles")
    .mockResolvedValueOnce(["other-page"])
    .mockResolvedValue([]);
  vi.spyOn(api, "roles").mockImplementation(async (_tenant, filter) => ({
    items: [role(filter?.after ? "other-page" : "first-role")],
    total: 31,
    next_cursor: filter?.after ? undefined : "next",
  }));
  const bind = vi.spyOn(api, "bindRole").mockResolvedValue(undefined);
  render(
    <MembersPage
      tenant="tenant"
      revision={1}
      capabilities={{
        permissions: {
          "iam:member:read": true,
          "iam:role:read": true,
          "iam:role:assign": true,
        },
      }}
      onChanged={changed}
    />,
  );
  await screen.findByText("person@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Roles" }));
  await screen.findByText("1 assigned roles across all pages.");
  const modal = screen;
  await modal.findByText("1 assigned roles across all pages.");
  await modal.findByText("first-role");
  fireEvent.click(modal.getByRole("button", { name: "Next roles" }));
  const checkbox = await modal.findByRole("checkbox");
  expect((checkbox as HTMLInputElement).checked).toBe(true);
  fireEvent.click(checkbox);
  await modal.findByText("Role assignment saved.");
  expect(bind).toHaveBeenCalledWith(
    "tenant",
    "person",
    role("other-page"),
    false,
  );
  expect(changed).toHaveBeenCalledOnce();
  await waitFor(() =>
    expect(api.roles).toHaveBeenLastCalledWith("tenant", {
      limit: 30,
      after: "next",
    }),
  );
  expect(
    ((await modal.findByRole("checkbox")) as HTMLInputElement).checked,
  ).toBe(false);
});

it("does not request a role directory when role read access is absent", () => {
  const load = vi.spyOn(api, "roles");
  render(
    <RolesPage
      tenant="tenant"
      revision={1}
      permissions={[]}
      capabilities={{ permissions: {} }}
      onCreate={vi.fn()}
      onChanged={changed}
    />,
  );
  expect(load).not.toHaveBeenCalled();
  expect(
    screen.getByText("Your role does not include role read access."),
  ).toBeTruthy();
});
