import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { api, APIError } from "./api";
import { RoleMembers } from "./RoleMembers";
import { RolesPage } from "./Roles";
import type { DirectoryPage, Member, RoleDetail } from "./types";

const member = (id: string, status = "active"): Member => ({
  account_id: id,
  email: `${id}@example.test`,
  status,
  joined_at: "2026-01-01T00:00:00Z",
});
const role = {
  tenant_id: "tenant",
  role_id: "support",
  name: "Support",
  description: "",
  version: 4,
};
const props = {
  tenant: "tenant",
  role,
  revision: 1,
  canRead: true,
  canAssign: true,
  busy: false,
  onChange: vi.fn().mockResolvedValue(""),
};
beforeEach(() => {
  vi.restoreAllMocks();
  history.replaceState(null, "", "/#/roles");
  props.onChange.mockClear();
});
afterEach(cleanup);

it("preserves role scope, filters and cursor while paging assigned members", async () => {
  const load = vi
    .spyOn(api, "roleMembers")
    .mockImplementation(async (_tenant, _role, _assignment, filter) => ({
      items: [member(filter?.after ? "last" : "first")],
      total: 31,
      next_cursor: filter?.after ? undefined : "next",
    }));
  render(<RoleMembers {...props} />);
  await screen.findByText("first@example.test");
  fireEvent.change(screen.getByLabelText("Find a role member"), {
    target: { value: " team " },
  });
  fireEvent.change(screen.getByLabelText("Find a role member status"), {
    target: { value: "active" },
  });
  fireEvent.submit(
    screen.getByLabelText("Find a role member").closest("form")!,
  );
  await screen.findByText("first@example.test");
  fireEvent.click(screen.getByRole("button", { name: "Next role members" }));
  await screen.findByText("last@example.test");
  expect(load).toHaveBeenLastCalledWith("tenant", "support", "assigned", {
    q: "team",
    status: "active",
    limit: 30,
    after: "next",
  });
});

it("rejects an obsolete assigned response after switching to available members", async () => {
  let resolve!: (page: DirectoryPage<Member>) => void;
  const pending = new Promise<DirectoryPage<Member>>((done) => {
    resolve = done;
  });
  vi.spyOn(api, "roleMembers").mockImplementation(
    (_tenant, _role, assignment) =>
      assignment === "assigned"
        ? pending
        : Promise.resolve({
            items: [member("available"), member("suspended", "suspended")],
            total: 2,
          }),
  );
  render(<RoleMembers {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Available members" }));
  await screen.findByText("available@example.test");
  await act(async () => resolve({ items: [member("obsolete")], total: 1 }));
  expect(screen.queryByText("obsolete@example.test")).toBeNull();
  const suspended = within(
    screen.getByRole("row", { name: /suspended@example.test/ }),
  );
  expect(
    (
      suspended.getByRole("button", {
        name: "Assign to role",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(
    within(
      screen.getByRole("row", { name: /available@example.test/ }),
    ).getByRole("button", { name: "Assign to role" }),
  );
  await screen.findByText("Member assigned to this role.");
  expect(props.onChange).toHaveBeenCalledWith("available", true);
});

it("keeps member identities and requests unavailable without member read permission", () => {
  const load = vi.spyOn(api, "roleMembers");
  render(<RoleMembers {...props} canRead={false} />);
  expect(load).not.toHaveBeenCalled();
  expect(screen.queryByRole("table")).toBeNull();
  expect(screen.getByText(/does not include member read access/)).toBeTruthy();
});

it("lets read-only users inspect bindings without mutation controls", async () => {
  vi.spyOn(api, "roleMembers").mockResolvedValue({
    items: [member("viewer")],
    total: 1,
  });
  render(<RoleMembers {...props} canAssign={false} />);
  await screen.findByText("viewer@example.test");
  expect(screen.queryByRole("button", { name: "Remove from role" })).toBeNull();
  expect(screen.getByText("Read only")).toBeTruthy();
});

it("refreshes the displayed role version after a binding conflict without replaying it", async () => {
  const detail: RoleDetail = {
    role,
    grants: [],
    subjects: [],
    subject_count: 1,
    versions: [],
  };
  vi.spyOn(api, "roles").mockResolvedValue({ items: [role], total: 1 });
  const loadDetail = vi
    .spyOn(api, "role")
    .mockResolvedValueOnce(detail)
    .mockResolvedValue({ ...detail, role: { ...role, version: 5 } });
  vi.spyOn(api, "roleMembers").mockResolvedValue({
    items: [member("person")],
    total: 1,
  });
  const bind = vi
    .spyOn(api, "bindRole")
    .mockRejectedValueOnce(
      new APIError(
        409,
        "conflict",
        "The resource changed. Refresh and try again.",
      ),
    )
    .mockResolvedValue(undefined);
  const changed = vi.fn().mockResolvedValue(undefined);
  render(
    <RolesPage
      tenant="tenant"
      revision={1}
      permissions={[]}
      capabilities={{
        permissions: {
          "iam:role:read": true,
          "iam:member:read": true,
          "iam:role:assign": true,
          "iam:role:manage": true,
        },
      }}
      onCreate={vi.fn()}
      onChanged={changed}
    />,
  );
  await screen.findByText("Support");
  fireEvent.click(screen.getByRole("button", { name: "View role" }));
  await screen.findByRole("link", { name: "Retirement" });
  fireEvent.click(screen.getByRole("link", { name: "Retirement" }));
  const dialog = screen;
  expect(
    (
      (await dialog.findByRole("button", {
        name: "Retire role",
      })) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("link", { name: "Members" }));
  await screen.findByText("person@example.test");
  fireEvent.click(dialog.getByRole("button", { name: "Remove from role" }));
  await dialog.findByText("The resource changed. Refresh and try again.");
  await dialog.findByText(/Version 5 · 1 member bindings/);
  expect(bind).toHaveBeenCalledTimes(1);
  expect(changed).not.toHaveBeenCalled();
  await dialog.findByText("person@example.test");
  fireEvent.click(dialog.getByRole("button", { name: "Remove from role" }));
  await dialog.findByText("Member removed from this role.");
  expect(bind).toHaveBeenLastCalledWith(
    "tenant",
    "person",
    { ...role, version: 5 },
    false,
  );
  expect(loadDetail).toHaveBeenCalledTimes(5);
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
});
