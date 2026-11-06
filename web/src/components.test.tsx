import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { Modal } from "./components";

afterEach(cleanup);

function PendingConfirmation() {
  const [busy, setBusy] = useState(false);
  return <><button>Background action</button><Modal title="Confirm action" onClose={vi.fn()}>
    <button disabled={busy} onClick={() => setBusy(true)}>Confirm</button>
  </Modal></>;
}

it("keeps Tab inside a dialog when the focused action becomes disabled", () => {
  render(<PendingConfirmation />);
  const dialog = screen.getByRole("dialog");
  const confirm = within(dialog).getByRole("button", { name: "Confirm" });
  confirm.focus();
  fireEvent.click(confirm);
  expect(confirm).toHaveProperty("disabled", true);
  fireEvent.keyDown(document, { key: "Tab" });
  expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Close" }));
});

it("recovers focus in both directions after it leaves the dialog", () => {
  render(<PendingConfirmation />);
  const background = screen.getByRole("button", { name: "Background action" });
  const dialog = screen.getByRole("dialog");
  background.focus();
  fireEvent.keyDown(document, { key: "Tab" });
  expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Close" }));
  background.focus();
  fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
  expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Confirm" }));
});
