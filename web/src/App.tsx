import { useConsoleRoute, useAlive } from "./routing";
import { friendlyError } from "./errors";
import {
  FormEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
  MailPlus,
  Plus,
  RefreshCw,
  ScrollText,
  Settings,
  ShieldCheck,
  UsersRound,
  X,
} from "lucide-react";
import { api, APIError, subscribeSession } from "./api";
import { COMMERCIAL_URL, SOURCE_URL } from "./config";
import type {
  Account,
  AuditEvent,
  Capabilities,
  Permission,
  Tenant,
} from "./types";

import { PageHeader } from "./components";
import { AccountPage, InvitationLanding } from "./Account";
import { MembersPage } from "./Members";
import { InvitationsPage } from "./Invitations";
import { FormPage } from "./navigation";
import { RolesPage } from "./Roles";
import { AuditWorkspace } from "./Audit";
import { OrganizationPage } from "./Organization";

type Section =
  | "organization"
  | "overview"
  | "members"
  | "roles"
  | "permissions"
  | "audit"
  | "check"
  | "account"
  | "tenants"
  | "invitations";

const sections: Array<{ id: Section; label: string; icon: typeof LayoutGrid }> =
  [
    { id: "overview", label: "Overview", icon: LayoutGrid },
    { id: "members", label: "Members", icon: UsersRound },
    { id: "invitations", label: "Invitations", icon: MailPlus },
    { id: "roles", label: "Roles", icon: ShieldCheck },
    { id: "permissions", label: "Permissions", icon: KeyRound },
    { id: "audit", label: "Audit trail", icon: ScrollText },
    { id: "check", label: "Policy lab", icon: Fingerprint },
  ];

function Login({ onLogin }: { onLogin: (account: Account) => void }) {
  const demoEmail = import.meta.env.VITE_PUBLIC_DEMO_VIEWER_EMAIL?.trim() ?? "";
  const demoPassword = import.meta.env.VITE_PUBLIC_DEMO_VIEWER_PASSWORD ?? "";
  const hasPublicDemo = Boolean(demoEmail && demoPassword);
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
          <img className="brand-mark" src="/amsonia-mark-v2.svg" alt="" />
          <span>AMSONIA CORE</span>
        </div>
        <div className="story-copy">
          <span className="eyebrow">Open-source Go SaaS foundation</span>
          <h1>
            Organization access
            <br />
            you can own.
          </h1>
          <p>
            Manage accounts, roles, permissions, and audit history with the
            Amsonia Core admin scaffold.
          </p>
          <p className="fine-print">
            <a href={SOURCE_URL} target="_blank" rel="noreferrer">
              Read the Apache-2.0 source on GitHub
            </a>
            <span> · </span>
            <a href={COMMERCIAL_URL} target="_blank" rel="noreferrer">
              See Amsonia Source Distribution
            </a>
          </p>
        </div>
        <div className="trust-strip">
          <ShieldCheck size={22} />
          <span>Role-based access · PostgreSQL RLS · Apache-2.0</span>
        </div>
      </section>
      <section className="login-panel">
        <form className="login-card" onSubmit={submit}>
          <header>
            <span className="folio">01 / CONSOLE</span>
            <h2>Welcome back</h2>
            <p>Sign in to your administration workspace.</p>
          </header>
          {hasPublicDemo ? (
            <section
              className="demo-credentials"
              aria-label="Read-only demo credentials"
            >
              <div>
                <span>PUBLIC DEMO</span>
                <strong>Read-only tenant member</strong>
              </div>
              <dl>
                <div>
                  <dt>Email</dt>
                  <dd>
                    <code>{demoEmail}</code>
                  </dd>
                </div>
                <div>
                  <dt>Password</dt>
                  <dd>
                    <code>{demoPassword}</code>
                  </dd>
                </div>
              </dl>
              <button
                type="button"
                onClick={() => {
                  setEmail(demoEmail);
                  setPassword(demoPassword);
                  setError("");
                }}
              >
                Use demo credentials
              </button>
            </section>
          ) : null}
          <label>
            Email
            <input
              type="email"
              autoComplete="username"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </label>
          <label>
            Password
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </label>
          {error ? (
            <p className="form-error" role="alert">
              {error}
            </p>
          ) : null}
          <button className="primary-button" type="submit" disabled={busy}>
            {busy ? "Signing in…" : "Enter the console"}
            <ArrowRight size={17} />
          </button>
          <p className="fine-print">
            The shared demo account can inspect one sample tenant but cannot
            create tenants, roles, grants, or members.
          </p>
        </form>
      </section>
    </main>
  );
}

