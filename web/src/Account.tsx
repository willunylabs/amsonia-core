import { useConsoleRoute, useAlive } from "./routing";
import { friendlyError } from "./errors";
import { useEffect, useState, type FormEvent } from "react";
import { api, APIError } from "./api";
import { ErrorMessage, PageHeader } from "./components";
import { DeviceSessions } from "./DeviceSessions";
import { PageNavigation } from "./navigation";
import { PASSWORD_LENGTH_MESSAGE, validNewPassword } from "./password";
import type { Account, InvitationPreview, Profile } from "./types";

export function AccountPage({
  onLogout,
  email = "",
}: {
  onLogout: () => void;
  email?: string;
}) {
  const { segments } = useConsoleRoute();
  const page = segments[1] ?? "profile";
  const titles: Record<string, string> = {
    profile: "Account profile",
    password: "Change password",
    devices: "Signed-in devices",
  };
  return (
    <>
      <PageHeader
        eyebrow="Your account"
        title={titles[page] ?? "Account settings"}
        detail={
          page === "profile"
            ? "Update your name and review your account identity."
            : page === "password"
              ? "Choose a new password for your account."
              : "Review and sign out your account's devices."
        }
      />
      <PageNavigation
        label="Account navigation"
        items={[
          { path: "account/profile", label: "Profile" },
          { path: "account/password", label: "Password" },
          { path: "account/devices", label: "Devices" },
        ]}
      />
      {page === "profile" ? (
        <ProfileForm />
      ) : page === "password" ? (
        <PasswordForm email={email} onLogout={onLogout} />
      ) : page === "devices" ? (
        <DeviceSessions onLogout={onLogout} disabled={false} />
      ) : (
        <p className="quiet-state">This account page does not exist.</p>
      )}
    </>
  );
}
function ProfileForm() {
  const alive = useAlive();
  const [profile, setProfile] = useState<Profile>();
  const [attempt, setAttempt] = useState(0);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    api
      .profile()
      .then((p) => {
        if (active) {
          setProfile(p);
          setName(p.display_name);
        }
      })
      .catch((e) => {
        if (active) setError(friendlyError(e));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [attempt]);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy || !profile) return;
    if (Array.from(name.trim()).length > 80) {
      setError("Use at most 80 characters for your display name.");
      return;
    }
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const updated = await api.updateProfile(name);
      if (!alive.current) return;
      setProfile(updated);
      setName(updated.display_name);
      setMessage("Profile saved.");
    } catch (e) {
      setError(friendlyError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="paper-card admin-form form-page" onSubmit={save}>
      <h2>Profile details</h2>
      <ErrorMessage message={error} />
      {message ? (
        <p role="status" className="success-note">
          {message}
        </p>
      ) : null}
      {loading ? <p role="status">Loading profile…</p> : null}
      {!profile && error ? (
        <button
          type="button"
          className="secondary-button"
          onClick={() => setAttempt((v) => v + 1)}
        >
          Retry profile
        </button>
      ) : null}
      <label>
        Email
        <input value={profile?.email ?? ""} disabled autoComplete="username" />
      </label>
      <p>Your email identifies your account and cannot be changed here.</p>
      <label>
        Display name
        <input
          value={name}
          disabled={busy || loading || !profile}
          onChange={(e) => {
            setName(e.target.value);
            setMessage("");
          }}
        />
      </label>
      <small>Up to 80 characters.</small>
      <div className="form-actions">
        <a className="secondary-button" href="#/overview">
          Cancel
        </a>
        <button
          className="primary-button"
          disabled={busy || loading || !profile}
        >
          {busy ? "Saving…" : "Save profile"}
        </button>
      </div>
    </form>
  );
}
function PasswordForm({
  email,
  onLogout,
}: {
  email: string;
  onLogout: () => void;
}) {
  const alive = useAlive();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setError("");
    if (next !== confirm) {
      setError("The new passwords do not match.");
      return;
    }
    if (!validNewPassword(next)) {
      setError(PASSWORD_LENGTH_MESSAGE);
      return;
    }
    setBusy(true);
    try {
      await api.changePassword(current, next);
      if (alive.current) onLogout();
    } catch (e) {
      if (alive.current) setError(friendlyError(e));
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <form className="paper-card admin-form form-page" onSubmit={save}>
      <h2>Password details</h2>
      <p>
        All devices, including this one, will be signed out. Use 12–128
        characters.
      </p>
      <ErrorMessage message={error} />
      <input type="hidden" autoComplete="username" value={email} />
      <label>
        Current password
        <input
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          required
          disabled={busy}
        />
      </label>
      <label>
        New password
        <input
          type="password"
          autoComplete="new-password"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          required
          disabled={busy}
        />
      </label>
      <label>
        Confirm new password
        <input
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          required
          disabled={busy}
        />
      </label>
      <div className="form-actions">
        <a className="secondary-button" href="#/account/profile">
          Cancel
        </a>
        <button className="primary-button" disabled={busy}>
          {busy ? "Changing password…" : "Change password and sign out"}
        </button>
      </div>
    </form>
  );
}

