import type {
  Account,
  DirectoryFilter,
  DirectoryPage,
  Profile,
  AccountSession,
  Invitation,
  InvitationPreview,
  RoleDetail,
  ScopedPermission,
  AuditPage,
  AuditFilter,
  Capabilities,
  Member,
  Permission,
  Role,
  Session,
  Tenant,
} from "./types";

function directoryQuery(
  filter: DirectoryFilter & { assignment?: string },
): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filter))
    if (value !== undefined && value !== "") params.set(key, String(value));
  return params.size ? `?${params}` : "";
}

function auditQuery(filter: AuditFilter): string {
  return new URLSearchParams(
    Object.entries(filter)
      .filter(([, value]) => value !== undefined && value !== "")
      .map(([key, value]) => [key, String(value)]),
  ).toString();
}

const CONFIGURED_API_BASE = (import.meta.env.VITE_API_URL ?? "").replace(
  /\/$/,
  "",
);
const configuredURL = new URL(
  CONFIGURED_API_BASE || window.location.origin,
  window.location.origin,
);
if (configuredURL.origin !== window.location.origin) {
  throw new Error(
    "VITE_API_URL must resolve to the console origin; proxy API traffic under the same origin.",
  );
}
const API_BASE = CONFIGURED_API_BASE;
const TOKEN_KEY = "amsonia.core.access.v1";
let authEpoch = 0;
let activeAccountID = "";
let authQueue: Promise<unknown> = Promise.resolve();
let refreshInFlight: {
  epoch: number;
  promise: Promise<Session | null>;
} | null = null;
type SessionListener = (account: Account | null, expired: boolean) => void;
const sessionListeners = new Set<SessionListener>();

export function subscribeSession(listener: SessionListener): () => void {
  sessionListeners.add(listener);
  return () => {
    sessionListeners.delete(listener);
  };
}

function notifySession(account: Account | null, expired = false): void {
  for (const listener of sessionListeners) listener(account, expired);
}

// The cookie is shared by tabs; the access token stays in this tab. Queue all
// cookie-changing auth calls. Web Locks extend that ordering to other tabs.
function withAuthLock<T>(action: () => Promise<T>): Promise<T> {
  const run = async (): Promise<T> => {
    if (navigator.locks)
      return await navigator.locks.request(
        `amsonia.core.auth:${API_BASE}`,
        action,
      );
    return action();
  };
  const pending = authQueue.then(run, run);
  authQueue = pending.catch(() => undefined);
  return pending;
}

function sessionChanged(): APIError {
  return new APIError(
    409,
    "session_changed",
    "Your signed-in account changed. Please try again.",
  );
}

function acceptSession(session: Session, epoch: number): Session {
  if (epoch !== authEpoch) throw sessionChanged();
  sessionStorage.setItem(TOKEN_KEY, session.access_token);
  activeAccountID = session.account.id;
  notifySession(session.account);
  return session;
}

function expireSession(epoch: number): void {
  if (epoch !== authEpoch) return;
  authEpoch++;
  activeAccountID = "";
  sessionStorage.removeItem(TOKEN_KEY);
  notifySession(null, true);
}

export class APIError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.code = code;
  }
}

export function getToken(): string {
  return sessionStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  authEpoch++;
  activeAccountID = "";
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else {
    sessionStorage.removeItem(TOKEN_KEY);
    notifySession(null);
  }
}

async function execute<T>(
  path: string,
  init: RequestInit,
  read?: (response: Response) => Promise<T>,
): Promise<T> {
  const token = getToken();
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init.headers,
    },
  });
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string };
    } | null;
    throw new APIError(
      response.status,
      payload?.error?.code ?? "request_failed",
      payload?.error?.message ?? "The request could not be completed.",
    );
  }
  if (response.status === 204) return undefined as T;
  return read ? read(response) : (response.json() as Promise<T>);
}

async function refreshAccess(optional = false): Promise<Session | null> {
  const epoch = authEpoch;
  if (!refreshInFlight || refreshInFlight.epoch !== epoch) {
    const pending = {
      epoch,
      promise: withAuthLock(async () => {
        if (epoch !== authEpoch) throw sessionChanged();
        try {
          const session = await execute<Session | undefined>(
            optional ? "/api/v1/auth/restore" : "/api/v1/auth/refresh",
            { method: "POST" },
          );
          return session ? acceptSession(session, epoch) : null;
        } catch (error) {
          // Offline/service errors are retryable. Only a definitive denial
          // invalidates local credentials and returns the app to sign-in.
          if (error instanceof APIError && error.status === 401)
            expireSession(epoch);
          throw error;
        }
      }),
    };
    refreshInFlight = pending;
    void pending.promise
      .finally(() => {
        if (refreshInFlight === pending) refreshInFlight = null;
      })
      .catch(() => undefined);
  }
  return refreshInFlight.promise;
}