export function App() {
  const [account, setAccount] = useState<Account | null>(null);
  const [inviteToken, setInviteToken] = useState(
    () => new URLSearchParams(location.hash.slice(1)).get("invite") ?? "",
  );
  const [checking, setChecking] = useState(true);
  const [startupError, setStartupError] = useState("");
  const [sessionNotice, setSessionNotice] = useState("");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const readInvitation = () =>
      setInviteToken(
        new URLSearchParams(location.hash.slice(1)).get("invite") ?? "",
      );
    window.addEventListener("hashchange", readInvitation);
    return () => window.removeEventListener("hashchange", readInvitation);
  }, []);

  useEffect(
    () =>
      subscribeSession((next, expired) => {
        setAccount(next);
        setChecking(false);
        setStartupError("");
        setSessionNotice(
          expired ? "Your session has ended. Sign in again to continue." : "",
        );
      }),
    [],
  );
  useEffect(() => {
    if (inviteToken) {
      setChecking(false);
      return;
    }
    let active = true;
    setChecking(true);
    setStartupError("");
    api
      .restoreSession()
      .then((value) => {
        if (active) setAccount(value);
      })
      .catch((cause) => {
        if (
          active &&
          !(cause instanceof APIError && cause.code === "session_changed")
        )
          setStartupError(friendlyError(cause));
      })
      .finally(() => {
        if (active) setChecking(false);
      });
    return () => {
      active = false;
    };
  }, [inviteToken, attempt]);

  if (inviteToken)
    return (
      <InvitationLanding
        key={inviteToken}
        token={inviteToken}
        onLogin={(value) => {
          setInviteToken("");
          setAccount(value);
        }}
      />
    );
  if (checking)
    return (
      <div className="boot-screen">
        <img className="brand-mark" src="/amsonia-mark-v2.svg" alt="" />
        <span>Opening Amsonia Core…</span>
      </div>
    );
  if (startupError)
    return (
      <main className="invitation-shell">
        <section className="login-card admin-form">
          <h1>Unable to restore your session</h1>
          <p role="alert">{startupError}</p>
          <p>
            Your session has been kept. Retry when the connection is available.
          </p>
          <button
            className="primary-button"
            onClick={() => setAttempt((value) => value + 1)}
          >
            Retry connection
          </button>
        </section>
      </main>
    );
  if (!account)
    return (
      <>
        {sessionNotice ? (
          <p className="session-notice" role="status">
            {sessionNotice}
          </p>
        ) : null}
        <Login
          onLogin={(value) => {
            setSessionNotice("");
            setAccount(value);
          }}
        />
      </>
    );
  return (
    <Console
      key={account.id}
      account={account}
      onLogout={() => setAccount(null)}
    />
  );
}

const TENANT_PREFERENCE = "amsonia.core.tenant.v1:";
function preferredTenant(accountID: string): string {
  try {
    return localStorage.getItem(TENANT_PREFERENCE + accountID) ?? "";
  } catch {
    return "";
  }
}
function rememberTenant(accountID: string, tenant: string): void {
  try {
    if (tenant) localStorage.setItem(TENANT_PREFERENCE + accountID, tenant);
    else localStorage.removeItem(TENANT_PREFERENCE + accountID);
  } catch {
    /* Optional preference, never an authorization source. */
  }
}

