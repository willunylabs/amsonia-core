import { useConsoleRoute, useAlive } from "./routing";
import { friendlyError } from "./errors";
import { useEffect, useState, type FormEvent } from "react";
import { api } from "./api";
import { ErrorMessage, PageHeader } from "./components";
import type { Capabilities, Member, Role } from "./types";
import { useDirectory } from "./useDirectory";
import { DirectorySearch, DirectoryNavigation } from "./DirectoryControls";
import { MemberPasswordRecovery } from "./PasswordRecovery";
import { PageNavigation } from "./navigation";
type Props = {
  systemAdmin?: boolean;
  actorID?: string;
  tenant: string;
  revision: number;
  capabilities: Capabilities;
  onChanged: () => Promise<void>;
};
export function MembersPage(props: Props) {
  const { segments } = useConsoleRoute();
  if (!props.capabilities.permissions["iam:member:read"])
    return (
      <p className="quiet-state">
        Your role does not include member read access.
      </p>
    );
  return segments[1] ? (
    <MemberWorkspace
      key={`${props.tenant}:${segments[1]}:${segments[2] ?? "details"}`}
      {...props}
      id={segments[1]}
    />
  ) : (
    <MemberDirectory {...props} />
  );
}
function MemberDirectory({ tenant, revision, capabilities }: Props) {
  const members = useDirectory(api.members, tenant, true, revision);
  const canRoles = !!capabilities.permissions["iam:role:read"];
  const { navigate } = useConsoleRoute();
  return (
    <>
      <PageHeader
        eyebrow="Identity"
        title="Members"
        detail="Find people, review their roles, and manage organization access."
      />
      <DirectorySearch
        label="Find a member"
        placeholder="Email or account ID"
        statuses={["active", "suspended"]}
        onSearch={members.search}
      />
      <DirectoryNavigation directory={members} label="Members" />
      <div className="table-card" role="region" aria-label="Member results" tabIndex={0}>
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
                    <button
                      className="secondary-button"
                      onClick={() =>
                        navigate(
                          `members/${encodeURIComponent(member.account_id)}/details`,
                        )
                      }
                    >
                      Manage member
                    </button>
                    {canRoles ? (
                      <button
                        className="secondary-button"
                        onClick={() =>
                          navigate(
                            `members/${encodeURIComponent(member.account_id)}/roles`,
                          )
                        }
                      >
                        Roles
                      </button>
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
  );
}
function MemberWorkspace({
  tenant,
  id,
  systemAdmin,
  actorID,
  capabilities,
  onChanged,
}: Props & { id: string }) {
  const { segments, navigate } = useConsoleRoute();
  const tab = segments[2] ?? "details";
  const base = `members/${encodeURIComponent(id)}`;
  const alive = useAlive();
  const [member, setMember] = useState<Member>();
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState("active");
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const manage = !!capabilities.permissions["iam:member:manage"];
  useEffect(() => {
    let active = true;
    setError("");
    api
      .member(tenant, id)
      .then((value) => {
        if (active) {
          setMember(value);
          setStatus(value.status);
        }
      })
      .catch((e) => {
        if (active) setError(friendlyError(e));
      });
    return () => {
      active = false;
    };
  }, [tenant, id, attempt]);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!member || !manage || busy) return;
    setError("");
    setNotice("");
    if (tab === "remove" && email !== member.email) {
      setError("Enter the member's email to confirm removal.");
      return;
    }
    setBusy(true);
    try {
      await api.manageMember(
        tenant,
        id,
        tab === "remove" ? "remove" : (status as "active" | "suspended"),
      );
      await onChanged();
      if (!alive.current) return;
      if (tab === "remove") {
        navigate("members");
        return;
      }
      setMember({ ...member, status });
      setNotice("Membership saved.");
    } catch (e) {
      setError(friendlyError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <a className="back-link" href="#/members">
        ← Back to members
      </a>
      <PageHeader
        eyebrow="Tenant member"
        title={member?.email ?? "Member settings"}
        detail={
          tab === "roles"
            ? "Review and assign this member's roles."
            : tab === "recovery"
              ? "Help this member recover access to their account."
            : tab === "remove"
              ? "Remove membership from this tenant."
              : "Review identity and update tenant access."
        }
      />
      <PageNavigation
        label="Member navigation"
        items={[
          { path: `${base}/details`, label: "Membership" },
          ...(capabilities.permissions["iam:role:read"]
            ? [{ path: `${base}/roles`, label: "Roles" }]
            : []),
          ...(manage && systemAdmin && id !== actorID && member?.status === "active"
            ? [{path: `${base}/recovery`, label: "Password recovery"}] : []),
          ...(manage
            ? [{ path: `${base}/remove`, label: "Remove membership" }]
            : []),
        ]}
      />
      <ErrorMessage message={error} />
      {notice ? <p role="status">{notice}</p> : null}
      {!member ? (
        error ? (
          <button
            className="secondary-button"
            onClick={() => setAttempt((v) => v + 1)}
          >
            Retry member
          </button>
        ) : (
          <p role="status">Loading member…</p>
        )
      ) : tab === "roles" ? (
        capabilities.permissions["iam:role:read"] ? (
          <MemberRoles
            tenant={tenant}
            member={member}
            canAssign={!!capabilities.permissions["iam:role:assign"]}
            onChanged={onChanged}
          />
        ) : (
          <p className="quiet-state">
            Your role does not include role read access.
          </p>
        )
      ) : tab === "recovery" && manage && systemAdmin && id !== actorID && member.status === "active" ? (
        <MemberPasswordRecovery tenant={tenant} id={id} email={member.email} />
      ) : tab === "details" ? (
        <form className="paper-card admin-form form-page" onSubmit={save}>
          <h2>Membership details</h2>
          <label>
            Email
            <input value={member.email} disabled />
          </label>
          <label>
            Account ID
            <input value={member.account_id} disabled />
          </label>
          <label>
            Joined
            <input
              value={new Date(member.joined_at).toLocaleString()}
              disabled
            />
          </label>
          <label>
            Membership status
            <select
              aria-label="Membership status"
              value={status}
              onChange={(e) => setStatus(e.target.value)}
              disabled={!manage || busy}
            >
              <option value="active">Active</option>
              <option value="suspended">Suspended</option>
            </select>
          </label>
          <p>
            Suspending blocks tenant access and retains role assignments.
            Resuming restores access with those roles.
          </p>
          <div className="form-actions">
            <a className="secondary-button" href="#/members">
              Cancel
            </a>
            {manage ? (
              <button
                className="primary-button"
                disabled={busy || status === member.status}
              >
                {busy ? "Saving…" : "Save membership"}
              </button>
            ) : null}
          </div>
        </form>
      ) : tab === "remove" && manage ? (
        <form className="paper-card admin-form form-page" onSubmit={save}>
          <h2>Remove membership</h2>
          <p>
            Removing {member.email} deletes this tenant membership and role
            bindings. A new invitation is required to rejoin. The account and
            other tenants are retained.
          </p>
          <label>
            Confirm member email
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              disabled={busy}
            />
          </label>
          <div className="form-actions">
            <a className="secondary-button" href={`#/${base}/details`}>
              Cancel
            </a>
            <button className="danger-button" disabled={busy}>
              {busy ? "Removing…" : "Remove member"}
            </button>
          </div>
        </form>
      ) : (
        <p className="quiet-state">This member page is unavailable.</p>
      )}
    </>
  );
}

function MemberRoles({
  tenant,
  member,
  canAssign,
  onChanged,
}: {
  tenant: string;
  member: Member;
  canAssign: boolean;
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
    <section className="paper-card admin-form">
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
    </section>
  );
}