async function request<T>(
  path: string,
  init: RequestInit = {},
  read?: (response: Response) => Promise<T>,
): Promise<T> {
  const epoch = authEpoch;
  const token = getToken();
  const accountID = activeAccountID;
  try {
    return await execute<T>(path, init, read);
  } catch (error) {
    const canRefresh =
      error instanceof APIError &&
      error.status === 401 &&
      !path.startsWith("/api/v1/invitations/") &&
      path !== "/api/v1/auth/login" &&
      path !== "/api/v1/auth/refresh" &&
      path !== "/api/v1/auth/restore" &&
      path !== "/api/v1/auth/logout";
    if (!canRefresh || epoch !== authEpoch) throw error;
    // A late 401 may belong to the old token, after another request refreshed.
    if (!getToken() || token === getToken()) {
      if (!(await refreshAccess())) {
        expireSession(epoch);
        throw error;
      }
    }
    if (epoch !== authEpoch || (accountID && accountID !== activeAccountID))
      throw sessionChanged();
    try {
      return await execute<T>(path, init, read);
    } catch (retryError) {
      if (retryError instanceof APIError && retryError.status === 401)
        expireSession(epoch);
      throw retryError;
    }
  }
}

export const api = {
  async login(email: string, password: string): Promise<Session> {
    const epoch = ++authEpoch;
    return withAuthLock(async () =>
      acceptSession(
        await execute<Session>("/api/v1/auth/login", {
          method: "POST",
          body: JSON.stringify({ email, password }),
        }),
        epoch,
      ),
    );
  },
  async restoreSession(): Promise<Account | null> {
    const epoch = authEpoch;
    try {
      const account = getToken()
        ? await request<Account>("/api/v1/auth/me")
        : ((await refreshAccess(true))?.account ?? null);
      if (epoch !== authEpoch) throw sessionChanged();
      activeAccountID = account?.id ?? "";
      return account;
    } catch (error) {
      if (error instanceof APIError && error.status === 401) return null;
      throw error;
    }
  },
  profile: () => request<Profile>("/api/v1/auth/profile"),
  updateProfile: (display_name: string) =>
    request<Profile>("/api/v1/auth/profile", {
      method: "PATCH",
      body: JSON.stringify({ display_name }),
    }),
  changePassword: (current_password: string, new_password: string) =>
    request<void>("/api/v1/auth/password", {
      method: "POST",
      body: JSON.stringify({ current_password, new_password }),
    }),
  sessions: async () =>
    (await request<{ items: AccountSession[] }>("/api/v1/auth/sessions")).items,
  revokeSession: (id: string) =>
    request<void>(`/api/v1/auth/sessions/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  revokeOtherSessions: () =>
    request<void>("/api/v1/auth/sessions/revoke-others", {
      method: "POST",
      body: "{}",
    }),
  member: (tenant: string, id: string) =>
    request<Member>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/members/${encodeURIComponent(id)}`,
    ),
  previewInvitation: (token: string) =>
    request<InvitationPreview>("/api/v1/invitations/preview", {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  acceptInvitation: (token: string, password: string) =>
    request<Account>("/api/v1/invitations/accept", {
      method: "POST",
      body: JSON.stringify({ token, password }),
    }),
  invitations: (tenant: string, filter: DirectoryFilter = {}) =>
    request<DirectoryPage<Invitation>>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/invitations${directoryQuery(filter)}`,
    ),
  createInvitation: (tenant: string, email: string) =>
    request<Invitation & { token: string }>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/invitations`,
      { method: "POST", body: JSON.stringify({ email }) },
    ),
  revokeInvitation: (tenant: string, id: string) =>
    request<void>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/invitations/${encodeURIComponent(id)}`,
      { method: "DELETE" },
    ),
  manageMember: (
    tenant: string,
    id: string,
    status: "active" | "suspended" | "remove",
  ) =>
    request<void>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/members/${encodeURIComponent(id)}`,
      {
        method: status === "remove" ? "DELETE" : "PATCH",
        ...(status === "remove" ? {} : { body: JSON.stringify({ status }) }),
      },
    ),
  memberRoles: async (tenant: string, id: string) =>
    (
      await request<{ items: string[] }>(
        `/api/v1/tenants/${encodeURIComponent(tenant)}/members/${encodeURIComponent(id)}/roles`,
      )
    ).items,
  roleMembers: (
    tenant: string,
    id: string,
    assignment: "assigned" | "available",
    filter: DirectoryFilter = {},
  ) =>
    request<DirectoryPage<Member>>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles/${encodeURIComponent(id)}/members${directoryQuery({ ...filter, assignment })}`,
    ),
  bindRole: (tenant: string, member: string, role: Role, assign: boolean) =>
    request<void>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/members/${encodeURIComponent(member)}/roles/${encodeURIComponent(role.role_id)}`,
      {
        method: assign ? "PUT" : "DELETE",
        body: JSON.stringify({ expected_version: role.version }),
      },
    ),
  role: (tenant: string, id: string, before?: number) =>
    request<RoleDetail>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles/${encodeURIComponent(id)}?summary=true${before ? `&before_version=${before}` : ""}`,
    ),
  updateRole: (tenant: string, role: Role, name: string, description: string) =>
    request<Role>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles/${encodeURIComponent(role.role_id)}`,
      {
        method: "PATCH",
        body: JSON.stringify({
          name,
          description,
          expected_version: role.version,
        }),
      },
    ),
  deleteRole: (tenant: string, role: Role) =>
    request<void>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles/${encodeURIComponent(role.role_id)}`,
      {
        method: "DELETE",
        body: JSON.stringify({ expected_version: role.version }),
      },
    ),
  changeGrant: (
    tenant: string,
    role: Role,
    grant: ScopedPermission,
    add: boolean,
  ) =>
    request<void>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles/${encodeURIComponent(role.role_id)}/grants`,
      {
        method: add ? "POST" : "DELETE",
        body: JSON.stringify({ ...grant, expected_version: role.version }),
      },
    ),
  auditPage: (tenant: string, filter: AuditFilter) =>
    request<AuditPage>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/audit-events?${auditQuery(filter)}`,
    ),
  exportAudit: (tenant: string, filter: AuditFilter, signal?: AbortSignal) => {
    const { q, outcome, operation, from, until } = filter;
    return request<{ blob: Blob; filename: string }>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/audit-events/export?${auditQuery({ q, outcome, operation, from, until })}`,
      { signal },
      async (response) => ({
        blob: await response.blob(),
        filename:
          response.headers
            .get("Content-Disposition")
            ?.match(/filename="([A-Za-z0-9_.-]+)"/)?.[1] ?? "amsonia-audit.csv",
      }),
    );
  },
  me: () => request<Account>("/api/v1/auth/me"),
  logout: async () => {
    const epoch = ++authEpoch;
    const token = getToken();
    await withAuthLock(() =>
      execute<void>("/api/v1/auth/logout", {
        method: "POST",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      }),
    );
    if (epoch === authEpoch) setToken("");
  },
  tenants: async () =>
    (await request<{ items: Tenant[] }>("/api/v1/tenants")).items,
  createTenant: (name: string) =>
    request<Tenant>("/api/v1/tenants", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  permissions: async () =>
    (await request<{ items: Permission[] }>("/api/v1/permissions")).items,
  members: (tenant: string, filter: DirectoryFilter = {}) =>
    request<DirectoryPage<Member>>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/members${directoryQuery(filter)}`,
    ),
  roles: (tenant: string, filter: DirectoryFilter = {}) =>
    request<DirectoryPage<Role>>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles${directoryQuery(filter)}`,
    ),
  capabilities: (tenant: string) =>
    request<Capabilities>(
      `/api/v1/tenants/${encodeURIComponent(tenant)}/capabilities`,
    ),
  createRole: (
    tenant: string,
    input: {
      id: string;
      name: string;
      description: string;
      permissions: string[];
    },
  ) =>
    request<Role>(`/api/v1/tenants/${encodeURIComponent(tenant)}/roles`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  audit: async (tenant: string) =>
    (
      await request<AuditPage>(
        `/api/v1/tenants/${encodeURIComponent(tenant)}/audit-events?limit=80`,
      )
    ).items,
  check: (input: object) =>
    request<{ allowed: boolean; reason: string; effective_scope?: string }>(
      "/api/v1/authorization/check",
      { method: "POST", body: JSON.stringify(input) },
    ),
};