function Console({
  account,
  onLogout,
}: {
  account: Account;
  onLogout: () => void;
}) {
  const { segments, navigate } = useConsoleRoute();
  const section = (segments[0] ?? "overview") as Section;
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [application, setApplication] = useState<{
    tenancy_mode: "single" | "multi";
  } | null>(null);
  const multiTenant = application?.tenancy_mode === "multi";
  const [tenantID, setTenantID] = useState(() => preferredTenant(account.id));
  const [members, setMembers] = useState<number | null>(null);
  const [roles, setRoles] = useState<number | null>(null);
  const [revision, setRevision] = useState(0);
  const [permissions, setPermissions] = useState<Permission[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [capabilities, setCapabilities] = useState<Capabilities>({
    permissions: {},
  });
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const tenantRequest = useRef(0);
  const globalRequest = useRef(0);
  const tenantRef = useRef(tenantID);
  const refreshInFlight = useRef<Promise<void> | null>(null);
  const [loadedTenantID, setLoadedTenantID] = useState("");
  const [tenantLoading, setTenantLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const selectedTenant = useMemo(
    () => tenants.find((tenant) => tenant.id === tenantID) ?? null,
    [tenants, tenantID],
  );

  const selectTenant = useCallback(
    (id: string) => {
      if (id === tenantRef.current) return;
      ++tenantRequest.current;
      const root = location.hash.slice(2).split("/")[0];
      if (
        ["members", "roles", "invitations"].includes(root) &&
        location.hash.slice(2).includes("/")
      )
        navigate(root);
      tenantRef.current = id;
      setTenantID(id);

      setLoadedTenantID("");
      rememberTenant(account.id, id);
    },
    [account.id, navigate],
  );

  const loadGlobal = useCallback(async (): Promise<string | null> => {
    const serial = ++globalRequest.current;
    try {
      const [tenantItems, permissionItems, applicationOptions] =
        await Promise.all([
          api.tenants(),
          api.permissions(),
          api.application(),
        ]);
      if (serial !== globalRequest.current) return null;
      const current = tenantRef.current;
      const preferred = preferredTenant(account.id);
      const next =
        tenantItems.find((tenant) => tenant.id === current)?.id ??
        tenantItems.find((tenant) => tenant.id === preferred)?.id ??
        tenantItems[0]?.id ??
        "";
      setApplication(applicationOptions);
      setTenants(tenantItems);
      setPermissions(permissionItems);
      if (next !== current) selectTenant(next);
      else rememberTenant(account.id, next);
      setError("");
      return next;
    } catch (cause) {
      if (serial === globalRequest.current) setError(friendlyError(cause));
      return null;
    } finally {
      if (serial === globalRequest.current) setLoading(false);
    }
  }, [account.id, selectTenant]);

  const loadTenant = useCallback(
    async (id: string, refresh = false, retryAccess = true): Promise<void> => {
      const serial = ++tenantRequest.current;
      const clear = () => {
        setMembers(null);
        setRoles(null);
        setAudit([]);
        setCapabilities({ permissions: {} });
        setLoadedTenantID("");
      };
      if (!refresh) clear();
      if (!id) {
        setTenantLoading(false);
        return;
      }
      if (!refresh) setTenantLoading(true);
      setError("");
      try {
        const capabilityItems = await api.capabilities(id);
        if (serial !== tenantRequest.current || id !== tenantRef.current)
          return;
        const allowed = capabilityItems.permissions;
        const [memberItems, roleItems, auditItems] = await Promise.all([
          allowed["iam:member:read"]
            ? api.members(id, { status: "active", limit: 1 })
            : Promise.resolve(null),
          allowed["iam:role:read"]
            ? api.roles(id, { limit: 1 })
            : Promise.resolve(null),
          allowed["iam:audit:read"] ? api.audit(id) : Promise.resolve([]),
        ]);
        if (serial !== tenantRequest.current || id !== tenantRef.current)
          return;
        setMembers(memberItems?.total ?? null);
        setRoles(roleItems?.total ?? null);
        setRevision((value) => value + 1);
        setAudit(auditItems);
        setCapabilities(capabilityItems);
        setLoadedTenantID(id);
      } catch (cause) {
        if (serial !== tenantRequest.current || id !== tenantRef.current)
          return;
        if (
          cause instanceof APIError &&
          (cause.status === 403 || cause.status === 404)
        ) {
          clear();

          if (retryAccess) {
            const next = await loadGlobal();
            if (next === id && tenantRef.current === id)
              await loadTenant(id, false, false);
            return;
          }
        }
        setError(friendlyError(cause));
      } finally {
        if (serial === tenantRequest.current) setTenantLoading(false);
      }
    },
    [loadGlobal],
  );

  const refreshWorkspace = useCallback((): Promise<void> => {
    if (!refreshInFlight.current) {
      setRefreshing(true);
      const before = tenantRef.current;
      refreshInFlight.current = (async () => {
        const id = await loadGlobal();
        if (id !== null && id === before && id === tenantRef.current)
          await loadTenant(id, true);
      })().finally(() => {
        refreshInFlight.current = null;
        setRefreshing(false);
      });
    }
    return refreshInFlight.current;
  }, [loadGlobal, loadTenant]);

  useEffect(() => {
    void loadGlobal();
  }, [loadGlobal]);
  useEffect(() => {
    void loadTenant(selectedTenant?.id ?? "");
  }, [loadTenant, selectedTenant?.id]);
  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState === "visible") void refreshWorkspace();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [refreshWorkspace]);

  async function logout() {
    setSigningOut(true);
    setError("");
    try {
      await api.logout();
      onLogout();
    } catch (cause) {
      setError(friendlyError(cause));
    } finally {
      setSigningOut(false);
    }
  }

  return (
    <div className="console-shell">
      <aside className="sidebar">
        <div className="brand-lockup">
          <img className="brand-mark" src="/amsonia-mark-v2.svg" alt="" />
          <span>AMSONIA</span>
        </div>
        <nav aria-label="Primary navigation">
          <span className="nav-label">
            {multiTenant ? "Tenant console" : "Organization console"}
          </span>
          {sections.map((item) => (
            <button
              key={item.id}
              className={section === item.id ? "nav-item active" : "nav-item"}
              aria-label={item.label}
              aria-current={section === item.id ? "page" : undefined}
              onClick={() => navigate(item.id)}
            >
              <item.icon size={17} />
              <span className="nav-text">{item.label}</span>
              <span className="nav-short" aria-hidden="true">
                {
                  {
                    organization: "Settings",
                    overview: "Home",
                    members: "Members",
                    invitations: "Invites",
                    roles: "Roles",
                    permissions: "Access",
                    audit: "Audit",
                    check: "Policy",
                    account: "Account",
                    tenants: "Tenants",
                  }[item.id]
                }
              </span>
            </button>
          ))}
        </nav>
        <div className="sidebar-meta">
          <div className="status-line">
            <span className="status-dot" />
            Core API connected
          </div>
          <a href={COMMERCIAL_URL} target="_blank" rel="noreferrer">
            Amsonia Source Distribution <ArrowRight size={13} />
          </a>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="tenant-switcher">
            <div className="tenant-picker">
              <span className="tenant-symbol">
                <Building2 size={16} />
              </span>
              <span>
                <small>{multiTenant ? "Current tenant" : "Organization"}</small>
                {selectedTenant?.name ??
                  (multiTenant ? "No tenant yet" : "No organization access")}
              </span>
              {multiTenant ? <ChevronDown size={15} /> : null}
              {multiTenant && tenants.length > 1 ? (
                <select
                  aria-label="Current tenant"
                  value={tenantID}
                  onChange={(event) => selectTenant(event.target.value)}
                >
                  {tenants.map((tenant) => (
                    <option key={tenant.id} value={tenant.id}>
                      {tenant.name}
                    </option>
                  ))}
                </select>
              ) : null}
            </div>
            {selectedTenant ? (
              <button
                className="icon-button"
                aria-label="Organization settings"
                aria-current={section === "organization" ? "page" : undefined}
                onClick={() => navigate("organization")}
              >
                <Settings size={17} />
              </button>
            ) : null}
            {multiTenant && account.system_admin && tenants.length > 0 ? (
              <button
                className="icon-button"
                aria-label="Create tenant"
                onClick={() => navigate("tenants/new")}
              >
                <Plus size={17} />
              </button>
            ) : null}
          </div>
          <div className="account-menu">
            <button
              className="icon-button"
              aria-label="Refresh workspace"
              disabled={refreshing}
              onClick={() => void refreshWorkspace()}
            >
              <RefreshCw size={17} />
            </button>
            <button
              className="icon-button"
              aria-label="Account settings"
              onClick={() => navigate("account/profile")}
            >
              <UsersRound size={18} />
            </button>
            <span className="avatar">
              {account.email.slice(0, 1).toUpperCase()}
            </span>
            <span>
              <strong>{account.email}</strong>
              <small>
                {account.system_admin
                  ? "System administrator"
                  : multiTenant
                    ? "Tenant member"
                    : "Organization member"}
              </small>
            </span>
            <button
              className="icon-button"
              onClick={() => void logout()}
              disabled={signingOut}
              aria-label="Sign out"
            >
              <LogOut size={17} />
            </button>
          </div>
        </header>
        <main className="page">
          {error ? (
            <div className="error-banner" role="alert">
              {error}
              <button
                onClick={() => {
                  void refreshWorkspace();
                }}
              >
                Retry
              </button>
            </div>
          ) : null}
          {loading ? (
            <div className="loading-card">
              Loading your authorization workspace…
            </div>
          ) : null}
          {section === "account" ? (
            <AccountPage onLogout={onLogout} email={account.email} />
          ) : section === "tenants" && segments[1] === "new" ? (
            multiTenant && account.system_admin ? (
              <CreateTenantForm
                onClose={() => navigate("overview")}
                onCreated={async (tenant) => {
                  await loadGlobal();
                  selectTenant(tenant.id);
                  navigate("overview");
                }}
              />
            ) : (
              <p className="quiet-state">
                {multiTenant
                  ? "Only system administrators can create tenants."
                  : "This deployment uses one organization. Tenant creation is unavailable."}
              </p>
            )
          ) : (
            <>
              {!loading && !selectedTenant ? (
                <EmptyTenant
                  multiTenant={multiTenant}
                  canCreate={multiTenant && account.system_admin}
                  onCreate={() => navigate("tenants/new")}
                />
              ) : null}
              {tenantLoading ? <p role="status">Loading access…</p> : null}
              {!loading &&
              !tenantLoading &&
              selectedTenant &&
              loadedTenantID === tenantID ? (
                section === "organization" ? (
                  <OrganizationPage
                    key={selectedTenant.id}
                    tenant={selectedTenant.id}
                    onChanged={refreshWorkspace}
                  />
                ) : (
                  <SectionView
                    key={selectedTenant.id}
                    section={section}
                    tenant={selectedTenant}
                    account={account}
                    revision={revision}
                    members={members}
                    roles={roles}
                    permissions={permissions}
                    audit={audit}
                    capabilities={capabilities}
                    onChanged={refreshWorkspace}
                    onCreateRole={() => navigate("roles/new")}
                  />
                )
              ) : null}
            </>
          )}
        </main>
      </div>
    </div>
  );
}

