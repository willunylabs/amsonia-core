import { friendlyError } from "./errors";
import { useEffect, useState, type FormEvent } from "react";
import { ShieldCheck } from "lucide-react";
import { api } from "./api";
import { ErrorMessage, Modal, PageHeader } from "./components";
import type {
  Capabilities,
  Permission,
  Role,
  RoleDetail,
  ScopedPermission,
} from "./types";

import { useDirectory } from "./useDirectory";
import { DirectorySearch, DirectoryNavigation } from "./DirectoryControls";

export function RolesPage({
  tenant,
  revision,
  permissions,
  capabilities,
  onCreate,
  onChanged,
}: {
  tenant: string;
  revision: number;
  permissions: Permission[];
  capabilities: Capabilities;
  onCreate: () => void;
  onChanged: () => Promise<void>;
}) {
  const directory = useDirectory(
    api.roles,
    tenant,
    !!capabilities.permissions["iam:role:read"],
    revision,
  );
  const [selected, setSelected] = useState<Role | null>(null);
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
              onClick={() => setSelected(role)}
            >
              View role
            </button>
          </article>
        ))}
      </section>
      {selected ? (
        <RoleModal
          tenant={tenant}
          role={selected}
          permissions={permissions}
          capabilities={capabilities}
          onClose={() => setSelected(null)}
          onChanged={onChanged}
        />
      ) : null}
    </>
  );
}
function RoleModal({
  tenant,
  role,
  permissions,
  capabilities,
  onClose,
  onChanged,
}: {
  tenant: string;
  role: Role;
  permissions: Permission[];
  capabilities: Capabilities;
  onClose: () => void;
  onChanged: () => Promise<void>;
}) {
  const [detail, setDetail] = useState<RoleDetail | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [permission, setPermission] = useState(permissions[0]?.key ?? "");
  const [scope, setScope] = useState<ScopedPermission["scope"]>("tenant");
  const [constraints, setConstraints] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [retire, setRetire] = useState(false);
  useEffect(() => {
    let active = true;
    api
      .role(tenant, role.role_id)
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
  }, [tenant, role.role_id]);
  async function change(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
      const updated = await api.role(tenant, role.role_id);
      setDetail(updated);
      setName(updated.role.name);
      setDescription(updated.role.description);
      await onChanged();
    } catch (cause) {
      setError(friendlyError(cause));
      if (cause instanceof Error)
        try {
          setDetail(await api.role(tenant, role.role_id));
        } catch {
          /* Keep the original actionable error. */
        }
    } finally {
      setBusy(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (detail)
      await change(() =>
        api.updateRole(tenant, detail.role, name, description),
      );
  }
  async function grant(event: FormEvent) {
    event.preventDefault();
    if (detail)
      await change(() =>
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
  }
  async function older() {
    if (!detail?.next_version) return;
    setBusy(true);
    setError("");
    try {
      const page = await api.role(tenant, role.role_id, detail.next_version);
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
      onClose();
    } catch (cause) {
      setError(friendlyError(cause));
      setBusy(false);
    }
  }
  const manage = !!capabilities.permissions["iam:role:manage"],
    grants = !!capabilities.permissions["iam:grant:manage"];
  return (
    <Modal title={`Role · ${role.role_id}`} onClose={onClose}>
      <div className="admin-form">
        <ErrorMessage message={error} />
        {detail ? (
          <>
            <form className="admin-form unpadded" onSubmit={save}>
              <span className="pill">
                Version {detail.role.version} · {detail.subjects.length} member
                bindings
              </span>
              <label>
                Name
                <input
                  value={name}
                  maxLength={128}
                  disabled={!manage || busy}
                  onChange={(e) => setName(e.target.value)}
                  required
                />
              </label>
              <label>
                Description
                <textarea
                  value={description}
                  disabled={!manage || busy}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={2}
                />
              </label>
              {manage ? (
                <button className="primary-button" disabled={busy}>
                  Save role details
                </button>
              ) : null}
            </form>
            <h3>Permission grants</h3>
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
                          api.changeGrant(tenant, detail.role, item, false),
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
              <form className="admin-form inset-form" onSubmit={grant}>
                <label>
                  Permission
                  <select
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
                    Allowed workspace roles (comma separated; empty means any)
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
                <button className="secondary-button" disabled={busy}>
                  Add permission grant
                </button>
              </form>
            ) : null}
            <h3>Version history</h3>
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
                    <li key={`${g.permission}-${g.scope}-${g.workspace_roles}`}>
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
            {manage ? (
              <>
                <h3>Retire role</h3>
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
                      busy || !!detail.grants.length || !!detail.subjects.length
                    }
                    onClick={() => setRetire(true)}
                  >
                    Retire role
                  </button>
                )}
              </>
            ) : null}
          </>
        ) : (
          <p>Loading role…</p>
        )}
      </div>
    </Modal>
  );
}
