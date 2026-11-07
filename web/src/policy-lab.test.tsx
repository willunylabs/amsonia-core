import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PolicyLab } from "./PolicyLab";
import { api, APIError } from "./api";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const props = {
  tenant: { id: "tenant", name: "Team", state: "active", created_at: "2026-01-01T00:00:00Z" },
  account: { id: "account", email: "member@example.test", system_admin: false, created_at: "2026-01-01T00:00:00Z" },
  permissions: ["iam:role:read", "iam:member:manage"].map(key => ({ key, description: key })),
};
type Decision = Awaited<ReturnType<typeof api.check>>;
const allowed: Decision = { allowed: true, reason: "granted", effective_scope: "tenant" };
const denied: Decision = { allowed: false, reason: "not_granted" };
function deferred() {
  let resolve!: (value: Decision) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Decision>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

it.each(["success", "error"])("ignores an obsolete %s after evaluating a new permission", async outcome => {
  const first = deferred(), second = deferred();
  const check = vi.spyOn(api, "check").mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  render(<PolicyLab {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Evaluate policy" }));
  expect(screen.getByRole("button", { name: "Evaluating policy…" })).toHaveProperty("disabled", true);
  fireEvent.change(screen.getByLabelText("Permission"), { target: { value: "iam:member:manage" } });
  fireEvent.click(screen.getByRole("button", { name: "Evaluate policy" }));
  await act(async () => second.resolve(denied));
  await screen.findByRole("heading", { name: "Denied" });
  await act(async () => outcome === "success" ? first.resolve(allowed) : first.reject(new APIError(500,"fixture","Old failure")));
  expect(screen.getByRole("heading", { name: "Denied" })).toBeTruthy();
  expect(screen.queryByRole("heading", { name: "Allowed" })).toBeNull();
  expect(screen.queryByText("Old failure")).toBeNull();
  expect(screen.getByText("iam:member:manage", { selector: "code" })).toBeTruthy();
  expect(check).toHaveBeenLastCalledWith(expect.objectContaining({ permission: "iam:member:manage" }));
});

it("clears a completed result on selection change and rejects pending work after unmount", async () => {
  const pending = deferred();
  vi.spyOn(api,"check").mockResolvedValueOnce(allowed).mockReturnValueOnce(pending.promise);
  const { unmount } = render(<PolicyLab {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Evaluate policy" }));
  await screen.findByRole("heading", { name: "Allowed" });
  fireEvent.change(screen.getByLabelText("Permission"), { target: { value: "iam:member:manage" } });
  expect(screen.queryByRole("heading", { name: "Allowed" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Evaluate policy" }));
  unmount();
  await act(async () => pending.resolve(allowed));
  expect(screen.queryByRole("heading", { name: "Allowed" })).toBeNull();
});

it("does not display a prior account's decision in a new tenant or account", async () => {
  const first = deferred();
  vi.spyOn(api,"check").mockReturnValueOnce(first.promise);
  const { rerender } = render(<PolicyLab {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Evaluate policy" }));
  rerender(<PolicyLab {...props} tenant={{...props.tenant,id:"other"}} account={{...props.account,id:"another"}} />);
  await act(async () => first.resolve(allowed));
  expect(screen.queryByRole("heading", { name: "Allowed" })).toBeNull();
  expect(screen.getByRole("button", { name: "Evaluate policy" })).toHaveProperty("disabled", false);
});

it("accepts a catalog loaded after mount and clears decisions when the selected permission disappears",async () => {
  vi.spyOn(api,"check").mockResolvedValue(allowed);
  const { rerender }=render(<PolicyLab {...props} permissions={[]} />);
  expect(screen.getByRole("button",{name:"Evaluate policy"})).toHaveProperty("disabled",true);
  rerender(<PolicyLab {...props} />);
  fireEvent.click(screen.getByRole("button",{name:"Evaluate policy"}));
  await screen.findByRole("heading",{name:"Allowed"});
  rerender(<PolicyLab {...props} permissions={[props.permissions[1]]} />);
  expect(screen.queryByRole("heading",{name:"Allowed"})).toBeNull();
  expect(screen.getByLabelText("Permission")).toHaveProperty("value",props.permissions[1].key);
});