function EmptyTenant({
  multiTenant,
  canCreate,
  onCreate,
}: {
  multiTenant: boolean;
  canCreate: boolean;
  onCreate: () => void;
}) {
  return (
    <section className="empty-state">
      <div className="empty-orbit">
        <Building2 size={30} />
      </div>
      <span className="eyebrow">
        {multiTenant ? "Tenant workspace" : "Organization workspace"}
      </span>
      <h1>
        {canCreate
          ? "Create your first tenant"
          : multiTenant
            ? "No tenant assigned"
            : "No organization access"}
      </h1>
      <p>
        {canCreate
          ? "A tenant is the hard security boundary for members, roles, policy, and audit history."
          : multiTenant
            ? "Ask your tenant administrator for an invitation link."
            : "Ask your administrator for an organization invitation. For a new installation, the deployment administrator must finish organization setup."}
      </p>
      {canCreate ? (
        <button className="primary-button" onClick={onCreate}>
          <Plus size={17} />
          Create tenant
        </button>
      ) : null}
    </section>
  );
}

type SectionProps = {
  section: Section;
  tenant: Tenant;
  account: Account;
  members: number | null;
  roles: number | null;
  revision: number;
  permissions: Permission[];
  audit: AuditEvent[];
  capabilities: Capabilities;
  onCreateRole: () => void;
  onChanged: () => Promise<void>;
};

