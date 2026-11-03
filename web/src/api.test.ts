import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, getToken, setToken, subscribeSession } from "./api";

const jsonResponse = (body: unknown, init: ResponseInit = {}) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });

describe("API client", () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  afterEach(() => vi.unstubAllGlobals());

  it("stores the access token after a successful login", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        access_token: "access-token",
        expires_at: "2030-01-01T00:00:00Z",
        account: {
          id: "account-1",
          email: "admin@example.com",
          system_admin: true,
          created_at: "2026-01-01T00:00:00Z",
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const session = await api.login(
      "admin@example.com",
      "correct horse battery staple",
    );

    expect(session.account.email).toBe("admin@example.com");
    expect(getToken()).toBe("access-token");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          email: "admin@example.com",
          password: "correct horse battery staple",
        }),
      }),
    );
  });

  it("sends the bearer token and unwraps list responses", async () => {
    setToken("signed-access-token");
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        items: [{ key: "tenant.read", description: "Read tenants" }],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(api.permissions()).resolves.toEqual([
      { key: "tenant.read", description: "Read tenants" },
    ]);
    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(request.headers).toMatchObject({
      Authorization: "Bearer signed-access-token",
    });
  });

  it("converts structured API failures into APIError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          {
            error: {
              code: "invalid_credentials",
              message: "Email or password is incorrect.",
            },
          },
          { status: 401 },
        ),
      ),
    );

    await expect(
      api.login("admin@example.com", "wrong-password"),
    ).rejects.toMatchObject({
      name: "APIError",
      status: 401,
      code: "invalid_credentials",
      message: "Email or password is incorrect.",
    });
    expect(getToken()).toBe("");
  });

  it("keeps credentials for an explicit retry when logout fails", async () => {
    setToken("expired-token");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new TypeError("network unavailable")),
    );

    await expect(api.logout()).rejects.toThrow("network unavailable");
    expect(getToken()).toBe("expired-token");
  });

  it("rotates the session once when concurrent requests receive 401", async () => {
    setToken("expired-token");
    let protectedCalls = 0;
    let refreshCalls = 0;
    const fetchMock = vi
      .fn()
      .mockImplementation(async (input: string | URL | Request) => {
        const path = String(input);
        if (path.endsWith("/api/v1/auth/refresh")) {
          refreshCalls++;
          await Promise.resolve();
          return jsonResponse({
            access_token: "fresh-token",
            expires_at: "2030-01-01T00:00:00Z",
            account: {
              id: "account-1",
              email: "admin@example.com",
              system_admin: true,
              created_at: "2026-01-01T00:00:00Z",
            },
          });
        }
        protectedCalls++;
        if (protectedCalls <= 2) {
          return jsonResponse(
            { error: { code: "session_invalid", message: "Expired" } },
            { status: 401 },
          );
        }
        return jsonResponse({ items: [] });
      });
    vi.stubGlobal("fetch", fetchMock);

    await Promise.all([api.permissions(), api.permissions()]);

    expect(refreshCalls).toBe(1);
    expect(getToken()).toBe("fresh-token");
    expect(protectedCalls).toBe(4);
  });
});

it("escapes opaque IDs and supplies role versions for administrative writes", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetchMock);
  const role = {
    tenant_id: "t",
    role_id: "support/一",
    name: "Support",
    description: "",
    version: 7,
  };
  await api.bindRole("tenant/二", "member/a", role, true);
  expect(fetchMock).toHaveBeenLastCalledWith(
    "/api/v1/tenants/tenant%2F%E4%BA%8C/members/member%2Fa/roles/support%2F%E4%B8%80",
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify({ expected_version: 7 }),
    }),
  );
  await api.changeGrant(
    "t",
    role,
    {
      permission: "iam:member:read",
      scope: "workspace",
      workspace_roles: ["editor"],
    },
    false,
  );
  expect(fetchMock).toHaveBeenLastCalledWith(
    expect.stringContaining("/roles/support%2F%E4%B8%80/grants"),
    expect.objectContaining({
      method: "DELETE",
      body: JSON.stringify({
        permission: "iam:member:read",
        scope: "workspace",
        workspace_roles: ["editor"],
        expected_version: 7,
      }),
    }),
  );
  vi.unstubAllGlobals();
});

