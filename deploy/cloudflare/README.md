# Amsonia static site on Cloudflare Pages

Verified operational observations: 2026-10-26. The canonical product site is a
static Astro build. The configured Pages project is `amsonia-site`, and the
manual `.github/workflows/deploy-site-pages.yml` workflow is active. Successful
Pages releases include [run37731939252](https://github.com/willunylabs/amsonia-core/actions/runs/37731939252)
on 2026-10-08. The legacy AWS `.github/workflows/deploy-site.yml` workflow is
`disabled_manually`; its `amsonia-prod` EC2 instance is stopped.

These are dated workflow/inventory observations, not a reconstructed custom-domain
cutover date or a direct Cloudflare DNS audit. Verify live domain state separately
when planning an infrastructure change. The old shadow-only plan is historical.

## Existing release workflow

The manual workflow builds the exact requested `ref`, runs `npm run check`, and
uploads `site/dist` to the configured project with branch `main` and the resolved
commit SHA. Its post-deploy checks target the project's `pages.dev` hostname.
The workflow still labels that verification step “shadow”; it does not attach
custom domains or change DNS. A green run alone does not verify canonical-host
routing. Check the public `https://amsonia.dev` pages, canonical metadata, robots,
sitemap, redirects and deliberate 404 behavior after an authorized release.

The protected `amsonia-production` environment supplies:

| Kind | Name | Purpose |
| --- | --- | --- |
| Variable | `AMSONIA_CLOUDFLARE_PAGES_PROJECT` | `amsonia-site` project |
| Secret | `CLOUDFLARE_ACCOUNT_ID` | deployment account |
| Secret | `CLOUDFLARE_API_TOKEN` | scoped Pages deployment credential |

Keep secret values in the existing deployment mechanism. Do not copy them into
local docs, commands or browser JavaScript. This documentation update does not
change deployment settings, DNS or access.

## Service boundaries and recovery

The Willuny blog/admin backend is separate: running AWS `willuny-intl-app`,
`i-07cfb87c8d7f2e5fa`, `us-east-1`, with API origin `https://api.willuny.com`.
Articles are CMS records, not Astro pages. Use the verified
`amsonia-web/docs/runbooks/blog-cms-publishing.md` procedure for publication.

The [legacy AWS inventory](../aws/README.md) is retained for recovery context.
A stopped host is not a ready rollback target. Any restoration or DNS change
requires a separate reviewed plan that verifies artifacts, services, health and
current domain state. Do not re-enable the disabled workflow or start the old
instance merely to publish a blog post.
