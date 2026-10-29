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
and built-site checks covering 23 routes / 24 HTML documents. Initial artwork
validation reused Astro 7.2.2. The maintenance validation on 2026-10-29 aligned
the installed dependencies with the lockfile and passed with the manifest pin
Astro 7.3.6 using Node 24.19.0 and the CI public-URL configuration.

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

## Approved build maintenance

To clear the existing security scans, the approved follow-up updates undici
8.12.2 and zod 4.6.6 in the site lockfile, Vite 7.3.7 and Vitest 4.1.12 in the
console manifest/lock, and Go 1.26.10 in the module, CI and site-release build
toolchain pins. Version requirements are updated in the setup documentation.
Both npm audits report zero vulnerabilities. Console typecheck, lint, eight
tests and build pass. Go source provenance, vet, unit/race tests and the
reachable-vulnerability scan pass with Go 1.26.10. The scan reports zero reachable
vulnerabilities; four additional module-level findings have no imported affected
packages or calls. Existing Pages/AWS deployment contract checks also pass.

The rebuilt family, Next, Platform and documentation pages were checked in
Playwright at desktop and mobile sizes with no missing assets, page errors or
horizontal overflow. Runtime SVG checksums remain unchanged. The maintenance
work changes build/dependency inputs only; artwork, application/API logic,
deployment steps, access boundaries and production data are unchanged. This
source integration is one draft PR; it has not been merged or released.


## Navigation and reading iteration — 2026-10-29

The native mobile disclosure now closes on Escape (returning focus to its
summary), outside pointer/focus and when the viewport crosses above the 1080px mobile breakpoint. Its
label follows the expanded state, control/panel IDs are connected, and a bounded
scroll area keeps the final contact link reachable in a 390x260 viewport. The
native navigation remains usable without JavaScript. Main fragment targets use
a 5rem scroll margin so document headings clear the sticky header.

Playwright reproduced all three dismissal failures and a document heading at 0px
beneath the 65px header before the fix. Dismissal/focus behavior now passes;
collapsed links are excluded from Tab navigation, the final menu link fits within
the short viewport, and direct/clicked document targets stop at 80px. Desktop and
mobile page checks retain loaded artwork, one H1, canonical links and no overflow
or page exceptions. The existing site check passes23 routes and 24 HTML documents.
Browser evidence is retained in the companion checkout's
`output/website-iteration-20261029/`. This iteration does not alter dependencies,
artwork, application/API behavior, analytics or deployment.
