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
import { AccountPage } from "./Account";
import { DeviceSessions } from "./DeviceSessions";
import { api, APIError, getToken, setToken } from "./api";
import type { AccountSession } from "./types";
const current: AccountSession = {
  id: "current",
  created_at: "2026-11-01T00:00:00Z",
  last_seen_at: "2026-11-03T00:00:00Z",
  expires_at: "2030-01-01T00:00:00Z",
  remote_address: "127.0.0.1",
  user_agent: "Current browser",
  current: true,
};
const other = {
  ...current,
  id: "other",
  user_agent: "Other browser",
  current: false,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
beforeEach(() => {
  vi.restoreAllMocks();
  history.replaceState(null, "", "/#/account/profile");
  setToken("signed-in-token");
  vi.spyOn(api, "sessions").mockResolvedValue([current, other]);
  vi.spyOn(api, "profile").mockResolvedValue({
    id: "a",
    email: "a@example.test",
    display_name: "Alice",
    created_at: current.created_at,
  });
});
afterEach(cleanup);
it("keeps profile and device pages separate and retries the failed device page", async () => {
  vi.mocked(api.sessions)
    .mockRejectedValueOnce(
      new APIError(503, "unavailable", "Devices unavailable"),
    )
    .mockResolvedValueOnce([current]);
  render(<AccountPage onLogout={vi.fn()} />);
  await screen.findByDisplayValue("Alice");
  expect(api.sessions).not.toHaveBeenCalled();
  expect(screen.queryByText("Signed-in devices")).toBeNull();
  fireEvent.click(screen.getByRole("link", { name: "Devices" }));
  await screen.findByText("Devices unavailable");
  expect(screen.queryByRole("button", { name: "Save profile" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry devices" }));
  await screen.findByText("Current browser");
  expect(api.profile).toHaveBeenCalledTimes(1);
});
it("offers a profile retry without loading device data", async () => {
  vi.mocked(api.profile)
    .mockRejectedValueOnce(
      new APIError(503, "unavailable", "Profile unavailable"),
    )
    .mockResolvedValueOnce({
      id: "a",
      email: "a@example.test",
      display_name: "Alice",
      created_at: current.created_at,
    });
  render(<AccountPage onLogout={vi.fn()} />);
  await screen.findByText("Profile unavailable");
  expect(api.sessions).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Retry profile" }));
  await screen.findByDisplayValue("Alice");
});
it("requires confirmation, retains this sign-in and reloads after signing out other devices", async () => {
  const pending = deferred<void>();
  const revoke = vi
    .spyOn(api, "revokeOtherSessions")
    .mockReturnValue(pending.promise);
  vi.mocked(api.sessions)
    .mockResolvedValueOnce([current, other])
    .mockResolvedValueOnce([current]);
  const onLogout = vi.fn();
  history.replaceState(null, "", "/#/account/devices");
  render(<AccountPage onLogout={onLogout} />);
  await screen.findByText("Other browser");
  fireEvent.click(
    screen.getByRole("button", { name: "Sign out other devices" }),
  );
  expect(revoke).not.toHaveBeenCalled();
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }),
  );
  expect(revoke).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "Sign out other devices" }),
  );
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Confirm sign out",
    }),
  );
  expect(revoke).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("button", { name: "Save profile" })).toBeNull();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.getByRole("dialog")).toBeTruthy();
  await act(async () => pending.resolve());
  await screen.findByText(
    "Other devices signed out. This device remains signed in.",
  );
  await screen.findByText("1 signed-in device · 0 other devices");
  expect(screen.queryByText("Other browser")).toBeNull();
  expect(onLogout).not.toHaveBeenCalled();
  expect(getToken()).toBe("signed-in-token");
  expect(api.profile).not.toHaveBeenCalled();
});
it("retains a failed confirmation for a deliberate retry", async () => {
  const revoke = vi
    .spyOn(api, "revokeOtherSessions")
    .mockRejectedValueOnce(
      new APIError(503, "unavailable", "Network unavailable"),
    )
    .mockResolvedValueOnce();
  render(
    <DeviceSessions onLogout={vi.fn()} disabled={false} onBusy={vi.fn()} />,
  );
  await screen.findByText("Other browser");
  fireEvent.click(
    screen.getByRole("button", { name: "Sign out other devices" }),
  );
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Confirm sign out",
    }),
  );
  await within(screen.getByRole("dialog")).findByText("Network unavailable");
  expect(revoke).toHaveBeenCalledTimes(1);
  expect(api.sessions).toHaveBeenCalledTimes(1);
  vi.mocked(api.sessions).mockResolvedValue([current]);
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Confirm sign out",
    }),
  );
  await screen.findByText("1 signed-in device · 0 other devices");
  expect(revoke).toHaveBeenCalledTimes(2);
});
it("clears known-revoked current credentials even when cookie cleanup is offline", async () => {
  vi.spyOn(api, "revokeSession").mockResolvedValue();
  vi.spyOn(api, "logout").mockRejectedValue(new Error("offline"));
  const onLogout = vi.fn();
  render(
    <DeviceSessions onLogout={onLogout} disabled={false} onBusy={vi.fn()} />,
  );
  await screen.findByText("Current browser");
  fireEvent.click(screen.getByRole("button", { name: "Sign out this device" }));
  expect(api.revokeSession).not.toHaveBeenCalled();
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Confirm sign out",
    }),
  );
  await waitFor(() => expect(onLogout).toHaveBeenCalledTimes(1));
  expect(api.revokeSession).toHaveBeenCalledWith("current");
  expect(getToken()).toBe("");
});
it("does not invoke logout when an old current-device action finishes after unmount", async () => {
  const pending = deferred<void>();
  vi.spyOn(api, "revokeSession").mockReturnValue(pending.promise);
  const logout = vi.spyOn(api, "logout");
  const onLogout = vi.fn(),
    onBusy = vi.fn();
  const view = render(
    <DeviceSessions onLogout={onLogout} disabled={false} onBusy={onBusy} />,
  );
  await screen.findByText("Current browser");
  fireEvent.click(screen.getByRole("button", { name: "Sign out this device" }));
  fireEvent.click(
    within(screen.getByRole("dialog")).getByRole("button", {
      name: "Confirm sign out",
    }),
  );
  view.unmount();
  await act(async () => pending.resolve());
  expect(logout).not.toHaveBeenCalled();
  expect(onLogout).not.toHaveBeenCalled();
  expect(onBusy).toHaveBeenLastCalledWith(false);
});
it("hides stale devices on refresh failure and restores them on retry", async () => {
  vi.mocked(api.sessions)
    .mockResolvedValueOnce([current, other])
    .mockRejectedValueOnce(new APIError(503, "unavailable", "Refresh failed"))
    .mockResolvedValueOnce([current]);
  render(
    <DeviceSessions onLogout={vi.fn()} disabled={false} onBusy={vi.fn()} />,
  );
  await screen.findByText("Other browser");
  fireEvent.click(screen.getByRole("button", { name: "Refresh devices" }));
  await screen.findByText("Refresh failed");
  expect(screen.queryByText("Other browser")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry devices" }));
  await screen.findByText("1 signed-in device · 0 other devices");
});
it("ignores a late list response after leaving account settings", async () => {
  const pending = deferred<AccountSession[]>();
  vi.mocked(api.sessions).mockReturnValueOnce(pending.promise);
  const view = render(
    <DeviceSessions onLogout={vi.fn()} disabled={false} onBusy={vi.fn()} />,
  );
  await screen.findByText("Loading signed-in devices…");
  view.unmount();
  await act(async () => pending.resolve([other]));
  expect(screen.queryByText("Other browser")).toBeNull();
});
