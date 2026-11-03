import { useEffect, useRef, useState } from "react";
import { api, setToken } from "./api";
import { ErrorMessage, Modal } from "./components";
import { friendlyError } from "./errors";
import type { AccountSession } from "./types";

const ignoreBusy = () => {};

export function DeviceSessions({
  onLogout,
  disabled,
  onBusy = ignoreBusy,
}: {
  onLogout: () => void;
  disabled: boolean;
  onBusy?: (busy: boolean) => void;
}) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{
    attempt: number;
    items?: AccountSession[];
    error?: string;
  }>();
  const [confirmation, setConfirmation] = useState<AccountSession | "others">();
  const [mutating, setMutating] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const alive = useRef(false);
  const serial = useRef(0);
  const saving = useRef(false);
  const current = result?.attempt === attempt ? result : undefined;
  const loading = !current;
  const sessions = current?.items ?? [];
  const otherCount = sessions.filter((s) => !s.current).length;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      onBusy(false);
    };
  }, [onBusy]);
  useEffect(() => {
    const request = ++serial.current;
    let active = true;
    api
      .sessions()
      .then((items) => {
        if (active && serial.current === request) setResult({ attempt, items });
      })
      .catch((cause) => {
        if (active && serial.current === request)
          setResult({ attempt, error: friendlyError(cause) });
      });
    return () => {
      active = false;
    };
  }, [attempt]);

  function ask(target: AccountSession | "others") {
    if (disabled || loading || saving.current) return;
    setError("");
    setMessage("");
    setConfirmation(target);
  }
  async function signOut() {
    if (!confirmation || disabled || saving.current) return;
    saving.current = true;
    ++serial.current;
    setMutating(true);
    onBusy(true);
    setError("");
    setMessage("");
    try {
      if (confirmation === "others") await api.revokeOtherSessions();
      else await api.revokeSession(confirmation.id);
      if (!alive.current) return;
      if (confirmation !== "others" && confirmation.current) {
        // The family is already revoked. Cookie cleanup can fail offline; it
        // must not leave a known-revoked bearer displayed as signed in.
        await api.logout().catch(() => undefined);
        if (alive.current) {
          setToken("");
          onLogout();
        }
        return;
      }
      setConfirmation(undefined);
      setMessage(
        confirmation === "others"
          ? "Other devices signed out. This device remains signed in."
          : "Device signed out.",
      );
      setAttempt((value) => value + 1);
    } catch (cause) {
      if (alive.current) setError(friendlyError(cause));
    } finally {
      saving.current = false;
      if (alive.current) {
        setMutating(false);
        onBusy(false);
      }
    }
  }
  return (
    <section className="paper-card admin-form" aria-labelledby="devices-title">
      <div className="device-heading">
        <div>
          <h2 id="devices-title">Signed-in devices</h2>
          <p>Signing out a device invalidates its access and refresh tokens.</p>
        </div>
        <div className="button-row">
          <button
            className="secondary-button"
            disabled={disabled || loading || mutating}
            onClick={() => {
              setError("");
              setMessage("");
              setAttempt((v) => v + 1);
            }}
          >
            Refresh devices
          </button>
          <button
            className="secondary-button"
            disabled={disabled || loading || mutating || !otherCount}
            onClick={() => ask("others")}
          >
            Sign out other devices
          </button>
        </div>
      </div>
      {loading ? <p role="status">Loading signed-in devices…</p> : null}
      <ErrorMessage message={current?.error ?? ""} />
      {current?.error ? (
        <button
          className="secondary-button"
          disabled={disabled || mutating}
          onClick={() => setAttempt((v) => v + 1)}
        >
          Retry devices
        </button>
      ) : null}
      {!confirmation ? <ErrorMessage message={error} /> : null}
      {message ? (
        <p role="status" className="success-note">
          {message}
        </p>
      ) : null}
      {current?.items ? (
        <p>
          {sessions.length} signed-in{" "}
          {sessions.length === 1 ? "device" : "devices"} · {otherCount} other{" "}
          {otherCount === 1 ? "device" : "devices"}
        </p>
      ) : null}
      {current?.items && !sessions.length ? (
        <p className="quiet-state">
          No active devices found. Refresh to check again.
        </p>
      ) : null}
      {sessions.map((session) => (
        <article className="admin-row" key={session.id}>
          <div>
            <strong>{session.current ? "This device" : "Other device"}</strong>
            <p className="break-text">
              {session.user_agent || "Unknown browser"}
            </p>
            <small className="break-text">
              {session.remote_address || "Unknown address"} · Last active{" "}
              {new Date(session.last_seen_at).toLocaleString()}
            </small>
            <p className="device-dates">
              Signed in {new Date(session.created_at).toLocaleString()} ·
              Expires {new Date(session.expires_at).toLocaleString()}
            </p>
          </div>
          <button
            className="secondary-button"
            disabled={disabled || mutating}
            onClick={() => ask(session)}
          >
            Sign out {session.current ? "this device" : "device"}
          </button>
        </article>
      ))}
      {confirmation ? (
        <Modal
          title={
            confirmation === "others"
              ? "Sign out other devices?"
              : confirmation.current
                ? "Sign out this device?"
                : "Sign out device?"
          }
          onClose={() => {
            if (!saving.current) setConfirmation(undefined);
          }}
        >
          <p>
            {confirmation === "others"
              ? "Every other device signed in to your account will lose access, including devices outside this list. This device stays signed in. Future sign-ins are still allowed."
              : confirmation.current
                ? "This device will lose access and return to sign-in. Your other devices stay signed in."
                : "This device will lose access and must sign in again. Your current device stays signed in."}
          </p>
          {confirmation !== "others" ? (
            <p className="break-text">
              {confirmation.user_agent || "Unknown browser"} ·{" "}
              {confirmation.remote_address || "Unknown address"}
            </p>
          ) : null}
          <ErrorMessage message={error} />
          <div className="button-row">
            <button
              className="secondary-button"
              disabled={mutating}
              onClick={() => setConfirmation(undefined)}
            >
              Cancel
            </button>
            <button
              className="primary-button"
              disabled={disabled || mutating}
              onClick={() => void signOut()}
            >
              {mutating ? "Signing out…" : "Confirm sign out"}
            </button>
          </div>
        </Modal>
      ) : null}
    </section>
  );
}
