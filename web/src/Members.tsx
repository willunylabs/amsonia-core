import { friendlyError } from "./errors";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "./api";
import { ErrorMessage, Modal, PageHeader } from "./components";
import type { Capabilities, Member, Role } from "./types";

import { useDirectory } from "./useDirectory";
import { DirectorySearch, DirectoryNavigation } from "./DirectoryControls";

type Props = {
  tenant: string;
  revision: number;
  capabilities: Capabilities;
  onChanged: () => Promise<void>;
};
export function MembersPage({
  tenant,
  revision,
  capabilities,
  onChanged,
}: Props) {
  const [notice, setNotice] = useState("");
  const [invite, setInvite] = useState(false);
  const [selected, setSelected] = useState<Member | null>(null);
  const [confirm, setConfirm] = useState<{
    member: Member;
    status: "suspended" | "remove";
  } | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const canManage = !!capabilities.permissions["iam:member:manage"],
    canRead = !!capabilities.permissions["iam:member:read"],
    canRoles = !!capabilities.permissions["iam:role:read"];
  const members = useDirectory(api.members, tenant, canRead, revision);
  const invitations = useDirectory(
    api.invitations,
    tenant,
    canManage,
    revision,
  );
  async function refreshInvites() {
    invitations.reload();
  }
  async function mutate(
    member: Member,
    status: "active" | "suspended" | "remove",
  ) {
    setBusy(true);
    setError("");
    try {
      await api.manageMember(tenant, member.account_id, status);
      setConfirm(null);
      setNotice(
        `Member ${status === "remove" ? "removed" : status === "active" ? "resumed" : "suspended"}.`,
      );
      await onChanged();
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  async function revoke(id: string) {
    setBusy(true);
    setError("");
    try {
      await api.revokeInvitation(tenant, id);
      await refreshInvites();
      setNotice("Invitation revoked.");
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        eyebrow="Identity"
        title="Members"
        detail="Invite people, assign roles, and manage access to this tenant."
        action={
          canManage ? (
            <button className="primary-button" onClick={() => setInvite(true)}>
              Invite member
            </button>
          ) : undefined
        }
      />
      <ErrorMessage message={error} />
      {notice ? <p role="status">{notice}</p> : null}
      {canRead ? (
        <>
          <DirectorySearch
            label="Find a member"
            placeholder="Email or account ID"
            statuses={["active", "suspended"]}
            onSearch={members.search}
          />
          <DirectoryNavigation directory={members} label="Members" />
          <div className="table-card">
            <table>
              <thead>
                <tr>
                  <th>Member</th>
                  <th>Status</th>
                  <th>Joined</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {members.page?.items.map((member) => (
                  <tr key={member.account_id}>
                    <td>
                      <strong>{member.email}</strong>
                      <small className="block break-text">
                        {member.account_id}
                      </small>
                    </td>
                    <td>
                      <span
                        className={
                          member.status === "active" ? "pill positive" : "pill"
                        }
                      >
                        {member.status}
                      </span>
                    </td>
                    <td>{new Date(member.joined_at).toLocaleDateString()}</td>
                    <td>
                      <div className="button-row">
                        {canRoles ? (
                          <button
                            className="secondary-button"
                            onClick={() => setSelected(member)}
                          >
                            Roles
                          </button>
                        ) : null}
                        {canManage ? (
                          <>
                            <button
                              className="secondary-button"
                              disabled={busy}
                              onClick={() =>
                                member.status === "active"
                                  ? setConfirm({
                                      member,
                                      status: "suspended",
                                    })
                                  : void mutate(member, "active")
                              }
                            >
                              {member.status === "active"
                                ? "Suspend"
                                : "Resume"}
                            </button>
                            <button
                              className="danger-button"
                              disabled={busy}
                              onClick={() =>
                                setConfirm({ member, status: "remove" })
                              }
                            >
                              Remove
                            </button>
                          </>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {members.page && !members.page.items.length ? (
              <p className="quiet-state">No members match these filters.</p>
            ) : null}
          </div>
        </>
      ) : (
        <p className="quiet-state">
          Your role does not include member read access.
        </p>
      )}
      {canManage ? (
        <section className="paper-card admin-form">
          <h2>Invitations</h2>
          <p>
            New links expire after seven days. Re-inviting an email revokes its
            earlier pending link.
          </p>
          <DirectorySearch
            label="Find an invitation"
            placeholder="Email or invitation ID"
            statuses={["pending", "accepted", "revoked", "expired"]}
            onSearch={invitations.search}
          />
          <DirectoryNavigation directory={invitations} label="Invitations" />
          {invitations.page?.items.length ? (
            invitations.page.items.map((item) => (
              <article className="admin-row" key={item.id}>
                <div>
                  <strong>{item.email}</strong>
                  <p>Expires {new Date(item.expires_at).toLocaleString()}</p>
                  <span className="pill">{item.status}</span>
                </div>
                {item.status === "pending" ? (
                  <button
                    className="secondary-button"
                    disabled={busy}
                    onClick={() => void revoke(item.id)}
                  >
                    Revoke invitation
                  </button>
                ) : null}
              </article>
            ))
          ) : invitations.page ? (
            <p>No invitations match these filters.</p>
          ) : null}
        </section>
      ) : null}
      {invite ? (
        <InviteModal
          tenant={tenant}
          onClose={() => setInvite(false)}
          onCreated={refreshInvites}
        />
      ) : null}
      {selected && canRoles && canRead ? (
        <MemberRoles
          tenant={tenant}
          member={selected}
          canAssign={!!capabilities.permissions["iam:role:assign"]}
          onClose={() => setSelected(null)}
          onChanged={onChanged}
        />
      ) : null}
      {confirm ? (
        <Modal
          title={
            confirm.status === "remove" ? "Remove member?" : "Suspend member?"
          }
          onClose={() => setConfirm(null)}
        >
          <div className="admin-form">
            <p>
              {confirm.member.email} will lose tenant access.
              {confirm.status === "remove"
                ? " Role bindings will be removed. A new invitation is required to rejoin."
                : " Existing role bindings will be restored when membership is resumed."}
            </p>
            <ErrorMessage message={error} />
            <button
              className="danger-button"
              disabled={busy}
              onClick={() => void mutate(confirm.member, confirm.status)}
            >
              Confirm {confirm.status === "remove" ? "removal" : "suspension"}
            </button>
          </div>
        </Modal>
      ) : null}
    </>
  );
}
function InviteModal({
  tenant,
  onClose,
  onCreated,
}: {
  tenant: string;
  onClose: () => void;
  onCreated: () => Promise<void>;
}) {
  const [email, setEmail] = useState("");
  const [link, setLink] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const item = await api.createInvitation(tenant, email);
      setLink(
        `${location.origin}${location.pathname}#invite=${encodeURIComponent(item.token)}`,
      );
      await onCreated();
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
    } catch {
      setError("Copy the invitation link from the field below.");
    }
  }
  return (
    <Modal title="Invite a member" onClose={onClose}>
      <form className="admin-form" onSubmit={submit}>
        <p>Share the link privately. It is shown once; no email is sent.</p>
        {link ? (
          <>
            <label>
              Invitation link
              <input readOnly value={link} onFocus={(e) => e.target.select()} />
            </label>
            <button
              type="button"
              className="primary-button"
              onClick={() => void copy()}
            >
              {copied ? "Copied" : "Copy invitation link"}
            </button>
            <p role="status">Invitation created for {email}.</p>
          </>
        ) : (
          <>
            <label>
              Email
              <input
                type="email"
                autoFocus
                value={email}
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
      </form>
    </Modal>
  );
}
function MemberRoles({
  tenant,
  member,
  canAssign,
  onClose,
  onChanged,
}: {
  tenant: string;
  member: Member;
  canAssign: boolean;
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const directory = useDirectory(api.roles, tenant, true);
  const [notice, setNotice] = useState("");
  const [ids, setIDs] = useState<string[]>([]);
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api
      .memberRoles(tenant, member.account_id)
      .then((value) => {
        if (active) {
          setIDs(value);
          setReady(true);
        }
      })
      .catch((cause) => {
        if (active) setError(friendlyError(cause));
      });
    return () => {
      active = false;
    };
  }, [tenant, member.account_id]);
  async function toggle(role: Role) {
    setBusy(true);
    setError("");
    try {
      await api.bindRole(
        tenant,
        member.account_id,
        role,
        !ids.includes(role.role_id),
      );
      setIDs(await api.memberRoles(tenant, member.account_id));
      setNotice("Role assignment saved.");
      directory.retry();
      await onChanged();
    } catch (cause) {
      setError(friendlyError(cause));
      directory.retry();
      try {
        setIDs(await api.memberRoles(tenant, member.account_id));
      } catch {
        /* Preserve the original mutation error for a deliberate retry. */
      }
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="Member roles" onClose={onClose}>
      <div className="admin-form">
        <p className="break-text">{member.email}</p>
        <ErrorMessage message={error} />
        <p>
          {ready
            ? `${ids.length} assigned roles across all pages.`
            : "Loading assignments…"}
        </p>
        {notice ? <p role="status">{notice}</p> : null}
        {busy ? <p role="status">Saving role assignment…</p> : null}
        <DirectorySearch
          label="Find a role"
          placeholder="Name, description or role ID"
          onSearch={directory.search}
        />
        <DirectoryNavigation directory={directory} label="Roles" />
        {directory.page && !directory.page.items.length ? (
          <p>No roles match these filters.</p>
        ) : null}
        {directory.page?.items.map((role) => (
          <label className="permission-choice" key={role.role_id}>
            <input
              type="checkbox"
              checked={ids.includes(role.role_id)}
              disabled={
                !ready ||
                busy ||
                !canAssign ||
                (member.status !== "active" && !ids.includes(role.role_id))
              }
              onChange={() => void toggle(role)}
            />
            <span>
              <strong>{role.name}</strong>
              <small>
                {role.role_id} · v{role.version}
              </small>
            </span>
          </label>
        ))}
        <p>
          Each change is saved immediately. You can delegate only permissions
          you already hold.
        </p>
      </div>
    </Modal>
  );
}
