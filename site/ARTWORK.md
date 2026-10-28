# Affinity website artwork

The family and Next pages now use the approved source-layer illustration; the
Platform page uses the connected-capabilities illustration. New feature icons
replace the former ornamental labels. Existing product claims, links, schema,
private/single-tenant Next scope and source-access/contact semantics are retained.

The public button rules implement the Affinity sheet using live text: shared
indigo/violet tokens, 8px radius, 48px targets, soft shadows, hover/press/focus
states and reduced-motion support. Intro artwork is optional, so documentation
and other ordinary content pages keep their existing layout. The mobile inset
band uses the same 16px gutter as its content shell to prevent horizontal overflow.

## Provenance

Native AF source, full sheets and PNG exports remain in
`iCloud Drive/Codex-Artifacts/Affinity/Willuny-Amsonia-Web-20261027/`.
Only ten required SVGs are copied to `public/artwork/affinity-web-v1/`.
`ARTWORK.sha256` records their byte-identical source export checksums. This is a
versioned asset path; use a new revision path for later artwork changes.

Meaningful hero art has a descriptive alt and explicit dimensions. Feature icons
are decorative and retain empty alt text. The Platform multi-tenant illustration
is used only on Platform, not to advertise customer tenancy for Next.

## Validation

The existing `npm run check` passed: domain and visual guards, a static build,
and built-site checks covering 23 routes / 24 HTML documents. Node 24.19.0 and
existing dependencies were reused. Installed Astro 7.2.2 differs from the manifest
pin 7.3.6, so clean CI dependency builds remain the version acceptance check.

Playwright verified family, Next and Platform at 1440x900 and 390x844, plus the
shared documentation intro and mobile navigation. New illustrations load, each
page keeps one H1 and its canonical, and no horizontal overflow remains. Both
sites show the 2px indigo keyboard outline, 3px offset, 2px hover rise, pressed
surface/shadow and reduced-motion behavior. The final rendering pass reports no
page exceptions or failed responses.

Representative screenshots are in `evidence/affinity-web-assets-20261028/`.
Full reports and eight screenshots are retained in the companion amsonia-web
checkout's `output/affinity-web-assets-20261027/` directory. The companion branch
`codex/affinity-web-assets-20261027` updates the actual Willuny homepage and public
controls; the legacy redirected Next.js `/amsonia` page is not the product site.

All changes in this repository are scoped to `site/`. No Go/backend, API,
deployment or production-data change is included. This source integration is
submitted as one draft PR; it has not been merged or released.