function SectionView(props: SectionProps) {
  const { segments, navigate } = useConsoleRoute();
  switch (props.section) {
    case "members":
      return (
        <MembersPage
          tenant={props.tenant.id}
          revision={props.revision}
          capabilities={props.capabilities}
          onChanged={props.onChanged}
        />
      );
    case "invitations":
      return (
        <InvitationsPage
          tenant={props.tenant.id}
          revision={props.revision}
          capabilities={props.capabilities}
          onChanged={props.onChanged}
        />
      );
    case "roles":
      if (segments[1] === "new" && segments.length === 2)
        return props.capabilities.permissions["iam:role:manage"] ? (
          <CreateRoleForm
            tenant={props.tenant}
            permissions={props.permissions}
            onClose={() => navigate("roles")}
            onCreated={async () => {
              await props.onChanged();
              navigate("roles");
            }}
          />
        ) : (
          <p className="quiet-state">
            Your role does not include role management access.
          </p>
        );
      return (
        <RolesPage
          tenant={props.tenant.id}
          revision={props.revision}
          permissions={props.permissions}
          capabilities={props.capabilities}
          onChanged={props.onChanged}
          onCreate={props.onCreateRole}
        />
      );
    case "permissions":
      return <PermissionsPage items={props.permissions} />;
    case "audit":
      return (
        <AuditWorkspace
          tenant={props.tenant.id}
          allowed={!!props.capabilities.permissions["iam:audit:read"]}
        />
      );
    case "check":
      return (
        <PolicyLab
          tenant={props.tenant}
          account={props.account}
          permissions={props.permissions}
        />
      );
    default:
      if (props.section !== "overview")
        return (
          <>
            <PageHeader
              eyebrow="Navigation"
              title="Page not found"
              detail="Choose a page from the navigation."
            />
            <a className="secondary-button" href="#/overview">
              Back to overview
            </a>
          </>
        );
      return (
        <Overview
          tenant={props.tenant}
          members={props.members}
          roles={props.roles}
          permissions={props.permissions}
          audit={props.audit}
        />
      );
  }
}