export function InvitationLanding({
  token,
  onLogin,
}: {
  token: string;
  onLogin: (account: Account) => void;
}) {
  const alive = useAlive();
  const [preview, setPreview] = useState<InvitationPreview | null>(null);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [accepted, setAccepted] = useState(false);
  const [showSignInHelp, setShowSignInHelp] = useState(false);
  useEffect(() => {
    let active = true;
    api
      .previewInvitation(token)
      .then((value) => {
        if (active) setPreview(value);
      })
      .catch((cause) => {
        if (active) {
          setError(friendlyError(cause));
          if (cause instanceof APIError && cause.status === 404)
            setShowSignInHelp(true);
        }
      });
    return () => {
      active = false;
    };
  }, [token]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!preview || busy) return;
    if (!preview.account_exists && !accepted && !validNewPassword(password)) {
      setError(PASSWORD_LENGTH_MESSAGE);
      return;
    }
    setBusy(true);
    setError("");
    setShowSignInHelp(false);
    let joined = accepted;
    try {
      if (!accepted) {
        await api.acceptInvitation(token, password);
        if (!alive.current) return;
        joined = true;
        setAccepted(true);
      }
      const session = await api.login(preview.email, password);
      if (!alive.current) return;
      history.replaceState(null, "", location.pathname + location.search);
      onLogin(session.account);
    } catch (cause) {
      if (alive.current) {
        setError(friendlyError(cause));
        if (!joined && (!(cause instanceof APIError) || cause.status >= 500 || cause.status === 404))
          setShowSignInHelp(true);
      }
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <main className="invitation-shell">
      <section className="login-card">
        <span className="eyebrow">Amsonia Core · Invitation</span>
        <h1>{preview ? `Join ${preview.tenant_name}` : "Tenant invitation"}</h1>
        <ErrorMessage message={error} />
        {showSignInHelp ? (
          <p>
            If you already joined, go back to sign in with your password. If
            you cannot sign in and the link no longer works, contact your
            administrator.
          </p>
        ) : null}
        {preview ? (
          <form className="admin-form" onSubmit={submit}>
            <p>
              You have been invited as <strong>{preview.email}</strong>. Your
              administrator can assign roles after you join.
            </p>
            <p>
              {preview.account_exists
                ? "Use your existing account password. Joining will preserve your password and other memberships."
                : "Choose an account password with 12–128 characters."}
            </p>
            <label>
              {preview.account_exists
                ? "Account password"
                : "Choose a password"}
              <input
                autoFocus
                type="password"
                autoComplete={
                  preview.account_exists ? "current-password" : "new-password"
                }
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={busy}
                required
              />
            </label>
            <small>
              Expires {new Date(preview.expires_at).toLocaleString()}
            </small>
            <button className="primary-button" disabled={busy}>
              {busy ? "Joining…" : accepted ? "Sign in" : "Accept invitation"}
            </button>
          </form>
        ) : !error ? (
          <p>Loading invitation…</p>
        ) : null}
        <a href={location.pathname + location.search}>Back to sign in</a>
      </section>
    </main>
  );
}
