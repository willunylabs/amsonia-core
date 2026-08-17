# Amsonia website and Core demo deployment

This directory defines the origin-side deployment contract for the indexable
Amsonia product site and the isolated, non-indexable Core demo.

The public origin is the dedicated `amsonia-prod` EC2 instance documented in
[`../aws/README.md`](../aws/README.md). The existing Willuny EC2 instance is
not an Amsonia deployment target.

## Runtime layout

```text
Cloudflare DNS/proxy
  -> Traefik :443
     -> Host: amsonia.dev
        -> everything              -> 127.0.0.1:8084 (Astro static site)
     -> Host: demo.amsonia.dev
        -> /api, /health, /readyz  -> 127.0.0.1:8082 (Go API)
        -> everything else         -> 127.0.0.1:8083 (React Console)
                                     |
                                     -> PostgreSQL 127.0.0.1:5433
```

The repository-built `amsonia-static` binary serves both static applications.
The product site uses strict file resolution and returns a real 404 for unknown
paths. The Console enables extensionless SPA fallback, while missing files such
as `/sitemap.xml` still return a real 404.

## Search contract

- `amsonia.dev` is indexable and publishes `/robots.txt` plus `/sitemap.xml`.
- `demo.amsonia.dev` HTML includes `<meta name="robots" content="noindex">`.
- Traefik adds `X-Robots-Tag: noindex` to every Demo response, including API and
  error responses.
- Demo `/robots.txt` allows crawling so crawlers can observe `noindex`.
- Demo `/sitemap.xml` is absent and returns 404.
- `amsonia.dev/api/*` reaches the strict static server and returns 404.

## Release artifacts

Build the release on a compatible worker:

The Astro site build uses Node.js 22.19 or newer. Core development and the Vite
Console retain the repository's documented Node.js 22.15 baseline.

```bash
CGO_ENABLED=0 go build -o amsonia-static ./cmd/amsonia-static
npm --prefix web ci
npm --prefix web run build
npm --prefix site ci
npm --prefix site run check
```

Install artifacts without combining rollback boundaries:

```text
/opt/amsonia-core-demo/releases/<release>/amsonia-api
/opt/amsonia-core-demo/releases/<release>/amsonia-static
/opt/amsonia-core-demo/releases/<release>/web/*
/opt/amsonia-site/releases/<release>/amsonia-static
/opt/amsonia-site/releases/<release>/site/*
```

Keep the previous product-site and Demo release directories for at least 14
days. The `current` symlinks must be switched independently.

Use separate release roots so a Console rollback never changes the API binary:

```text
/opt/amsonia-core-demo/current           # API and CLI; unchanged in this rollout
/opt/amsonia-core-demo-web/current       # Demo Console and static server
/opt/amsonia-site/current                # Product site and static server
```

Production uses `traefik-dynamic.yml` as the active dynamic configuration. The
apex serves the product site and the Demo hostname serves the Console and API.
The `traefik-phase1.yml` and `traefik-demo-only.yml` files document the staged
rollout options and can support a hostname-scoped rollback, but they are not
active in production.

## Operational constraints

- Use dedicated system users, database roles, ports, and systemd units.
- Keep `/etc/amsonia-core/demo.env` mode `0640`, owned by
  `root:amsonia-core`; never commit it.
- Generate Demo administrator credentials separately and store them outside the
  repository. Never place credentials in documentation, screenshots, or CI.
- Validate `/health`, `/readyz`, same-origin `/api/v1/auth/login`, the Console,
  product-site `/healthz`, `/robots.txt`, `/sitemap.xml`, and unknown-path 404s
  after every release.
- The Demo is a technical preview, not a managed production service, multi-AZ
  deployment, availability promise, or SLA.

Cloudflare owns public HTTP and `www` canonical redirects. The origin-side HTTPS
redirect remains only as a direct-origin safety fallback and must not become an
extra public hop when Cloudflare rules are active.