function Overview({
  tenant,
  members,
  roles,
  permissions,
  audit,
}: Omit<
  SectionProps,
  | "section"
  | "account"
  | "capabilities"
  | "onCreateRole"
  | "onChanged"
  | "revision"
>) {
  const latest = audit[0];
  return (
    <>
      <PageHeader
        eyebrow="Authorization workspace"
        title={`Good ${new Date().getHours() < 12 ? "morning" : "afternoon"}.`}
        detail={`${tenant.name} is active. Here is the shape of its access model today.`}
      />
      <section className="metric-grid">
        <article>
          <span>Members</span>
          <strong>
            {members === null ? "—" : members.toString().padStart(2, "0")}
          </strong>
          <small>active memberships</small>
        </article>
        <article>
          <span>Roles</span>
          <strong>
            {roles === null ? "—" : roles.toString().padStart(2, "0")}
          </strong>
          <small>versioned definitions</small>
        </article>
        <article>
          <span>Catalog</span>
          <strong>{permissions.length.toString().padStart(2, "0")}</strong>
          <small>developer-owned keys</small>
        </article>
        <article className="accent-metric">
          <span>Isolation</span>
          <strong>
            <ShieldCheck />
          </strong>
          <small>signed RLS binding</small>
        </article>
      </section>
      <section className="overview-grid">
        <article className="paper-card posture-card">
          <div className="card-kicker">
            <Activity size={17} />
            Security posture
          </div>
          <h2>Boundaries are explicit.</h2>
          <ul>
            <li>
              <Check size={15} />
              Tenant ID signed per transaction
            </li>
            <li>
              <Check size={15} />
              Immutable role-version history
            </li>
            <li>
              <Check size={15} />
              Last-administrator protection
            </li>
          </ul>
        </article>
        <article className="paper-card latest-card">
          <div className="card-kicker">
            <ClipboardCheck size={17} />
            Latest policy event
          </div>
          {latest ? (
            <>
              <h2>{latest.operation.replaceAll(".", " / ")}</h2>
              <p>
                {latest.target_type} · {latest.target_id}
              </p>
              <time>{new Date(latest.at).toLocaleString()}</time>
            </>
          ) : (
            <>
              <h2>No events yet</h2>
              <p>No recent activity. Policy changes will appear here.</p>
            </>
          )}
        </article>
      </section>
    </>
  );
}

