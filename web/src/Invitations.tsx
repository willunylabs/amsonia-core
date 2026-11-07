import { useConsoleRoute, useAlive } from "./routing";
import { useState, type FormEvent } from "react";
import { api } from "./api";
import { ErrorMessage, Modal, PageHeader } from "./components";
import { friendlyError } from "./errors";
import type { Capabilities, Invitation } from "./types";
import { useDirectory } from "./useDirectory";
import { DirectorySearch, DirectoryNavigation } from "./DirectoryControls";
import { FormPage } from "./navigation";
type Props = {
  tenant: string;
  revision: number;
  capabilities: Capabilities;
  onChanged: () => Promise<void>;
};
export function InvitationsPage(props: Props) {
  const { segments, navigate } = useConsoleRoute();
  if (!props.capabilities.permissions["iam:member:manage"])
    return (
      <p className="quiet-state">
        Your role does not include invitation management access.
      </p>
    );
  return segments[1] === "new" ? (
    <InviteForm
      tenant={props.tenant}
      onClose={() => navigate("invitations")}
      onCreated={props.onChanged}
    />
  ) : (
    <InvitationDirectory {...props} />
  );
}
function InvitationDirectory({ tenant, revision }: Props) {
  const alive = useAlive();
  const directory = useDirectory(api.invitations, tenant, true, revision);
  const [selected, setSelected] = useState<Invitation>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  async function revoke() {
    if (!selected || busy) return;
    setBusy(true);
    setError("");
    try {
      await api.revokeInvitation(tenant, selected.id);
      if (!alive.current) return;
      setSelected(undefined);
      setNotice("Invitation revoked.");
      directory.reload();
    } catch (e) {
      setError(friendlyError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="Onboarding"
        title="Invitations"
        detail="Track pending invitations and review accepted, revoked or expired links."
        action={
          <a className="primary-button" href="#/invitations/new">
            Invite member
          </a>
        }
      />
      {notice ? <p role="status">{notice}</p> : null}
      <DirectorySearch
        label="Find an invitation"
        placeholder="Email or invitation ID"
        statuses={["pending", "accepted", "revoked", "expired"]}
        onSearch={directory.search}
      />
      <DirectoryNavigation directory={directory} label="Invitations" />
      <div className="table-card">
        <table>
          <thead>
            <tr>
              <th>Email</th>
              <th>Status</th>
              <th>Created</th>
              <th>Expires</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {directory.page?.items.map((item) => (
              <tr key={item.id}>
                <td>
                  <strong>{item.email}</strong>
                  <small className="block break-text">{item.id}</small>
                </td>
                <td>
                  <span className="pill">{item.status}</span>
                </td>
                <td>{new Date(item.created_at).toLocaleString()}</td>
                <td>{new Date(item.expires_at).toLocaleString()}</td>
                <td>
                  {item.status === "pending" ? (
                    <button
                      className="danger-button"
                      onClick={() => {
                        setError("");
                        setSelected(item);
                      }}
                    >
                      Revoke invitation
                    </button>
                  ) : (
                    "—"
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {directory.page && !directory.page.items.length ? (
          <p className="quiet-state">No invitations match these filters.</p>
        ) : null}
      </div>
      {selected ? (
        <Modal
          title="Revoke invitation?"
          onClose={() => {
            if (!busy) setSelected(undefined);
          }}
        >
          <div className="admin-form">
            <p>{selected.email} will no longer be able to use this link.</p>
            <ErrorMessage message={error} />
            <div className="form-actions">
              <button
                className="secondary-button"
                disabled={busy}
                onClick={() => setSelected(undefined)}
              >
                Cancel
              </button>
              <button
                className="danger-button"
                disabled={busy}
                onClick={() => void revoke()}
              >
                {busy ? "Revoking…" : "Confirm revocation"}
              </button>
            </div>
          </div>
        </Modal>
      ) : null}
    </>
  );
}

function InviteForm({
  tenant,
  onClose,
  onCreated,
}: {
  tenant: string;
  onClose: () => void;
  onCreated: () => Promise<void>;
}) {
  const alive = useAlive();
  const [email, setEmail] = useState("");
  const [issued, setIssued] = useState<{ email: string; link: string } | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || issued) return;
    setBusy(true);
    setError("");
    try {
      const item = await api.createInvitation(tenant, email);
      if (!alive.current) return;
      setIssued({
        email: item.email,
        link: `${location.origin}${location.pathname}#invite=${encodeURIComponent(item.token)}`,
      });
      await onCreated();
    } catch (cause) {
      if (alive.current) setError(friendlyError(cause));
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  async function copy() {
    if (!issued) return;
    setError("");
    try {
      await navigator.clipboard.writeText(issued.link);
      if (alive.current) setCopied(true);
    } catch {
      if (alive.current) setError("Copy the invitation link from the field below.");
    }
  }
  return (
    <FormPage
      title="Invite member"
      detail="Create a private, single-use organization invitation."
      back={{ path: "invitations", label: "Back to invitations" }}
    >
      <form className="admin-form" onSubmit={submit}>
        <p>Share the link privately. It is shown once; no email is sent.</p>
        {issued ? (
          <>
            <label>
              Invitation link
              <input readOnly value={issued.link} onFocus={(e) => e.target.select()} />
            </label>
            <button
              type="button"
              className="primary-button"
              onClick={() => void copy()}
            >
              {copied ? "Copied" : "Copy invitation link"}
            </button>
            <p role="status">Invitation created for {issued.email}.</p>
          </>
        ) : (
          <>
            <label>
              Email
              <input
                type="email"
                autoComplete="email"
                maxLength={254}
                autoFocus
                value={email}
                disabled={busy}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </label>
            <button className="primary-button" disabled={busy}>
              {busy ? "Creating…" : "Create invitation"}
            </button>
          </>
        )}
        <ErrorMessage message={error} />
        <div className="form-actions">
          <button
            type="button"
            className="secondary-button"
            disabled={busy}
            onClick={onClose}
          >
            {issued ? "Done" : "Cancel"}
          </button>
        </div>
      </form>
    </FormPage>
  );
}
