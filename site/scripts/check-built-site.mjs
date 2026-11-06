import assert from 'node:assert/strict';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

const siteRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const distRoot = join(siteRoot, 'dist');
const publicLinks = JSON.parse(readFileSync(join(siteRoot, 'config/public-links.json'), 'utf8'));
const nextDemoUrl = new URL(publicLinks.nextDemo).href.replace(/\/$/, '');
assert.equal(new URL(nextDemoUrl).protocol, 'https:', 'the public demo must use HTTPS');
const origin = String(process.env.PUBLIC_SITE_ORIGIN || '').trim();
assert.ok(origin, 'PUBLIC_SITE_ORIGIN is required for the site audit');
const companyOrigin = String(process.env.PUBLIC_COMPANY_ORIGIN || '').trim();
assert.ok(companyOrigin, 'PUBLIC_COMPANY_ORIGIN is required for the site audit');
const commercialUrl = new URL(process.env.PUBLIC_COMMERCIAL_URL);
assert.notEqual(commercialUrl.href.replace(/\/$/, ''), `${companyOrigin}/amsonia`, 'commercial CTA must not loop through the retired company product page');
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
  const image = html.match(/<meta property="og:image" content="([^"]+)">/)?.[1];
  assert.ok(image?.startsWith(`${origin}/`) && image.endsWith('.png'), `${route} must share a same-origin PNG`);
  assert.ok(html.includes(`<meta name="twitter:image" content="${image}">`), `${route} must share the same image on X`);
  assert.match(html, /<meta property="og:image:width" content="1200">/, `${route} must declare the image width`);
  assert.match(html, /<meta property="og:image:height" content="630">/, `${route} must declare the image height`);
  assert.match(html, /<meta property="og:image:type" content="image\/png">/, `${route} must declare the image type`);
  assert.match(html, /<meta name="robots" content="index,follow,/, `${route} must be indexable`);
  assert.doesNotMatch(html, /noindex/i, `${route} must not contain noindex`);

  for (const match of html.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)) {
    assert.doesNotThrow(() => JSON.parse(match[1]), `${route} contains invalid JSON-LD`);
  }
}

const htmlFiles = collectHtml(distRoot);
const errorHtml = readFileSync(pageFile('/404'), 'utf8');
assert.match(errorHtml, /<meta name="robots" content="noindex,follow"/, 'the error document must remain excluded even when requested directly');
assert.doesNotMatch(errorHtml, /<link rel="canonical"/, 'missing pages must not share a canonical error-document URL');

for (const file of htmlFiles) {
  const html = readFileSync(file, 'utf8');
  const demoLinks = [...html.matchAll(/<a\b[^>]*href="([^"]+)"[^>]*data-cta-target="demo"/g)];
  if (file === pageFile('/next')) {
    assert.equal(demoLinks.length, 1, 'only one verified demo CTA belongs on the Next product page');
    assert.equal(demoLinks[0]?.[1], nextDemoUrl, 'the demo CTA must use its reviewed build configuration');
    assert.ok(html.includes('Public preview. Registration, email and payments are disabled.'), 'the demo limits must be visible beside its entry');
    assert.ok(html.includes('Better Auth'), 'Next must name its current authentication library');
  } else {
    assert.equal(demoLinks.length, 0, `${file} must not advertise a demo for another product`);
  }
  const domainAuditHtml = file === pageFile('/next') ? html.replace(`href="${nextDemoUrl}"`, 'href="[verified-next-demo]"') : html;
  assert.doesNotMatch(domainAuditHtml, /willuny\.xyz|willunylabs\.com/i, `${file} must not publish an unapproved legacy Willuny domain`);
  assert.doesNotMatch(html, /github\.com\/willunylabs\/amsonia-next|opensource\.org\/license\/mit|MIT-licensed/i, `${file} must not promote private Next source as an open-source release`);
  assert.doesNotMatch(html, /Complete Amsonia|Commercial Amsonia|Amsonia Full/, `${file} must use the accepted product names`);
  assert.doesNotMatch(html, /demo\.amsonia\.dev|Live demo/i, `${file} must not present an unverified hosted demo as product evidence`);
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

// A sitemap can expose pages that readers cannot reach. Verify the actual
// anchor graph, ignoring fragment/self links, rather than only target existence.
const routesByPath = new Map(requiredRoutes.map((route) => [new URL(canonicalFor(route)).pathname, route]));
const pending = ['/'];
const reachable = new Set(pending);
while (pending.length > 0) {
  const route = pending.shift();
  const html = readFileSync(pageFile(route), 'utf8');
  for (const match of html.matchAll(/<a\b[^>]*\bhref="([^"]+)"/g)) {
    const target = new URL(match[1], canonicalFor(route));
    if (target.origin !== new URL(origin).origin) continue;
    const next = routesByPath.get(target.pathname);
    if (next !== undefined && !reachable.has(next)) {
      reachable.add(next);
      pending.push(next);
    }
  }
}
assert.deepEqual(requiredRoutes.filter((route) => !reachable.has(route)), [], 'every sitemap page needs a crawlable path from the homepage');