function PermissionsPage({ items }: { items: Permission[] }) {
  return (
    <>
      <PageHeader
        eyebrow="Immutable catalog"
        title="Permissions"
        detail="The application owns this vocabulary. Administrators compose it into roles."
      />
      <section className="permission-list">
        {items.map((permission, index) => (
          <article key={permission.key}>
            <span>{String(index + 1).padStart(2, "0")}</span>
            <div>
              <code>{permission.key}</code>
              <p>{permission.description}</p>
            </div>
            <BookOpenCheck size={19} />
          </article>
        ))}
      </section>
    </>
  );
}

function PolicyLab({
  tenant,
  account,
  permissions,
}: {
  tenant: Tenant;
  account: Account;
  permissions: Permission[];
}) {
  const [permission, setPermission] = useState(permissions[0]?.key ?? "");
  const [result, setResult] = useState<{
    allowed: boolean;
    reason: string;
    effective_scope?: string;
  } | null>(null);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setResult(null);
    try {
      setResult(
        await api.check({
          tenant_id: tenant.id,
          subject_id: account.id,
          permission,
          mode: "tenant_action",
          resource: { tenant_id: tenant.id },
        }),
      );
    } catch (cause) {
      setError(friendlyError(cause));
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Explainable decisions"
        title="Policy lab"
        detail="Ask the live authorization kernel one exact, tenant-scoped question."
      />
      <section className="lab-layout">
        <form className="paper-card lab-form" onSubmit={submit}>
          <label>
            Subject
            <input value={account.id} disabled />
          </label>
          <label>
            Permission
            <select
              value={permission}
              onChange={(event) => setPermission(event.target.value)}
            >
              {permissions.map((item) => (
                <option key={item.key}>{item.key}</option>
              ))}
            </select>
          </label>
          <button className="primary-button" type="submit">
            <Fingerprint size={17} />
            Evaluate policy
          </button>
          {error ? <p className="form-error">{error}</p> : null}
        </form>
        <article
          className={result?.allowed ? "decision-card allow" : "decision-card"}
        >
          <span className="eyebrow">Decision</span>
          {result ? (
            <>
              <div className="decision-symbol">
                {result.allowed ? <Check /> : <X />}
              </div>
              <h2>{result.allowed ? "Allowed" : "Denied"}</h2>
              <p>{result.reason.replaceAll("_", " ")}</p>
              <code>{result.effective_scope || "no effective scope"}</code>
            </>
          ) : (
            <>
              <div className="decision-symbol neutral">
                <Fingerprint />
              </div>
              <h2>Awaiting a question</h2>
              <p>
                The result will include a stable reason and effective scope.
              </p>
            </>
          )}
        </article>
      </section>
    </>
  );
}

