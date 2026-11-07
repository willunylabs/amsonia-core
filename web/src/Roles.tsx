import { useConsoleRoute, useAlive } from "./routing";
import { friendlyError } from "./errors";
import { roleDetailsError, trimRoleName } from "./roleFields";
import { useEffect, useState, type FormEvent } from "react";
import { ShieldCheck } from "lucide-react";
import { api } from "./api";
import { RoleMembers } from "./RoleMembers";
import { PageNavigation } from "./navigation";
import { ErrorMessage, PageHeader } from "./components";
import type {
  Capabilities,
  Permission,
  RoleDetail,
  ScopedPermission,
} from "./types";

import { useDirectory } from "./useDirectory";
import { DirectorySearch, DirectoryNavigation } from "./DirectoryControls";

type RolePageProps = {
  tenant: string;
  revision: number;
  permissions: Permission[];
  capabilities: Capabilities;
  onCreate: () => void;
  onChanged: () => Promise<void>;
};
export function RolesPage(props: RolePageProps) {
  const { segments, navigate } = useConsoleRoute();
  const roleID = segments[1];
  if (!props.capabilities.permissions["iam:role:read"])
    return (
      <p className="quiet-state">
        Your role does not include role read access.
      </p>
    );
  return roleID ? (
    <RoleWorkspace
      key={`${props.tenant}:${roleID}:${segments.slice(2).join("/")}`}
      {...props}
      roleID={roleID}
      onClose={() => navigate("roles")}
    />
  ) : (
    <RoleDirectory {...props} />
  );
}
function RoleDirectory({
  tenant,
  revision,
  capabilities,
  onCreate,
}: RolePageProps) {
  const { navigate } = useConsoleRoute();
  const directory = useDirectory(
    api.roles,
    tenant,
    !!capabilities.permissions["iam:role:read"],
    revision,
  );
  if (!capabilities.permissions["iam:role:read"])
    return (
      <p className="quiet-state">
        Your role does not include role read access.
      </p>
    );
  return (
    <>
      <PageHeader
        eyebrow="Policy building blocks"
        title="Roles"
        detail="Manage permission scopes and inspect immutable role history."
        action={
          capabilities.permissions["iam:role:manage"] ? (
            <button className="primary-button" onClick={onCreate}>
              New role
            </button>
          ) : undefined
        }
      />
      <DirectorySearch
        label="Find a role"
        placeholder="Name, description or role ID"
        onSearch={directory.search}
      />
      <DirectoryNavigation directory={directory} label="Roles" />
      {directory.page && !directory.page.items.length ? (
        <p className="quiet-state">No roles match these filters.</p>
      ) : null}
      <section className="role-grid">
        {directory.page?.items.map((role, index) => (
          <article className="role-card" key={role.role_id}>
            <span className="role-index">
              R-{String(index + 1).padStart(2, "0")}
            </span>
            <ShieldCheck size={22} />
            <h2>{role.name}</h2>
            <p>{role.description || "No description has been added yet."}</p>
            <footer>
              <code>{role.role_id}</code>
              <span>v{role.version}</span>
            </footer>
            <button
              className="secondary-button"
              onClick={() =>
                navigate(`roles/${encodeURIComponent(role.role_id)}/details`)
              }
            >
              View role
            </button>
          </article>
        ))}
      </section>
    </>
  );
}
function RoleWorkspace({
  tenant,
  revision,
  roleID,
  permissions,
  capabilities,
  onClose,
  onChanged,
}: RolePageProps & { roleID: string; onClose: () => void }) {
  const { segments, navigate } = useConsoleRoute();
  const tab = segments[2] ?? "details";
  const addingGrant = segments[3] === "new";
  const base = `roles/${encodeURIComponent(roleID)}`;
  const alive = useAlive();
  const [attempt, setAttempt] = useState(0);
  const [detail, setDetail] = useState<RoleDetail | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [permission, setPermission] = useState(permissions[0]?.key ?? "");
  const [scope, setScope] = useState<ScopedPermission["scope"]>("tenant");
  const [constraints, setConstraints] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [retire, setRetire] = useState(false);
  useEffect(() => {
    let active = true;
    api
      .role(tenant, roleID)
      .then((value) => {
        if (active) {
          setDetail(value);
          setName(value.role.name);
          setDescription(value.role.description);
        }
      })
      .catch((cause) => {
        if (active) setError(friendlyError(cause));
      });
    return () => {
      active = false;
    };
  }, [tenant, roleID, attempt]);
  async function change(
    action: () => Promise<unknown>,
    reportError = true,
  ): Promise<string> {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await action();
      if (!alive.current) return "";
      const updated = await api.role(tenant, roleID);
      if (!alive.current) return "";
      setDetail(updated);
      setName(updated.role.name);
      setDescription(updated.role.description);
      await onChanged();
      if (alive.current) setNotice("Role changes saved.");
      return "";
    } catch (cause) {
      const message = friendlyError(cause);
      if (reportError) setError(message);
      if (cause instanceof Error)
        try {
          setDetail(await api.role(tenant, roleID));
        } catch {
          /* Keep the original actionable error. */
        }
      return message;
    } finally {
      setBusy(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!detail || busy) return;
    const roleName = trimRoleName(name);
    const validation = roleDetailsError(roleName, description);
    if (validation) {
      setNotice("");
      setError(validation);
      return;
    }
    await change(() =>
      api.updateRole(tenant, detail.role, roleName, description),
    );
  }
  async function grant(event: FormEvent) {
    event.preventDefault();
    if (detail) {
      const error = await change(() =>
        api.changeGrant(
          tenant,
          detail.role,
          {
            permission,
            scope,
            workspace_roles:
              scope === "workspace"
                ? constraints
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean)
                : [],
          },
          true,
        ),
      );
      if (!error && alive.current) navigate(`${base}/permissions`);
    }
  }
  async function older() {
    if (!detail?.next_version) return;
    setBusy(true);
    setError("");
    try {
      const page = await api.role(tenant, roleID, detail.next_version);
      setDetail((current) =>
        current
          ? {
              ...current,
              versions: [...current.versions, ...page.versions],
              next_version: page.next_version,
            }
          : current,
      );
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (!detail) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteRole(tenant, detail.role);
      await onChanged();
      if (alive.current) onClose();
    } catch (cause) {
      setError(friendlyError(cause));
      setBusy(false);
    }
  }
  const manage = !!capabilities.permissions["iam:role:manage"],
    grants = !!capabilities.permissions["iam:grant:manage"];
  return (
    <>
      <a className="back-link" href="#/roles">
        ← Back to roles
      </a>
      <PageHeader
        eyebrow={`Role · ${roleID}`}
        title={detail?.role.name ?? roleID}
        detail={
          tab === "details"
            ? "Edit the role name and description."
            : tab === "permissions"
              ? "Review scoped permissions or add a permission grant."
              : tab === "members"
                ? "Manage people assigned to this role."
                : tab === "history"
                  ? "Inspect the role's version history."
                  : "Retire an unused role."
        }
      />
      <PageNavigation
        label="Role navigation"
        items={[
          { path: `${base}/details`, label: "Basic details" },
          { path: `${base}/permissions`, label: "Permissions" },
          { path: `${base}/members`, label: "Members" },
          { path: `${base}/history`, label: "History" },
          ...(manage ? [{ path: `${base}/retire`, label: "Retirement" }] : []),
        ]}
      />
      <section className="paper-card admin-form">
        <ErrorMessage message={error} />
        {notice ? <p role="status">{notice}</p> : null}
        {detail ? (
          <>
            <span className="pill">
              Version {detail.role.version} ·{" "}
              {detail.subject_count ?? detail.subjects.length} member bindings
            </span>
            {![
              "details",
              "permissions",
              "members",
              "history",
              "retire",
            ].includes(tab) ? (
              <p className="quiet-state">This role page does not exist.</p>
            ) : null}
            {tab === "details" ? (
              <form className="admin-form unpadded form-page" onSubmit={save}>
                <label>
                  Name
                  <input
                    value={name}
                    disabled={!manage || busy}
                    onChange={(e) => setName(e.target.value)}
                    aria-describedby="edit-role-name-limit"
                    required
                  />
                </label>
                <small id="edit-role-name-limit">Up to 64 characters.</small>
                <label>
                  Description
                  <textarea
                    value={description}
                    disabled={!manage || busy}
                    onChange={(e) => setDescription(e.target.value)}
                    aria-describedby="edit-role-description-limit"
                    rows={2}
                  />
                </label>
                <small id="edit-role-description-limit">Up to 500 characters.</small>
                {manage ? (
                  <div className="form-actions">
                    <a className="secondary-button" href="#/roles">
                      Cancel
                    </a>
                    <button className="primary-button" disabled={busy}>
                      Save role details
                    </button>
                  </div>
                ) : null}
              </form>
            ) : null}
            {tab === "members" ? (
              <RoleMembers
                tenant={tenant}
                revision={revision}
                role={detail.role}
                canRead={!!capabilities.permissions["iam:member:read"]}
                canAssign={!!capabilities.permissions["iam:role:assign"]}
                busy={busy}
                onChange={(member, assign) =>
                  change(
                    () => api.bindRole(tenant, member, detail.role, assign),
                    false,
                  )
                }
              />
            ) : null}
            {tab === "permissions" ? (
              <>
                <h2>
                  {addingGrant ? "Add permission grant" : "Permission grants"}
                </h2>
                {addingGrant && !grants ? (
                  <p className="quiet-state">
                    Your role does not include grant management access.
                  </p>
                ) : null}
                {!addingGrant ? (
                  <>
                    {detail.grants.length ? (
                      detail.grants.map((item) => (
                        <div
                          className="admin-row"
                          key={`${item.permission}-${item.scope}-${item.workspace_roles}`}
                        >
                          <div>
                            <code>{item.permission}</code>
                            <small className="block">
                              {item.scope}
                              {item.workspace_roles?.length
                                ? ` · ${item.workspace_roles.join(", ")}`
                                : ""}
                            </small>
                          </div>
                          {grants ? (
                            <button
                              className="danger-button"
                              disabled={busy}
                              onClick={() =>
                                void change(() =>
                                  api.changeGrant(
                                    tenant,
                                    detail.role,
                                    item,
                                    false,
                                  ),
                                )
                              }
                            >
                              Revoke grant
                            </button>
                          ) : null}
                        </div>
                      ))
                    ) : (
                      <p>No grants.</p>
                    )}
                    {grants ? (
                      <a
                        className="primary-button"
                        href={`#/${base}/permissions/new`}
                      >
                        New permission grant
                      </a>
                    ) : null}
                  </>
                ) : null}
                {grants && addingGrant ? (
                  <form className="admin-form inset-form" onSubmit={grant}>
                    <label>
                      Permission
                      <select
                        aria-label="Permission"
                        value={permission}
                        onChange={(e) => setPermission(e.target.value)}
                      >
                        {permissions.map((p) => (
                          <option key={p.key}>{p.key}</option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Scope
                      <select
                        aria-label="Scope"
                        value={scope}
                        onChange={(e) =>
                          setScope(e.target.value as ScopedPermission["scope"])
                        }
                      >
                        <option value="tenant">Tenant</option>
                        <option value="own">Own resources</option>
                        <option value="workspace">Workspace membership</option>
                      </select>
                    </label>
                    {scope === "workspace" ? (
                      <label>
                        Allowed workspace roles (comma separated; empty means
                        any)
                        <input
                          value={constraints}
                          onChange={(e) => setConstraints(e.target.value)}
                        />
                      </label>
                    ) : null}
                    <p>
                      Tenant actions require a tenant grant. Own and workspace
                      grants apply to resource checks supplied by a host
                      application.
                    </p>
                    <div className="form-actions">
                      <a
                        className="secondary-button"
                        href={`#/${base}/permissions`}
                      >
                        Cancel
                      </a>
                      <button className="primary-button" disabled={busy}>
                        Add permission grant
                      </button>
                    </div>
                  </form>
                ) : null}
              </>
            ) : null}
            {tab === "history" ? (
              <>
                <h2>Version history</h2>
                {detail.versions.map((version) => (
                  <details key={version.version} className="history-item">
                    <summary>
                      v{version.version} · {version.name} ·{" "}
                      {new Date(version.created_at).toLocaleString()}
                    </summary>
                    <p>{version.description}</p>
                    <small>Changed by {version.created_by}</small>
                    <ul>
                      {version.grants.map((g) => (
                        <li
                          key={`${g.permission}-${g.scope}-${g.workspace_roles}`}
                        >
                          <code>{g.permission}</code> · {g.scope}{" "}
                          {g.workspace_roles?.join(", ")}
                        </li>
                      ))}
                    </ul>
                  </details>
                ))}
                {detail.next_version ? (
                  <button
                    className="secondary-button"
                    disabled={busy}
                    onClick={() => void older()}
                  >
                    Older versions
                  </button>
                ) : null}
              </>
            ) : null}
            {manage && tab === "retire" ? (
              <>
                <h2>Retire role</h2>
                <p>
                  Remove all grants and member bindings before retiring a role.
                  Audit and version history are retained.
                </p>
                {retire ? (
                  <div className="button-row">
                    <button
                      className="danger-button"
                      disabled={busy}
                      onClick={() => void remove()}
                    >
                      Confirm retirement
                    </button>
                    <button
                      className="secondary-button"
                      onClick={() => setRetire(false)}
                    >
                      Cancel
                    </button>
                  </div>
                ) : (
                  <button
                    className="danger-button"
                    disabled={
                      busy ||
                      !!detail.grants.length ||
                      !!(detail.subject_count ?? detail.subjects.length)
                    }
                    onClick={() => setRetire(true)}
                  >
                    Retire role
                  </button>
                )}
              </>
            ) : null}
          </>
        ) : error ? (
          <button
            className="secondary-button"
            onClick={() => {
              setError("");
              setAttempt((v) => v + 1);
            }}
          >
            Retry role
          </button>
        ) : (
          <p role="status">Loading role…</p>
        )}
      </section>
    </>
  );
}
