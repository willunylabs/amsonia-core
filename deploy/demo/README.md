# Amsonia Core demo deployment

This directory describes the small, disposable demo deployment used for
`https://amsonia.dev`.

The demo runs the ARM64 API and React console on an existing AWS EC2 host behind
the host's Traefik HTTPS entrypoint. PostgreSQL listens only on loopback on a
dedicated port. The deployment is intentionally separate from the host's
existing applications and is not a production availability or security claim.

## Runtime layout

```text
Cloudflare DNS/proxy
  -> Traefik :80/:443 (Host: amsonia.dev)
     -> /api, /health, /readyz -> 127.0.0.1:8082 (Go API)
     -> everything else         -> 127.0.0.1:8083 (static React console)
                                  |
                                  -> PostgreSQL 127.0.0.1:5433
```

The web build uses same-origin `/api` requests. The static console includes
`noindex,nofollow` because the hostname is a technical preview, not the
canonical product site. Commercial and indexable content remains on
[`willuny.xyz`](https://willuny.xyz).

## Deployment contract

- Use a dedicated system user, database, roles, ports, and systemd units.
- Keep `/etc/amsonia-core/demo.env` mode `0640`, owned by `root:amsonia-core`;
  never commit it. The API service reads it through its dedicated group.
- Generate demo administrator credentials separately and store them outside the
  repository. Do not put credentials in the README, browser screenshots, or CI.
- Validate `/health`, `/readyz`, same-origin `/api/v1/auth/login`, and the live
  console after every release.
- Remove the systemd units, demo database, DNS record, and release directory when
  the demo is retired.

The deployment intentionally uses a single host and a local PostgreSQL instance
to keep demo cost low. It must not be described as the managed Amsonia product,
multi-AZ infrastructure, or a production SLA.
