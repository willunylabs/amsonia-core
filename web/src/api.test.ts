import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, getToken, setToken } from "./api";

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
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
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

  it("always clears the local token when logout fails", async () => {
    setToken("expired-token");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new TypeError("network unavailable")),
    );

    await expect(api.logout()).rejects.toThrow("network unavailable");
    expect(getToken()).toBe("");
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
