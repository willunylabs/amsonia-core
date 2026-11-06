# Amsonia Brand and Product Boundaries

Status: Accepted

Updated: 2026-11-06

This decision records the user's confirmed growth model: Next is the primary
commercial product; the Go/Core repository is the open-source acquisition track.
The existing open-source website entry and repository links support both paths.
This supersedes older instructions excluding Core from all site copy and keyword
coverage. It does not turn Core into a third commercial product.

## Hierarchy

- **Willuny Labs** is the company, publisher, seller, and support organization.
- **Amsonia** is the product family.
- **Amsonia Next** is the primary commercial focus: a private, single-tenant
  Next.js SaaS foundation with accounts, Stripe, PostgreSQL/Drizzle, and admin.
- **Amsonia Core** is the independently useful Apache-2.0 Go/PostgreSQL
  administration scaffold. It defaults to one organization, with accounts,
  invitations, RBAC, sessions, audit, API, CLI, and Console. Multi-tenancy and
  PostgreSQL row-level isolation are supported. Billing and complete commercial
  product workflows are outside this repository.

## Canonical homes

| Surface | Canonical URL | Purpose |
| --- | --- | --- |
| Company | `https://willuny.com` | Company, portfolio, engineering blog, legal, account, and support |
| Product family | `https://amsonia.dev` | Product evaluation, capabilities, documentation, and comparisons |
| Next | `https://amsonia.dev/next/` | Primary product evaluation and source-access inquiry |
| Open-source entry | `https://amsonia.dev/open-source/` | Go authorization search entry, runnable-example links, GitHub visits, and stars |
| Repository | `https://github.com/willunylabs/amsonia-core` | Core source, implementation docs, tests, issues, releases, and contribution |
| Company inquiry | `https://willuny.com/shop/contact` | Confirm Next availability, source scope, delivery, licensing, and support |

The retired `/core` routes continue redirecting to GitHub. Do not create a
competing Core route, product card, product-catalog entry, or software-product
schema. The commercial catalog contains Next only.

## Search and publishing

1. Next product and stack queries lead to `/next/`; factual product comparisons
   belong on the Amsonia site. One useful page can cover related keyword variants.
2. Go authorization, tenant RBAC, and PostgreSQL RLS queries can lead through the
   existing `/open-source/` gateway to specific public source and runnable examples.
3. Existing Willuny engineering article URLs remain stable. Blog articles are
   stored through the existing backend CMS API as Tiptap documents; adding a source
   draft or changing a static product page does not publish a blog article.
4. Core is independently useful. Give readers a direct GitHub path and an optional
   path to Next; keep the open-source and commercial scopes explicit.
5. Never publish private Next source, invented offers, unsupported integrations,
   customer claims, or unverified production/reliability promises. Current Next
   inquiry terms are confirmed by the company; no price or checkout offer is
   implied by this SEO work.
6. Use configured canonical origins, self-canonicals, useful crawlable links,
   accurate metadata, and clean sitemap entries. Do not duplicate body copy across
   the two owned domains or add internal campaign tags to owned-domain links.
7. Compare vendors from current primary sources with a visible review date and
   publisher disclosure. Missing documentation does not prove a missing feature.

## Measurement

Review equivalent GSC windows for Next product/stack/comparison queries and Go
technical queries separately. Track consented product inquiry and GitHub outbound
clicks, GitHub traffic/clones/stars, and GitHub-to-website visits. These are
separate observations; do not infer that every star came from the website or
that star growth automatically improved Google rankings.
