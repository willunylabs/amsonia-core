import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity,
  ArrowRight,
  BookOpenCheck,
  Building2,
  Check,
  ChevronDown,
  ClipboardCheck,
  Fingerprint,
  KeyRound,
  LayoutGrid,
  LogOut,
  Plus,
  ScrollText,
  ShieldCheck,
  Sparkles,
  UsersRound,
  X
} from "lucide-react";
import { api, APIError, getToken } from "./api";
import type { Account, AuditEvent, Member, Permission, Role, Tenant } from "./types";

type Section = "overview" | "members" | "roles" | "permissions" | "audit" | "check";

const sections: Array<{ id: Section; label: string; icon: typeof LayoutGrid }> = [
  { id: "overview", label: "Overview", icon: LayoutGrid },
  { id: "members", label: "Members", icon: UsersRound },
  { id: "roles", label: "Roles", icon: ShieldCheck },
  { id: "permissions", label: "Permissions", icon: KeyRound },
  { id: "audit", label: "Audit trail", icon: ScrollText },
  { id: "check", label: "Policy lab", icon: Fingerprint }
];

function friendlyError(error: unknown): string {
  if (error instanceof APIError) return error.message;
  return "Something went wrong. Please try again.";
}

function Login({ onLogin }: { onLogin: (account: Account) => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const session = await api.login(email, password);
      onLogin(session.account);
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-shell">
      <section className="login-story" aria-label="Product introduction">
        <div className="brand-lockup">
          <div className="brand-mark" aria-hidden="true"><Sparkles size={20} /></div>
          <span>AMSONIA CORE</span>
        </div>
        <div className="story-copy">
          <span className="eyebrow">Open-source Go SaaS foundation</span>
          <h1>Tenant access<br />you can own.</h1>
          <p>Explore multi-tenant authorization, RBAC, PostgreSQL row-level security, and audit history in the Amsonia Core demo.</p>
          <p className="fine-print">
            <a href="https://github.com/willunylabs/amsonia-core" target="_blank" rel="noreferrer">Read the Apache-2.0 source on GitHub</a>
            <span> · </span>
            <a href="https://willuny.xyz/amsonia" target="_blank" rel="noreferrer">See the commercial Amsonia distribution</a>
          </p>
        </div>
        <div className="trust-strip">
          <ShieldCheck size={22} />
          <span>Tenant-first RBAC · PostgreSQL RLS · Apache-2.0</span>
        </div>
      </section>
      <section className="login-panel">
        <form className="login-card" onSubmit={submit}>
          <header>
            <span className="folio">01 / CONSOLE</span>
            <h2>Welcome back</h2>
            <p>Sign in with the administrator created by the operator CLI.</p>
          </header>
          <label>Email<input type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
          <label>Password<input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
          {error ? <p className="form-error" role="alert">{error}</p> : null}
          <button className="primary-button" type="submit" disabled={busy}>
            {busy ? "Signing in…" : "Enter the console"}<ArrowRight size={17} />
          </button>
          <p className="fine-print">No public registration. Accounts enter through the system administrator or a one-time tenant invitation.</p>
        </form>
      </section>
    </main>
  );
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
    <section className="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title" onMouseDown={(event) => event.stopPropagation()}>
      <header><h2 id="modal-title">{title}</h2><button className="icon-button" onClick={onClose} aria-label="Close"><X size={18} /></button></header>
      {children}
    </section>
  </div>;
}

export function App() {
  const [account, setAccount] = useState<Account | null>(null);
  const [checking, setChecking] = useState(Boolean(getToken()));

  useEffect(() => {
    if (!getToken()) return;
    api.me().then(setAccount).catch(() => void api.logout()).finally(() => setChecking(false));
  }, []);

  if (checking) return <div className="boot-screen"><div className="brand-mark"><Sparkles size={20} /></div><span>Opening Amsonia Core…</span></div>;
  if (!account) return <Login onLogin={setAccount} />;
  return <Console account={account} onLogout={() => setAccount(null)} />;
}