function CreateTenantForm({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (tenant: Tenant) => void;
}) {
  const alive = useAlive();
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const tenant = await api.createTenant(name);
      if (alive.current) onCreated(tenant);
    } catch (cause) {
      setError(friendlyError(cause));
      setBusy(false);
    }
  }
  return (
    <FormPage
      title="Create tenant"
      detail="Create a tenant and its first administrator membership."
      back={{ path: "overview", label: "Back to overview" }}
    >
      <form className="admin-form" onSubmit={submit}>
        <p>A tenant becomes an isolated policy and membership boundary.</p>
        <label>
          Name
          <input
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={80}
            required
          />
        </label>
        {error ? <p className="form-error">{error}</p> : null}
        <div className="form-actions">
          <button
            type="button"
            className="secondary-button"
            disabled={busy}
            onClick={onClose}
          >
            Cancel
          </button>
          <button className="primary-button" disabled={busy}>
            {busy ? "Creating…" : "Create tenant"}
            <ArrowRight size={16} />
          </button>
        </div>
      </form>
    </FormPage>
  );
}

function CreateRoleForm({
  tenant,
  permissions,
  onClose,
  onCreated,
}: {
  tenant: Tenant;
  permissions: Permission[];
  onClose: () => void;
  onCreated: () => void;
}) {
  const alive = useAlive();
  const [id, setID] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  function toggle(permission: string) {
    setSelected((current) =>
      current.includes(permission)
        ? current.filter((item) => item !== permission)
        : [...current, permission],
    );
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.createRole(tenant.id, {
        id,
        name,
        description,
        permissions: selected,
      });
      if (alive.current) onCreated();
    } catch (cause) {
      setError(friendlyError(cause));
      setBusy(false);
    }
  }
  return (
    <FormPage
      title="Create role"
      detail="Define the role identity and its initial permissions."
      back={{ path: "roles", label: "Back to roles" }}
    >
      <form className="admin-form" onSubmit={submit}>
        <p>
          Choose a stable ID and the tenant-wide permissions this role
          delegates. You can refine scoped grants after creating the role.
        </p>
        <label>
          Stable role ID
          <input
            autoFocus
            value={id}
            onChange={(event) => setID(event.target.value)}
            placeholder="role_support"
            maxLength={256}
            required
          />
        </label>
        <label>
          Name
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Support specialist"
            maxLength={128}
            required
          />
        </label>
        <label>
          Description
          <textarea
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            rows={3}
          />
        </label>
        <fieldset className="permission-picker">
          <legend>Initial permissions</legend>
          {permissions.map((permission) => (
            <label className="permission-choice" key={permission.key}>
              <input
                type="checkbox"
                checked={selected.includes(permission.key)}
                onChange={() => toggle(permission.key)}
              />
              <span>
                <code>{permission.key}</code>
                <small>{permission.description}</small>
              </span>
            </label>
          ))}
        </fieldset>
        {error ? <p className="form-error">{error}</p> : null}
        <div className="form-actions">
          <button
            type="button"
            className="secondary-button"
            disabled={busy}
            onClick={onClose}
          >
            Cancel
          </button>
          <button className="primary-button" disabled={busy}>
            {busy ? "Creating…" : "Create role"}
            <ArrowRight size={16} />
          </button>
        </div>
      </form>
    </FormPage>
  );
}
