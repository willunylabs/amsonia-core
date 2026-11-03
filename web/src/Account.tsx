import { friendlyError } from "./errors";
import { useEffect, useState, type FormEvent } from "react";
import { api, setToken } from "./api";
import { ErrorMessage, PageHeader } from "./components";
import type {
  Account,
  AccountSession,
  InvitationPreview,
  Profile,
} from "./types";

export function AccountPage({ onLogout }: { onLogout: () => void }) {
  const [profile, setProfile] = useState<Profile | null>(null);
  const [sessions, setSessions] = useState<AccountSession[]>([]);
  const [name, setName] = useState("");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    Promise.all([api.profile(), api.sessions()])
      .then(([p, s]) => {
        if (active) {
          setProfile(p);
          setName(p.display_name);
          setSessions(s);
        }
      })
      .catch((cause) => {
        if (active) setError(friendlyError(cause));
      });
    return () => {
      active = false;
    };
  }, []);
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setMessage("");
    try {
      setProfile(await api.updateProfile(name));
      setMessage("Profile saved.");
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  async function password(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.changePassword(current, next);
      setToken("");
      onLogout();
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  async function revoke(session: AccountSession) {
    setBusy(true);
    setError("");
    try {
      await api.revokeSession(session.id);
      if (session.current) {
        await api.logout();
        onLogout();
      } else {
        setSessions(await api.sessions());
        setMessage("Session revoked.");
      }
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="Your account"
        title="Account settings"
        detail="Manage your profile, password, and signed-in devices."
      />
      <ErrorMessage message={error} />
      {message ? (
        <p role="status" className="success-note">
          {message}
        </p>
      ) : null}
      <div className="admin-grid">
        <form className="paper-card admin-form" onSubmit={save}>
          <h2>Profile</h2>
          <label>
            Email
            <input value={profile?.email ?? ""} disabled />
          </label>
          <label>
            Display name
            <input
              value={name}
              maxLength={80}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <button className="primary-button" disabled={busy || !profile}>
            Save profile
          </button>
        </form>
        <form className="paper-card admin-form" onSubmit={password}>
          <h2>Change password</h2>
          <p>
            All sessions will be revoked. Sign in again with your new password.
          </p>
          <label>
            Current password
            <input
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              required
            />
          </label>
          <label>
            New password
            <input
              type="password"
              autoComplete="new-password"
              minLength={12}
              maxLength={128}
              value={next}
              onChange={(e) => setNext(e.target.value)}
              required
            />
          </label>
          <button className="primary-button" disabled={busy}>
            Change password and sign out
          </button>
        </form>
      </div>
      <section className="paper-card admin-form">
        <h2>Signed-in devices</h2>
        <p>Revoking a device invalidates its access and refresh tokens.</p>
        {sessions.map((session) => (
          <article className="admin-row" key={session.id}>
            <div>
              <strong>
                {session.current ? "This device" : "Other device"}
              </strong>
              <p className="break-text">
                {session.user_agent || "Unknown browser"}
              </p>
              <small>
                {session.remote_address} · Last active{" "}
                {new Date(session.last_seen_at).toLocaleString()}
              </small>
            </div>
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() => void revoke(session)}
            >
              Revoke{session.current ? " this session" : " session"}
            </button>
          </article>
        ))}
      </section>
    </>
  );
}

export function InvitationLanding({
  token,
  onLogin,
}: {
  token: string;
  onLogin: (account: Account) => void;
}) {
  const [preview, setPreview] = useState<InvitationPreview | null>(null);
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [accepted, setAccepted] = useState(false);
  useEffect(() => {
    let active = true;
    api
      .previewInvitation(token)
      .then((value) => {
        if (active) setPreview(value);
      })
      .catch((cause) => {
        if (active) setError(friendlyError(cause));
      });
    return () => {
      active = false;
    };
  }, [token]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!preview) return;
    setBusy(true);
    setError("");
    try {
      if (!accepted) {
        await api.acceptInvitation(token, password);
        setAccepted(true);
      }
      const session = await api.login(preview.email, password);
      history.replaceState(null, "", location.pathname + location.search);
      onLogin(session.account);
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="invitation-shell">
      <section className="login-card">
        <span className="eyebrow">Amsonia Core · Invitation</span>
        <h1>{preview ? `Join ${preview.tenant_name}` : "Tenant invitation"}</h1>
        <ErrorMessage message={error} />
        {preview ? (
          <form className="admin-form" onSubmit={submit}>
            <p>
              You have been invited as <strong>{preview.email}</strong>. Your
              administrator can assign roles after you join.
            </p>
            <p>
              {preview.account_exists
                ? "Use your existing account password. Joining will preserve your password and other memberships."
                : "Choose an account password with at least 12 characters."}
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
                minLength={preview.account_exists ? undefined : 12}
                maxLength={128}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
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
        <a href="/">Back to sign in</a>
      </section>
    </main>
  );
}
