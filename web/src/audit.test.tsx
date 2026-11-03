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
import { AuditWorkspace } from "./Audit";
import type { AuditEvent, AuditPage } from "./types";

const event = (id: number, target: string): AuditEvent => ({
  id,
  tenant_id: "tenant",
  operation: "role.update",
  phase: "result",
  target_type: "role",
  target_id: target,
  outcome: "success",
  at: "2026-10-25T00:00:00Z",
});
let createURL: ReturnType<typeof vi.fn>;
let revokeURL: ReturnType<typeof vi.fn>;
let click: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  vi.restoreAllMocks();
  createURL = vi.fn().mockReturnValue("blob:owned-audit-download");
  revokeURL = vi.fn();
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    writable: true,
    value: createURL,
  });
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    writable: true,
    value: revokeURL,
  });
  click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {});
});
afterEach(cleanup);

it("preserves applied local date boundaries on older pages and exports only the applied search", async () => {
  const load = vi
    .spyOn(api, "auditPage")
    .mockImplementation(async (_tenant, filter) => ({
      items: [event(filter.before ? 1 : 2, filter.before ? "older" : "newer")],
      next_cursor: filter.before ? undefined : 2,
    }));
  const exportFile = vi
    .spyOn(api, "exportAudit")
    .mockResolvedValue({
      blob: new Blob(["csv"]),
      filename: "amsonia-audit.csv",
    });
  render(<AuditWorkspace tenant="tenant" allowed />);
  await screen.findByText("role · newer");
  fireEvent.change(screen.getByLabelText("Search"), {
    target: { value: "person" },
  });
  fireEvent.change(screen.getByLabelText("From (local time)"), {
    target: { value: "2026-10-25T08:00" },
  });
  fireEvent.change(screen.getByLabelText("Until (local time)"), {
    target: { value: "2026-10-25T09:00" },
  });
  fireEvent.submit(screen.getByLabelText("Search").closest("form")!);
  await screen.findByText("role · newer");
  fireEvent.click(screen.getByRole("button", { name: "Older events" }));
  await screen.findByText("role · older");
  expect(load).toHaveBeenLastCalledWith(
    "tenant",
    expect.objectContaining({
      q: "person",
      before: 2,
      from: new Date("2026-10-25T08:00").toISOString(),
      until: new Date("2026-10-25T09:00").toISOString(),
    }),
  );
  fireEvent.change(screen.getByLabelText("Search"), {
    target: { value: "unapplied" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Export matching events" }),
  );
  await screen.findByText("Audit CSV downloaded.");
  expect(exportFile).toHaveBeenCalledWith(
    "tenant",
    expect.objectContaining({ q: "person", before: 2 }),
    expect.any(AbortSignal),
  );
  expect(click).toHaveBeenCalledOnce();
  expect(document.querySelector("a[download]")).toBeNull();
  await waitFor(() =>
    expect(revokeURL).toHaveBeenCalledWith("blob:owned-audit-download"),
  );
});

it("keeps a reversed date range from reaching the API", async () => {
  const load = vi.spyOn(api, "auditPage").mockResolvedValue({ items: [] });
  render(<AuditWorkspace tenant="tenant" allowed />);
  await screen.findByText("No events match these filters.");
  fireEvent.change(screen.getByLabelText("From (local time)"), {
    target: { value: "2026-10-26T08:00" },
  });
  fireEvent.change(screen.getByLabelText("Until (local time)"), {
    target: { value: "2026-10-25T08:00" },
  });
  fireEvent.submit(screen.getByLabelText("Search").closest("form")!);
  await screen.findByText("Choose a valid start time before the end time.");
  expect(load).toHaveBeenCalledOnce();
});

it("cancels an export and ignores its late response", async () => {
  vi.spyOn(api, "auditPage").mockResolvedValue({ items: [] });
  let resolve!: (file: { blob: Blob; filename: string }) => void;
  const pending = new Promise<{ blob: Blob; filename: string }>((done) => {
    resolve = done;
  });
  const exportFile = vi.spyOn(api, "exportAudit").mockReturnValue(pending);
  render(<AuditWorkspace tenant="tenant" allowed />);
  await screen.findByText("No events match these filters.");
  fireEvent.click(
    screen.getByRole("button", { name: "Export matching events" }),
  );
  await screen.findByRole("button", { name: "Cancel export" });
  fireEvent.click(screen.getByRole("button", { name: "Cancel export" }));
  await screen.findByText("Export cancelled.");
  expect(exportFile.mock.calls[0][2]?.aborted).toBe(true);
  await act(async () =>
    resolve({ blob: new Blob(["old"]), filename: "old.csv" }),
  );
  expect(createURL).not.toHaveBeenCalled();
  expect(click).not.toHaveBeenCalled();
});

it("does not download an obsolete tenant export or overwrite a new pending download", async () => {
  vi.spyOn(api, "auditPage").mockResolvedValue({ items: [] });
  let old!: (file: { blob: Blob; filename: string }) => void;
  let latest!: (file: { blob: Blob; filename: string }) => void;
  vi.spyOn(api, "exportAudit")
    .mockReturnValueOnce(
      new Promise((done) => {
        old = done;
      }),
    )
    .mockReturnValueOnce(
      new Promise((done) => {
        latest = done;
      }),
    );
  const view = render(<AuditWorkspace tenant="a" allowed />);
  await screen.findByText("No events match these filters.");
  fireEvent.click(
    screen.getByRole("button", { name: "Export matching events" }),
  );
  view.rerender(<AuditWorkspace tenant="b" allowed />);
  await screen.findByText("No events match these filters.");
  fireEvent.click(
    screen.getByRole("button", { name: "Export matching events" }),
  );
  await act(async () => old({ blob: new Blob(["old"]), filename: "old.csv" }));
  expect(createURL).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Preparing CSV…" })).toBeTruthy();
  await act(async () =>
    latest({ blob: new Blob(["new"]), filename: "new.csv" }),
  );
  await screen.findByText("Audit CSV downloaded.");
  expect(click).toHaveBeenCalledOnce();
});

it("hides obsolete query rows and retries a failed new query", async () => {
  let resolve!: (page: AuditPage) => void;
  vi.spyOn(api, "auditPage")
    .mockReturnValueOnce(
      new Promise((done) => {
        resolve = done;
      }),
    )
    .mockRejectedValueOnce(new TypeError("offline"))
    .mockResolvedValue({ items: [event(3, "current")] });
  const view = render(<AuditWorkspace tenant="a" allowed />);
  view.rerender(<AuditWorkspace tenant="b" allowed />);
  await screen.findByRole("button", { name: "Retry audit query" });
  await act(async () => resolve({ items: [event(1, "obsolete")] }));
  expect(screen.queryByText("role · obsolete")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry audit query" }));
  await screen.findByText("role · current");
});

it("shows export limits without triggering a download and suppresses all audit requests without access", async () => {
  vi.spyOn(api, "auditPage").mockResolvedValue({ items: [] });
  const exportFile = vi
    .spyOn(api, "exportAudit")
    .mockRejectedValue(
      new APIError(
        422,
        "audit_export_too_large",
        "Narrow the date range or filters.",
      ),
    );
  const view = render(<AuditWorkspace tenant="tenant" allowed />);
  await screen.findByText("No events match these filters.");
  fireEvent.click(
    screen.getByRole("button", { name: "Export matching events" }),
  );
  await screen.findByText("Narrow the date range or filters.");
  expect(click).not.toHaveBeenCalled();
  view.rerender(<AuditWorkspace tenant="tenant" allowed={false} />);
  expect(
    screen.queryByRole("button", { name: "Export matching events" }),
  ).toBeNull();
  expect(exportFile).toHaveBeenCalledOnce();
});
