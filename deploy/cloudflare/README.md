# Cloudflare Pages migration

The canonical Amsonia product and technical site is a fully static Astro build.
Cloudflare Pages is its target origin so the dedicated `amsonia-prod` EC2
instance can be retired after a measured cutover.

## Phase 1: shadow deployment

The manual `deploy-site-pages.yml` workflow builds the exact requested Git ref,
runs the complete site audit, uploads `site/dist`, and verifies the project on
its `pages.dev` hostname. It does not attach a custom domain or change DNS.

Configure the protected `amsonia-production` GitHub environment with:

| Kind | Name | Value |
| --- | --- | --- |
| Variable | `AMSONIA_CLOUDFLARE_PAGES_PROJECT` | dedicated Pages project name |
| Secret | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID |
| Secret | `CLOUDFLARE_API_TOKEN` | token scoped to Pages edit for this account |

Keep the existing AWS deploy workflow enabled throughout this phase. Compare
the Pages preview with the public origin, including canonical tags, robots,
sitemap, security headers, immutable assets, and real 404 responses.

## Phase 2: custom-domain cutover

After the shadow deployment passes review:

1. attach `amsonia.dev` and `www.amsonia.dev` to the Pages project;
2. verify `/`, `/core`, `/docs`, `robots.txt`, and `sitemap.xml` on the public
   host, plus a deliberate 404;
3. submit the sitemap again only if Search Console reports a fetch problem;
4. leave `amsonia-prod` running, but freeze its deploy workflow, for a minimum
   seven-day observation window; and
5. retire the EC2 instance, Elastic IP, dedicated deploy role, SSM document, and
   artifact path in a separate reviewed change.

Rollback during the observation window is to detach the Pages custom domain and
restore the proxied DNS records to the unchanged AWS origin. No content URL or
canonical changes during this migration, so search equity stays on
`amsonia.dev`.
