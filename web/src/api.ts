import type { Account, AuditEvent, Member, Permission, Role, Session, Tenant } from "./types";

const CONFIGURED_API_BASE = (import.meta.env.VITE_API_URL ?? "").replace(/\/$/, "");
const configuredURL = new URL(CONFIGURED_API_BASE || window.location.origin, window.location.origin);
if (configuredURL.origin !== window.location.origin) {
  throw new Error("VITE_API_URL must resolve to the console origin; proxy API traffic under the same origin.");
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
      ...init.headers
    }
  });
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as { error?: { code?: string; message?: string } } | null;
    throw new APIError(response.status, payload?.error?.code ?? "request_failed", payload?.error?.message ?? "The request could not be completed.");
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

async function refreshAccess(): Promise<boolean> {
  if (!refreshInFlight) {
    refreshInFlight = execute<Session>("/api/v1/auth/refresh", { method: "POST" })
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
    const canRefresh = error instanceof APIError && error.status === 401 &&
      path !== "/api/v1/auth/login" && path !== "/api/v1/auth/refresh" && path !== "/api/v1/auth/logout";
    if (!canRefresh || !(await refreshAccess())) throw error;
    return execute<T>(path, init);
  }
}

export const api = {
  async login(email: string, password: string): Promise<Session> {
    const session = await request<Session>("/api/v1/auth/login", { method: "POST", body: JSON.stringify({ email, password }) });
    setToken(session.access_token);
    return session;
  },
  me: () => request<Account>("/api/v1/auth/me"),
  logout: async () => {
    try {
      await request<void>("/api/v1/auth/logout", { method: "POST" });
    } finally {
      setToken("");
    }
  },
  tenants: async () => (await request<{ items: Tenant[] }>("/api/v1/tenants")).items,
  createTenant: (name: string) => request<Tenant>("/api/v1/tenants", { method: "POST", body: JSON.stringify({ name }) }),
  permissions: async () => (await request<{ items: Permission[] }>("/api/v1/permissions")).items,
  members: async (tenant: string) => (await request<{ items: Member[] }>(`/api/v1/tenants/${tenant}/members`)).items,
  roles: async (tenant: string) => (await request<{ items: Role[] }>(`/api/v1/tenants/${tenant}/roles`)).items,
  createRole: (tenant: string, input: { id: string; name: string; description: string; permissions: string[] }) => request<Role>(`/api/v1/tenants/${tenant}/roles`, { method: "POST", body: JSON.stringify(input) }),
  audit: async (tenant: string) => (await request<{ items: AuditEvent[] }>(`/api/v1/tenants/${tenant}/audit-events?limit=80`)).items,
  check: (input: object) => request<{ allowed: boolean; reason: string; effective_scope?: string }>("/api/v1/authorization/check", { method: "POST", body: JSON.stringify(input) })
};
