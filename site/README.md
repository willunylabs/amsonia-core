# Amsonia product site

The Astro site in this directory is the canonical public product and technical
surface for Amsonia. It is deployed to `amsonia.dev`; the React Console remains
separate at `demo.amsonia.dev` and is not indexable.

## Shared Willuny visual system

Amsonia uses the same production visual language as `willuny-go-ui`:

- the shared five-petal Amsonia/Willuny mark from `public/amsonia-mark.svg`;
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

- `/` introduces the Amsonia product family and makes Core the first product.
- `/core` and `/core/docs/*` contain evidence-backed open-source documentation.
- `/docs` is the product-family documentation entry point.
- commercial pricing, licensing, purchase, and company content remain on
  `willuny.com`.
- unreleased Amsonia products do not receive indexable placeholder pages.

## Local checks

```bash
npm ci
npm run check
```

`npm run check` validates configured public origins, verifies the shared logo
geometry and visual tokens, builds the static site, checks canonical metadata
and internal links, and rejects broken routes. Keep the shared logo, Open Graph
image, global tokens, header, and footer changes in the same pull request so the
production system cannot drift between surfaces.
