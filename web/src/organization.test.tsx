import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { api, APIError } from "./api";
import { OrganizationPage } from "./Organization";
import type { OrganizationSettings } from "./types";

const settings: OrganizationSettings = {
  id: "organization-a",
  name: "Example",
  state: "active",
  version: 1,
  created_at: "2026-11-03T00:00:00Z",
  updated_at: "2026-11-03T00:00:00Z",
  can_manage: true,
};
const changed = vi.fn().mockResolvedValue(undefined);
beforeEach(() => {
  vi.restoreAllMocks();
  changed.mockClear();
  history.replaceState(null, "", "/#/organization");
});
afterEach(cleanup);
function load(value = settings) {
  return vi.spyOn(api, "organization").mockResolvedValue(value);
}
function save() {
  return screen.getByRole("button", {
    name: "Save organization",
  }) as HTMLButtonElement;
}

it("loads settings independently, retries errors and presents member metadata", async () => {
  const request = load();
  request.mockRejectedValueOnce(new TypeError("offline"));
  render(<OrganizationPage tenant="organization-a" onChanged={changed} />);
  await screen.findByRole("alert");
  expect(screen.queryByLabelText("Organization name")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry settings" }));
  await screen.findByDisplayValue("Example");
  expect(screen.getByText("organization-a")).toBeTruthy();
  expect(request).toHaveBeenCalledTimes(2);
});
it("saves a normalized name with the displayed version and refreshes workspace identity", async () => {
  load();
  const update = vi
    .spyOn(api, "updateOrganization")
    .mockResolvedValue({ ...settings, name: "New team", version: 2 });
  render(<OrganizationPage tenant="organization-a" onChanged={changed} />);
  const field = await screen.findByLabelText("Organization name");
  fireEvent.change(field, { target: { value: "  New team  " } });
  fireEvent.click(save());
  await screen.findByText("Organization settings saved.");
  expect(update).toHaveBeenCalledWith("organization-a", "New team", 1);
  expect(changed).toHaveBeenCalledTimes(1);
  expect((field as HTMLInputElement).value).toBe("New team");
});
it("keeps a read-only member from submitting registry changes", async () => {
  load({ ...settings, can_manage: false });
  const update = vi.spyOn(api, "updateOrganization");
  render(<OrganizationPage tenant="organization-a" onChanged={changed} />);
  const field = await screen.findByLabelText("Organization name");
  expect((field as HTMLInputElement).readOnly).toBe(true);
  expect(
    screen.queryByRole("button", { name: "Save organization" }),
  ).toBeNull();
  fireEvent.submit(field.closest("form")!);
  expect(update).not.toHaveBeenCalled();
});
it("keeps a conflicting draft and requires an explicit reload before another save", async () => {
  const request = load();
  const update = vi
    .spyOn(api, "updateOrganization")
    .mockRejectedValueOnce(new APIError(409, "conflict", "Changed"))
    .mockResolvedValue({ ...settings, name: "Final", version: 3 });
  render(<OrganizationPage tenant="organization-a" onChanged={changed} />);
  fireEvent.change(await screen.findByLabelText("Organization name"), {
    target: { value: "My draft" },
  });
  fireEvent.click(save());
  await screen.findByRole("button", { name: "Reload latest settings" });
  expect(
    (screen.getByLabelText("Organization name") as HTMLInputElement).value,
  ).toBe("My draft");
  expect(save().disabled).toBe(true);
  expect(update).toHaveBeenCalledTimes(1);
  request.mockResolvedValue({
    ...settings,
    name: "Other administrator",
    version: 2,
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Reload latest settings" }),
  );
  await screen.findByDisplayValue("Other administrator");
  fireEvent.change(screen.getByLabelText("Organization name"), {
    target: { value: "Final" },
  });
  fireEvent.click(save());
  await screen.findByText("Organization settings saved.");
  expect(update).toHaveBeenLastCalledWith("organization-a", "Final", 2);
});
it("validates Unicode byte limits and control characters before sending", async () => {
  load();
  const update = vi.spyOn(api, "updateOrganization");
  render(<OrganizationPage tenant="organization-a" onChanged={changed} />);
  const field = await screen.findByLabelText("Organization name");
  for (const value of [" ", "组".repeat(27), "Bad\u0085name"]) {
    fireEvent.change(field, { target: { value } });
    fireEvent.submit(field.closest("form")!);
    await screen.findByRole("alert");
  }
  expect(update).not.toHaveBeenCalled();
});
it("ignores a late save after switching organizations", async () => {
  load();
  let resolve!: (value: OrganizationSettings) => void;
  vi.spyOn(api, "updateOrganization").mockReturnValue(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const view = render(
    <OrganizationPage key="a" tenant="organization-a" onChanged={changed} />,
  );
  fireEvent.change(await screen.findByLabelText("Organization name"), {
    target: { value: "Old draft" },
  });
  fireEvent.click(save());
  vi.mocked(api.organization).mockResolvedValue({
    ...settings,
    id: "organization-b",
    name: "Second",
  });
  view.rerender(
    <OrganizationPage key="b" tenant="organization-b" onChanged={changed} />,
  );
  await screen.findByDisplayValue("Second");
  await act(async () =>
    resolve({ ...settings, name: "Old saved", version: 2 }),
  );
  expect(changed).not.toHaveBeenCalled();
  expect(screen.queryByDisplayValue("Old saved")).toBeNull();
});
it("cancels the form and leaves its draft unsaved", async () => {
  load();
  const update = vi.spyOn(api, "updateOrganization");
  render(<OrganizationPage tenant="organization-a" onChanged={changed} />);
  await screen.findByDisplayValue("Example");
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(location.hash).toBe("#/overview"));
  expect(update).not.toHaveBeenCalled();
});
