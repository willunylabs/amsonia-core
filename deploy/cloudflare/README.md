# Amsonia product site on Cloudflare

The `amsonia-dev-commerce` Worker serves [amsonia.dev](https://amsonia.dev/).
Use the [Commerce production runbook](https://github.com/willunylabs/amsonia-commerce/blob/main/deployments/amsonia-dev/README.md)
for Worker releases and the [static publication guide](https://github.com/willunylabs/amsonia-commerce/blob/main/docs/commerce/static-publication.md)
for public page updates.

## Retired Pages origin

Cloudflare permanently redirects `amsonia-site.pages.dev` and its deployment
subdomains to `amsonia.dev` with the same path and query string. The manual
`deploy-site-pages.yml` workflow is disabled. The [2026-10-08 Pages release](https://github.com/willunylabs/amsonia-core/actions/runs/37731939252)
records the former release pipeline.

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