const accountA = {
  id: "account-a",
  email: "a@example.test",
  system_admin: false,
  created_at: "2026-01-01T00:00:00Z",
};
const freshSession = {
  access_token: "fresh-token",
  expires_at: "2030-01-01T00:00:00Z",
  account: accountA,
};
const unauthorized = () =>
  jsonResponse(
    { error: { code: "session_invalid", message: "Expired" } },
    { status: 401 },
  );
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("session recovery", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    setToken("");
  });
  afterEach(() => vi.unstubAllGlobals());
  it("restores a new tab from the refresh cookie with one shared rotation", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(freshSession));
    vi.stubGlobal("fetch", fetchMock);
    const [first, second] = await Promise.all([
      api.restoreSession(),
      api.restoreSession(),
    ]);
    expect(first).toEqual(accountA);
    expect(second).toEqual(accountA);
    expect(getToken()).toBe("fresh-token");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/restore",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
  });
  it("treats an anonymous restore response as signed out", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );
    await expect(api.restoreSession()).resolves.toBeNull();
    expect(getToken()).toBe("");
  });
  it.each(["network", "service"])(
    "preserves credentials on a retryable %s failure",
    async (kind) => {
      setToken("expired-token");
      const fetchMock = vi.fn().mockResolvedValueOnce(unauthorized());
      if (kind === "network")
        fetchMock.mockRejectedValueOnce(new TypeError("offline"));
      else
        fetchMock.mockResolvedValueOnce(
          jsonResponse(
            { error: { code: "unavailable", message: "Try later" } },
            { status: 503 },
          ),
        );
      vi.stubGlobal("fetch", fetchMock);
      await expect(api.restoreSession()).rejects.toThrow();
      expect(getToken()).toBe("expired-token");
      expect(fetchMock).toHaveBeenCalledTimes(2);
    },
  );
  it("retries a late old-token 401 with the token already refreshed by another request", async () => {
    setToken("old-token");
    const late = deferred<Response>();
    let roleCalls = 0,
      refreshCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((path: string, init: RequestInit) => {
        if (path.endsWith("/auth/refresh")) {
          refreshCalls++;
          return Promise.resolve(jsonResponse(freshSession));
        }
        if (path.endsWith("/roles") && roleCalls++ === 0) return late.promise;
        const authorization = (init.headers as Record<string, string>)
          .Authorization;
        return Promise.resolve(
          authorization === "Bearer old-token"
            ? unauthorized()
            : jsonResponse({ items: [] }),
        );
      }),
    );
    const roles = api.roles("t");
    await api.permissions();
    late.resolve(unauthorized());
    await roles;
    expect(refreshCalls).toBe(1);
    expect(roleCalls).toBe(2);
  });
  it("does not resurrect credentials when refresh finishes after sign-out starts", async () => {
    setToken("old-token");
    const rotation = deferred<Response>();
    const listener = vi.fn();
    const unsubscribe = subscribeSession(listener);
    const fetchMock = vi.fn().mockImplementation((path: string) => {
      if (path.endsWith("/auth/refresh")) return rotation.promise;
      if (path.endsWith("/auth/logout"))
        return Promise.resolve(new Response(null, { status: 204 }));
      return Promise.resolve(unauthorized());
    });
    vi.stubGlobal("fetch", fetchMock);
    try {
      const oldRequest = api.permissions().catch((error) => error);
      await vi.waitFor(() =>
        expect(fetchMock).toHaveBeenCalledWith(
          "/api/v1/auth/refresh",
          expect.anything(),
        ),
      );
      const logout = api.logout();
      rotation.resolve(jsonResponse(freshSession));
      await expect(oldRequest).resolves.toMatchObject({
        code: "session_changed",
      });
      await logout;
      expect(getToken()).toBe("");
      expect(listener.mock.calls.some(([account]) => account !== null)).toBe(
        false,
      );
    } finally {
      rotation.resolve(jsonResponse(freshSession));
      unsubscribe();
    }
  });
  it("ends the visible session if a fresh access token is also rejected", async () => {
    setToken("old-token");
    const listener = vi.fn();
    const unsubscribe = subscribeSession(listener);
    const fetchMock = vi
      .fn()
      .mockImplementation((path: string) =>
        Promise.resolve(
          path.endsWith("/auth/refresh")
            ? jsonResponse(freshSession)
            : unauthorized(),
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    try {
      await expect(api.permissions()).rejects.toMatchObject({ status: 401 });
      expect(getToken()).toBe("");
      expect(listener).toHaveBeenLastCalledWith(null, true);
      expect(fetchMock).toHaveBeenCalledTimes(3);
    } finally {
      unsubscribe();
    }
  });
  it("does not replay a mutation as another account after a shared-cookie identity change", async () => {
    const accountB = { ...accountA, id: "account-b", email: "b@example.test" };
    let profileCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((path: string) => {
        if (path.endsWith("/auth/login"))
          return Promise.resolve(jsonResponse(freshSession));
        if (path.endsWith("/auth/refresh"))
          return Promise.resolve(
            jsonResponse({
              ...freshSession,
              access_token: "account-b-token",
              account: accountB,
            }),
          );
        profileCalls++;
        return Promise.resolve(unauthorized());
      }),
    );
    await api.login(accountA.email, "password");
    await expect(api.updateProfile("Changed name")).rejects.toMatchObject({
      code: "session_changed",
    });
    expect(profileCalls).toBe(1);
    expect(getToken()).toBe("account-b-token");
  });
  it("queues cookie changes through an available browser auth lock", async () => {
    const request = vi.fn((_name: string, action: () => Promise<unknown>) =>
      action(),
    );
    vi.stubGlobal("navigator", { locks: { request } });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(jsonResponse(freshSession))
        .mockResolvedValueOnce(new Response(null, { status: 204 })),
    );
    await api.login(accountA.email, "password");
    await api.logout();
    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.calls[0][0]).toBe("amsonia.core.auth:");
    expect(getToken()).toBe("");
  });
});
