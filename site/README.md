# Amsonia product site

The Astro site in this directory is the canonical public product surface for
Amsonia. It is deployed to `amsonia.dev`. The Next product page links to the
separate public Amsonia Next demonstration after anonymous-access acceptance.
The demo and Platform transactions run outside this indexable static site;
the static build does not call them or depend on their runtime availability.

The reviewed public demo destination lives in `config/public-links.json`, separate
from runtime source and canonical-origin configuration. Keep this link restricted
to the Next page, disclose its current preview limits, and reverify anonymous
access and application permissions before changing it. No GitHub environment,
workflow or hosting setting is needed to publish this public-link configuration.

## Shared Willuny visual system

Amsonia uses the same production visual language as `amsonia-web`:

- the shared five-petal Amsonia/Willuny mark from the versioned
  `public/amsonia-mark-v2.svg` asset;
- white and slate surfaces with the `#635bff` to `#7a73ff` brand gradient;
- slate typography, indigo technical labels, subtle 56-pixel grid fields, and
  restrained borders and shadows;
- the same button geometry, navigation density, content widths, focus states,
  and responsive breakpoints used by the Willuny company site.

The product site may use a dark indigo technical panel to distinguish Amsonia
from the company surface. It must not introduce an unrelated palette, logo,
serif display system, or decorative theme. The old cream, forest, acid-green,
and botanical-specimen presentation is retired.

## Information boundary

- `/` introduces the Amsonia product family and its Platform and Next products.
- `/platform` owns the commercial Go + Next.js, multi-tenant product narrative.
- `/next` owns the private-source Next.js starter narrative; source availability and
  license terms are confirmed by Willuny inquiry.
- `/docs` is the product-family documentation entry point.
- product positioning, pricing, and purchase explanations remain indexable on
  `amsonia.dev`; transaction CTAs enter the non-indexable Willuny transaction surface;
- company, publisher, blog, legal, and support content remain on `willuny.com`.
- unreleased Amsonia products do not receive indexable placeholder pages.
- retired `/core/*` and `/releases` site routes redirect to the external GitHub
  repository and are excluded from the sitemap.

## Local checks

From the repository root, run `make site-check`. It supplies the same test URLs
as CI and works from a fresh checkout.

`npm run check` validates configured public origins, verifies the shared logo
geometry and visual tokens, builds the static site, checks canonical metadata
and internal links, and rejects broken routes. Keep the shared logo, Open Graph
image, global tokens, header, and footer changes in the same pull request so the
production system cannot drift between surfaces.

Public static assets are served with a one-year immutable cache policy. Never
overwrite a published logo filename in place; increment the versioned filename
and update every HTML, favicon, and structured-data reference in the same pull
request.

`site/public/_headers` carries the matching Cloudflare Pages edge policy. Keep
it aligned with the current origin security and immutable-asset contract.