function Console({ account, onLogout }: { account: Account; onLogout: () => void }) {
  const [section, setSection] = useState<Section>("overview");
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [tenantID, setTenantID] = useState("");
  const [members, setMembers] = useState<Member[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [modal, setModal] = useState<"tenant" | "role" | null>(null);

  const selectedTenant = useMemo(() => tenants.find((tenant) => tenant.id === tenantID) ?? null, [tenants, tenantID]);

  const loadGlobal = useCallback(async () => {
    setError("");
    try {
      const [tenantItems, permissionItems] = await Promise.all([api.tenants(), api.permissions()]);
      setTenants(tenantItems);
      setPermissions(permissionItems);
      setTenantID((current) => current || tenantItems[0]?.id || "");
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  const loadTenant = useCallback(async (id: string) => {
    if (!id) {
      setMembers([]); setRoles([]); setAudit([]); return;
    }
    setError("");
    try {
      const [memberItems, roleItems, auditItems] = await Promise.all([api.members(id), api.roles(id), api.audit(id)]);
      setMembers(memberItems);
      setRoles(roleItems);
      setAudit(auditItems);
    } catch (cause) {
      setError(friendlyError(cause));
    }
  }, []);

  useEffect(() => { void loadGlobal(); }, [loadGlobal]);
  useEffect(() => { void loadTenant(tenantID); }, [loadTenant, tenantID]);

  async function logout() {
    await api.logout();
    onLogout();
  }

  return <div className="console-shell">
    <aside className="sidebar">
      <div className="brand-lockup"><div className="brand-mark"><Sparkles size={19} /></div><span>AMSONIA</span></div>
      <nav aria-label="Primary navigation">
        <span className="nav-label">Tenant console</span>
        {sections.map((item) => <button key={item.id} className={section === item.id ? "nav-item active" : "nav-item"} onClick={() => setSection(item.id)}><item.icon size={17} />{item.label}</button>)}
      </nav>
      <div className="sidebar-meta">
        <div className="status-line"><span className="status-dot" />Core API connected</div>
        <a href="https://willuny.xyz/amsonia" target="_blank" rel="noreferrer">Complete Amsonia product <ArrowRight size={13} /></a>
      </div>
    </aside>
    <div className="workspace">
      <header className="topbar">
        <button className="tenant-picker" disabled={!tenants.length}>
          <span className="tenant-symbol"><Building2 size={16} /></span>
          <span><small>Current tenant</small>{selectedTenant?.name ?? "No tenant yet"}</span>
          <ChevronDown size={15} />
          {tenants.length > 1 ? <select aria-label="Current tenant" value={tenantID} onChange={(event) => setTenantID(event.target.value)}>{tenants.map((tenant) => <option key={tenant.id} value={tenant.id}>{tenant.name}</option>)}</select> : null}
        </button>
        <div className="account-menu"><span className="avatar">{account.email.slice(0, 1).toUpperCase()}</span><span><strong>{account.email}</strong><small>{account.system_admin ? "System administrator" : "Tenant member"}</small></span><button className="icon-button" onClick={logout} aria-label="Sign out"><LogOut size={17} /></button></div>
      </header>
      <main className="page">
        {error ? <div className="error-banner" role="alert">{error}<button onClick={() => { void loadGlobal(); void loadTenant(tenantID); }}>Retry</button></div> : null}
        {loading ? <div className="loading-card">Loading your authorization workspace…</div> : null}
        {!loading && !selectedTenant ? <EmptyTenant onCreate={() => setModal("tenant")} /> : null}
        {!loading && selectedTenant ? <SectionView section={section} tenant={selectedTenant} account={account} members={members} roles={roles} permissions={permissions} audit={audit} onCreateRole={() => setModal("role")} /> : null}
      </main>
    </div>
    {modal === "tenant" ? <CreateTenantModal onClose={() => setModal(null)} onCreated={async (tenant) => { setModal(null); await loadGlobal(); setTenantID(tenant.id); }} /> : null}
    {modal === "role" && selectedTenant ? <CreateRoleModal tenant={selectedTenant} permissions={permissions} onClose={() => setModal(null)} onCreated={async () => { setModal(null); await loadTenant(selectedTenant.id); }} /> : null}
  </div>;
}

function EmptyTenant({ onCreate }: { onCreate: () => void }) {
  return <section className="empty-state"><div className="empty-orbit"><Building2 size={30} /></div><span className="eyebrow">First workspace</span><h1>Create your first tenant</h1><p>A tenant is the hard security boundary for members, roles, policy, and audit history.</p><button className="primary-button" onClick={onCreate}><Plus size={17} />Create tenant</button></section>;
}

type SectionProps = { section: Section; tenant: Tenant; account: Account; members: Member[]; roles: Role[]; permissions: Permission[]; audit: AuditEvent[]; onCreateRole: () => void };

function SectionView(props: SectionProps) {
  switch (props.section) {
    case "members": return <MembersPage items={props.members} />;
    case "roles": return <RolesPage items={props.roles} onCreate={props.onCreateRole} />;
    case "permissions": return <PermissionsPage items={props.permissions} />;
    case "audit": return <AuditPage items={props.audit} />;
    case "check": return <PolicyLab tenant={props.tenant} account={props.account} permissions={props.permissions} />;
    default: return <Overview tenant={props.tenant} members={props.members} roles={props.roles} permissions={props.permissions} audit={props.audit} />;
  }
}

function PageHeader({ eyebrow, title, detail, action }: { eyebrow: string; title: string; detail: string; action?: React.ReactNode }) {
  return <header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1><p>{detail}</p></div>{action}</header>;
}

function Overview({ tenant, members, roles, permissions, audit }: Omit<SectionProps, "section" | "account" | "onCreateRole">) {
  const latest = audit[0];
  return <>
    <PageHeader eyebrow="Authorization workspace" title={`Good ${new Date().getHours() < 12 ? "morning" : "afternoon"}.`} detail={`${tenant.name} is active. Here is the shape of its access model today.`} />
    <section className="metric-grid">
      <article><span>Members</span><strong>{members.length.toString().padStart(2, "0")}</strong><small>active identities</small></article>
      <article><span>Roles</span><strong>{roles.length.toString().padStart(2, "0")}</strong><small>versioned definitions</small></article>
      <article><span>Catalog</span><strong>{permissions.length.toString().padStart(2, "0")}</strong><small>developer-owned keys</small></article>
      <article className="accent-metric"><span>Isolation</span><strong><ShieldCheck /></strong><small>signed RLS binding</small></article>
    </section>
    <section className="overview-grid">
      <article className="paper-card posture-card"><div className="card-kicker"><Activity size={17} />Security posture</div><h2>Boundaries are explicit.</h2><ul><li><Check size={15} />Tenant ID signed per transaction</li><li><Check size={15} />Immutable role-version history</li><li><Check size={15} />Last-administrator protection</li></ul></article>
      <article className="paper-card latest-card"><div className="card-kicker"><ClipboardCheck size={17} />Latest policy event</div>{latest ? <><h2>{latest.operation.replaceAll(".", " / ")}</h2><p>{latest.target_type} · {latest.target_id}</p><time>{new Date(latest.at).toLocaleString()}</time></> : <><h2>No events yet</h2><p>Your tenant is quiet. Policy changes will appear here.</p></>}</article>
    </section>
  </>;
}

function MembersPage({ items }: { items: Member[] }) {
  return <><PageHeader eyebrow="Identity" title="Members" detail="Every principal with active membership in this tenant boundary." /><div className="table-card"><table><thead><tr><th>Member</th><th>Status</th><th>Joined</th></tr></thead><tbody>{items.map((member) => <tr key={member.account_id}><td><div className="identity-cell"><span className="avatar small">{member.email[0].toUpperCase()}</span><span><strong>{member.email}</strong><small>{member.account_id}</small></span></div></td><td><span className="pill positive">{member.status}</span></td><td>{new Date(member.joined_at).toLocaleDateString()}</td></tr>)}</tbody></table></div></>;
}

function RolesPage({ items, onCreate }: { items: Role[]; onCreate: () => void }) {
  return <><PageHeader eyebrow="Policy building blocks" title="Roles" detail="Human-readable bundles with immutable history behind every change." action={<button className="primary-button compact" onClick={onCreate}><Plus size={16} />New role</button>} /><section className="role-grid">{items.map((role, index) => <article className="role-card" key={role.role_id}><span className="role-index">R-{String(index + 1).padStart(2, "0")}</span><ShieldCheck size={22} /><h2>{role.name}</h2><p>{role.description || "No description has been added yet."}</p><footer><code>{role.role_id}</code><span>v{role.version}</span></footer></article>)}</section></>;
}

function PermissionsPage({ items }: { items: Permission[] }) {
  return <><PageHeader eyebrow="Immutable catalog" title="Permissions" detail="The application owns this vocabulary. Administrators compose it into roles." /><section className="permission-list">{items.map((permission, index) => <article key={permission.key}><span>{String(index + 1).padStart(2, "0")}</span><div><code>{permission.key}</code><p>{permission.description}</p></div><BookOpenCheck size={19} /></article>)}</section></>;
}

function AuditPage({ items }: { items: AuditEvent[] }) {
  return <><PageHeader eyebrow="Append-only evidence" title="Audit trail" detail="A bounded, chronological account of policy administration." /><section className="timeline">{items.length ? items.map((event) => <article key={`${event.operation}-${event.at}-${event.target_id}`}><div className="timeline-dot" /><time>{new Date(event.at).toLocaleString()}</time><div><h2>{event.operation}</h2><p>{event.target_type} · {event.target_id}</p></div><span className={event.outcome === "success" ? "pill positive" : "pill"}>{event.outcome}</span></article>) : <div className="quiet-state">No audit events yet.</div>}</section></>;
}

function PolicyLab({ tenant, account, permissions }: { tenant: Tenant; account: Account; permissions: Permission[] }) {
  const [permission, setPermission] = useState(permissions[0]?.key ?? "");
  const [result, setResult] = useState<{ allowed: boolean; reason: string; effective_scope?: string } | null>(null);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault(); setError(""); setResult(null);
    try {
      setResult(await api.check({ tenant_id: tenant.id, subject_id: account.id, permission, mode: "tenant_action", resource: { tenant_id: tenant.id } }));
    } catch (cause) { setError(friendlyError(cause)); }
  }

  return <><PageHeader eyebrow="Explainable decisions" title="Policy lab" detail="Ask the live authorization kernel one exact, tenant-scoped question." /><section className="lab-layout"><form className="paper-card lab-form" onSubmit={submit}><label>Subject<input value={account.id} disabled /></label><label>Permission<select value={permission} onChange={(event) => setPermission(event.target.value)}>{permissions.map((item) => <option key={item.key}>{item.key}</option>)}</select></label><button className="primary-button" type="submit"><Fingerprint size={17} />Evaluate policy</button>{error ? <p className="form-error">{error}</p> : null}</form><article className={result?.allowed ? "decision-card allow" : "decision-card"}><span className="eyebrow">Decision</span>{result ? <><div className="decision-symbol">{result.allowed ? <Check /> : <X />}</div><h2>{result.allowed ? "Allowed" : "Denied"}</h2><p>{result.reason.replaceAll("_", " ")}</p><code>{result.effective_scope || "no effective scope"}</code></> : <><div className="decision-symbol neutral"><Fingerprint /></div><h2>Awaiting a question</h2><p>The result will include a stable reason and effective scope.</p></>}</article></section></>;
}

function CreateTenantModal({ onClose, onCreated }: { onClose: () => void; onCreated: (tenant: Tenant) => void }) {
  const [name, setName] = useState(""); const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent) { event.preventDefault(); setBusy(true); setError(""); try { onCreated(await api.createTenant(name)); } catch (cause) { setError(friendlyError(cause)); setBusy(false); } }
  return <Modal title="Create a tenant" onClose={onClose}><form className="modal-form" onSubmit={submit}><p>A tenant becomes an isolated policy and membership boundary.</p><label>Name<input autoFocus value={name} onChange={(event) => setName(event.target.value)} maxLength={80} required /></label>{error ? <p className="form-error">{error}</p> : null}<button className="primary-button" disabled={busy}>{busy ? "Creating…" : "Create tenant"}<ArrowRight size={16} /></button></form></Modal>;
}

function CreateRoleModal({ tenant, permissions, onClose, onCreated }: { tenant: Tenant; permissions: Permission[]; onClose: () => void; onCreated: () => void }) {
  const [id, setID] = useState(""); const [name, setName] = useState(""); const [description, setDescription] = useState(""); const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  function toggle(permission: string) { setSelected((current) => current.includes(permission) ? current.filter((item) => item !== permission) : [...current, permission]); }
  async function submit(event: FormEvent) { event.preventDefault(); setBusy(true); setError(""); try { await api.createRole(tenant.id, { id, name, description, permissions: selected }); onCreated(); } catch (cause) { setError(friendlyError(cause)); setBusy(false); } }
  return <Modal title="Create a role" onClose={onClose}><form className="modal-form" onSubmit={submit}><p>Choose a stable ID and the tenant-wide permissions this role delegates. The role and its first snapshot are committed together.</p><label>Stable role ID<input autoFocus value={id} onChange={(event) => setID(event.target.value)} placeholder="role_support" required /></label><label>Name<input value={name} onChange={(event) => setName(event.target.value)} placeholder="Support specialist" required /></label><label>Description<textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} /></label><fieldset className="permission-picker"><legend>Initial permissions</legend>{permissions.map((permission) => <label className="permission-choice" key={permission.key}><input type="checkbox" checked={selected.includes(permission.key)} onChange={() => toggle(permission.key)} /><span><code>{permission.key}</code><small>{permission.description}</small></span></label>)}</fieldset>{error ? <p className="form-error">{error}</p> : null}<button className="primary-button" disabled={busy}>{busy ? "Creating…" : "Create role"}<ArrowRight size={16} /></button></form></Modal>;
}
