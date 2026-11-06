# Product domain copy implementation result

Date: November 1, 2026, Asia/Shanghai.

The owner approved the company/product/demo positioning and requested continuation.
The accepted company plan merged in amsonia-web PR221 as
53798879e8ff250961655d94c5cd68aa2abce96a.

The Amsonia homepage now owns family discovery and Next-versus-Platform selection.
The Next page owns its Next.js starter evaluation and private source inquiry.
Next remains the primary product. The family title, visible hero, publisher
schema description and footer agree; the two substantially changed pages have
November 1 sitemap dates. The README's obsolete open-source Next statement is
aligned with the accepted private source decision.

Validation passed: domain and visual-system checks, one successful configured
static build, generated SEO/internal-link checks for 23 routes and 24 HTML files,
and browser checks at 1440x900 and 390x844 for home, Next and comparison. Each
page returned 200 with one H1, the correct self-canonical and indexable metadata,
without horizontal overflow, observed page exceptions or failed resource responses.
The homepage comparison and Next capability links were clicked; the source inquiry
points to https://willuny.com/shop/contact. No inquiry was submitted.

The first build invocation required missing public-origin configuration; the
successful invocation used the actual product, company, source and catalog URLs.
No mock backend, authentication or billing data was introduced. No demo link was
added: anonymous requests to willuny.xyz remain protected by Vercel Authentication.
The separate prepared demo patch is not an applied or published Next release.

Detailed browser evidence and screenshots are retained at
/Users/will/Codes/amsonia-web/output/domain-copy-implementation-20261101/.
The pre-existing port4321 server in another checkout was preserved. A task-owned
static server on port4322 served this exact build; owned browser contexts closed
in finally. Dependencies were reused; no install or new worktree was needed.
No backend, DNS, access-protection, workflow or deployment settings changed.
The code is delivered in a draft PR; publication is pending.

## Merge-gate continuation

The required web example job failed on GHSA-g3hv-75wg-m249 in minimatch,
not on the changed product-site pages. The advisory marks versions before
11.0.1 affected: https://github.com/advisories/GHSA-g3hv-75wg-m249.
A development-only override pins minimatch11.0.3 and regenerates the web lockfile.
No force audit fix, audit exclusion, workflow bypass or runtime dependency change
was used. The production lock entries are byte-for-byte equivalent as parsed
objects; only minimatch and its development-only dependency graph changed.

The required moderate-level npm audit reports zero vulnerabilities. Web ESLint,
TypeScript, both test files/all eight tests and the existing configured Vite build
pass. A deliberate unused TSX variable is still reported, source files are linted,
and generated dist paths remain ignored, verifying matcher compatibility.
The npm dependency tree resolves minimatch11.0.3 for all lint/parser consumers.
These checks target the dependency change; the already-passed product-site build
was not repeated. Generated web build output will be cleaned after verification.
