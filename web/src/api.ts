import type {
  Account,
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
let refreshInFlight: Promise<boolean> | null = null;

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
  if (token) sessionStorage.setItem(TOKEN_KEY, token);
  else sessionStorage.removeItem(TOKEN_KEY);
}

async function execute<T>(path: string, init: RequestInit): Promise<T> {
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
  return response.json() as Promise<T>;
}

async function refreshAccess(): Promise<boolean> {
  if (!refreshInFlight) {
    refreshInFlight = execute<Session>("/api/v1/auth/refresh", {
      method: "POST",
    })
      .then((session) => {
        setToken(session.access_token);
        return true;
      })
      .catch(() => {
        setToken("");
        return false;
      })
      .finally(() => {
        refreshInFlight = null;
      });
  }
  return refreshInFlight;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  try {
    return await execute<T>(path, init);
  } catch (error) {
    const canRefresh =
      error instanceof APIError &&
      error.status === 401 &&
      path !== "/api/v1/auth/login" &&
      path !== "/api/v1/auth/refresh" &&
      path !== "/api/v1/auth/logout";
    if (!canRefresh || !(await refreshAccess())) throw error;
    return execute<T>(path, init);
  }
}

export const api = {
  async login(email: string, password: string): Promise<Session> {
    const session = await request<Session>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    setToken(session.access_token);
    return session;
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
  invitations: async (tenant: string) =>
    (
      await request<{ items: Invitation[] }>(
        `/api/v1/tenants/${encodeURIComponent(tenant)}/invitations`,
      )
    ).items,
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
      `/api/v1/tenants/${encodeURIComponent(tenant)}/roles/${encodeURIComponent(id)}${before ? `?before_version=${before}` : ""}`,
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
      `/api/v1/tenants/${encodeURIComponent(tenant)}/audit-events?${new URLSearchParams(
        Object.entries(filter)
          .filter(([, value]) => value !== undefined && value !== "")
          .map(([key, value]) => [key, String(value)]),
      )}`,
    ),
  me: () => request<Account>("/api/v1/auth/me"),
  logout: async () => {
    try {
      await request<void>("/api/v1/auth/logout", { method: "POST" });
    } finally {
      setToken("");
    }
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
  members: async (tenant: string) =>
    (
      await request<{ items: Member[] }>(
        `/api/v1/tenants/${encodeURIComponent(tenant)}/members`,
      )
    ).items,
  roles: async (tenant: string) =>
    (
      await request<{ items: Role[] }>(
        `/api/v1/tenants/${encodeURIComponent(tenant)}/roles`,
      )
    ).items,
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
