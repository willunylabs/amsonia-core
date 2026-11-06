# Product-site SEO follow-up — November 3

The owner asked to continue the audited SEO repairs. Changes are confined to the
static product site and its existing built-site validation.

- Platform documentation links to its quickstart, deployment and billing guides.
  Platform evaluation links to changelog and roadmap; the Go buyer guide links
  to the Go comparison guide. These pages now have contextual navigation entries.
- The shared layout supports non-indexable documents. The error document emits
  `noindex,follow` and no canonical URL. Ordinary missing-route HTTP handling is
  unchanged. Static hosting may still serve `/404` itself with 200, but its
  exclusion metadata prevents it being advertised as an indexable content page.
- The existing consent-gated GA4 loader links only the configured company and
  product hostnames before tag initialization. The public Next demo stays outside
  that measurement boundary.
- The existing production environment's missing `PUBLIC_GA_ID` was restored to
  the verified existing public Web stream. No new property or credential was
  created.

Validation builds use existing dependencies with task-local Vite/Astro caches,
preserving the active primary checkout. The static build and existing checks
cover 23 required routes and 24 HTML files; the checker now verifies the actual
homepage anchor graph and the error document's exclusion metadata.

Production publication and consented GA4 reception require separate verification
in the release evidence. This record does not claim search-engine recrawl or
ranking gains.
