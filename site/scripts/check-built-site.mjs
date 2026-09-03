import assert from 'node:assert/strict';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

const siteRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const distRoot = join(siteRoot, 'dist');
const origin = String(process.env.PUBLIC_SITE_ORIGIN || '').trim();
assert.ok(origin, 'PUBLIC_SITE_ORIGIN is required for the site audit');
const companyOrigin = String(process.env.PUBLIC_COMPANY_ORIGIN || '').trim();
assert.ok(companyOrigin, 'PUBLIC_COMPANY_ORIGIN is required for the site audit');
const requiredRoutes = [
  '/', '/products', '/platform', '/platform/docs', '/platform/docs/quickstart', '/platform/docs/deployment',
  '/platform/docs/billing', '/platform/changelog', '/platform/roadmap', '/next', '/compare/platform-vs-next',
  '/go-saas-boilerplate', '/best-go-saas-boilerplates',
  '/features/multi-tenancy', '/features/rbac', '/features/stripe-billing', '/architecture', '/security', '/pricing', '/license',
  '/docs', '/open-source', '/about'
];

function pageFile(route) {
  if (route === '/') return join(distRoot, 'index.html');
  const relative = route.replace(/^\//, '');
  const candidates = [join(distRoot, relative, 'index.html'), join(distRoot, `${relative}.html`)];
  return candidates.find(existsSync) ?? candidates[0];
}

function collectHtml(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    return statSync(path).isDirectory() ? collectHtml(path) : path.endsWith('.html') ? [path] : [];
  });
}

function canonicalFor(route) {
  const url = new URL(route, origin);
  if (url.pathname !== '/' && !url.pathname.endsWith('/')) url.pathname += '/';
  return url.toString();
}

for (const route of requiredRoutes) {
  const file = pageFile(route);
  assert.ok(existsSync(file), `missing built route: ${route}`);
  const html = readFileSync(file, 'utf8');
  assert.match(html, new RegExp('<title>[^<]{12,}</title>'), `${route} must have a descriptive title`);
  assert.match(html, /<meta name="description" content="[^"]{70,}"/, `${route} must have a useful description`);
  assert.ok(html.includes(`<link rel="canonical" href="${canonicalFor(route)}">`), `${route} must self-canonicalize`);
  assert.match(html, /<meta property="og:title" content="[^"]+">/, `${route} must have Open Graph metadata`);
  assert.match(html, /<meta name="robots" content="index,follow,/, `${route} must be indexable`);
  assert.doesNotMatch(html, /noindex/i, `${route} must not contain noindex`);

  for (const match of html.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)) {
    assert.doesNotThrow(() => JSON.parse(match[1]), `${route} contains invalid JSON-LD`);
  }
}

const htmlFiles = collectHtml(distRoot);
for (const file of htmlFiles) {
  const html = readFileSync(file, 'utf8');
  assert.doesNotMatch(html, /willuny\.xyz|willunylabs\.com/i, `${file} must not publish a legacy Willuny domain`);
  assert.doesNotMatch(html, /Complete Amsonia|Commercial Amsonia|Amsonia Full/, `${file} must use the accepted product names`);
  assert.doesNotMatch(html, /demo\.amsonia\.dev|data-cta-target="demo"|Live demo/i, `${file} must not present an unverified hosted demo as product evidence`);
  assert.doesNotMatch(html, /Amsonia Core/i, `${file} must keep Core outside the product-site narrative`);
  assert.doesNotMatch(html, /mailto:|email-protection/i, `${file} must use the canonical contact page instead of an obfuscated email URL`);
  for (const match of html.matchAll(/href="(\/[^"]*)"/g)) {
    const href = match[1];
    const url = new URL(href, origin);
    if (url.origin !== origin || url.pathname.startsWith('/_astro/')) continue;
    const lastSegment = url.pathname.split('/').at(-1) || '';
    if (url.pathname !== '/' && !lastSegment.includes('.')) {
      assert.ok(url.pathname.endsWith('/'), `${file} links to a redirecting internal route: ${href}`);
    }
    const target = url.pathname === '/404' ? pageFile('/404') : pageFile(url.pathname);
    const publicTarget = join(distRoot, normalize(url.pathname).replace(/^\//, ''));
    assert.ok(existsSync(target) || existsSync(publicTarget), `${file} links to missing internal target: ${href}`);
  }
}

const homepage = readFileSync(pageFile('/'), 'utf8');
assert.ok(homepage.includes(`${companyOrigin}/#organization`), 'homepage publisher must use the configured Willuny organization ID');
assert.ok(homepage.includes(`${origin}/#product-family`), 'homepage must declare the configured Amsonia product family');
assert.ok(homepage.includes(`${origin}/platform/#software`), 'homepage must reference Amsonia Platform');
assert.ok(homepage.includes(`${origin}/next/#software`), 'homepage must reference Amsonia Next');
assert.match(homepage, /FAMILY \/ 00/, 'homepage must present Amsonia as the product family');
assert.match(homepage, /Amsonia Platform/, 'homepage must name the commercial product consistently');
assert.match(homepage, /Amsonia Next/, 'homepage must name the Next.js product consistently');

const sitemapFile = join(distRoot, 'sitemap.xml');
assert.ok(existsSync(sitemapFile), 'sitemap.xml must be generated');
const sitemap = readFileSync(sitemapFile, 'utf8');
for (const route of requiredRoutes) {
  assert.ok(sitemap.includes(`<loc>${canonicalFor(route)}</loc>`), `sitemap missing ${route}`);
}
assert.doesNotMatch(sitemap, /demo\.amsonia\.dev|\/404|<loc>[^<]*\?/, 'sitemap must contain only clean indexable URLs');
assert.doesNotMatch(sitemap, /\/core(?:\/|<)|\/releases(?:\/|<)/, 'sitemap must exclude retired Core and release routes');

const redirectsFile = join(distRoot, '_redirects');
assert.ok(existsSync(redirectsFile), 'Cloudflare Pages redirects must be generated');
const redirects = readFileSync(redirectsFile, 'utf8');
assert.match(redirects, /^\/core\s+https:\/\/github\.com\/willunylabs\/amsonia-core\s+301$/m, 'the retired Core root must redirect to GitHub');
assert.match(redirects, /^\/core\/\*\s+https:\/\/github\.com\/willunylabs\/amsonia-core\s+301$/m, 'retired Core descendants must redirect to GitHub');

const robots = readFileSync(join(distRoot, 'robots.txt'), 'utf8');
assert.match(robots, /User-agent: \*\s+Allow: \//, 'robots.txt must allow crawling');
assert.ok(robots.includes(`Sitemap: ${origin}/sitemap.xml`), 'robots.txt must advertise the configured canonical sitemap');

console.log(`Amsonia site checks passed for ${requiredRoutes.length} routes and ${htmlFiles.length} HTML files.`);