const homepage = readFileSync(pageFile('/'), 'utf8');
for (const image of ['og-amsonia.png']) {
  const png = readFileSync(join(distRoot, image));
  assert.equal(png.subarray(0, 8).toString('hex'), '89504e470d0a1a0a', `${image} must be a real PNG`);
  assert.equal(png.readUInt32BE(16), 1200, `${image} must be 1200 pixels wide`);
  assert.equal(png.readUInt32BE(20), 630, `${image} must be 630 pixels tall`);
}
const homepageGraph = [...homepage.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)]
  .flatMap((match) => JSON.parse(match[1])['@graph'] ?? []);
assert.ok(!homepageGraph.some((entry) => entry['@type'] === 'ProductGroup'), 'distinct software products must not be marked as merchant variants');
const catalog = homepageGraph.find((entry) => entry['@type'] === 'CollectionPage');
assert.equal(catalog?.mainEntity?.['@type'], 'ItemList', 'the homepage must describe its product catalog as a list');
assert.deepEqual(catalog.mainEntity.itemListElement.map((entry) => entry.url), [canonicalFor('/next'), canonicalFor('/platform')], 'catalog entries must point to the actual product pages');
assert.ok(homepage.includes(`${companyOrigin}/logo`), 'publisher logo must use the company logo');
assert.match(homepage, /github\.com\/willunylabs/, 'publisher must identify the GitHub organization');
assert.ok(homepage.includes(`${companyOrigin}/#organization`), 'homepage publisher must use the configured Willuny organization ID');
assert.ok(homepage.includes(`${origin}/#product-family`), 'homepage must declare the configured Amsonia product family');
assert.ok(homepage.includes(`${origin}/platform/#software`), 'homepage must reference Amsonia Platform');
assert.ok(homepage.includes(`${origin}/next/#software`), 'homepage must reference Amsonia Next');
assert.match(homepage, /FAMILY \/ 00/, 'homepage must present Amsonia as the product family');
assert.match(homepage, /Amsonia Platform/, 'homepage must name the commercial product consistently');
assert.match(homepage, /Amsonia Next/, 'homepage must name the Next.js product consistently');

for (const route of ['/', '/next', '/products', '/pricing', '/compare/platform-vs-next']) {
  const html = readFileSync(pageFile(route), 'utf8');
  assert.doesNotMatch(html, /Lemon Squeezy|Polar/i, `${route} must not advertise unsupported Next payment providers`);
}

const sitemapFile = join(distRoot, 'sitemap.xml');
assert.ok(existsSync(sitemapFile), 'sitemap.xml must be generated');
const sitemap = readFileSync(sitemapFile, 'utf8');
for (const route of requiredRoutes) {
  assert.ok(sitemap.includes(`<loc>${canonicalFor(route)}</loc>`), `sitemap missing ${route}`);
}
assert.ok(!sitemap.includes(new URL(nextDemoUrl).origin), 'the demo is external and must not enter the product sitemap');
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
