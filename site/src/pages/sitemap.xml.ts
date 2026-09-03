import type { APIRoute } from 'astro';
import { absoluteUrl } from '../lib/site';

const routes = [
  '/',
  '/products',
  '/platform',
  '/platform/docs',
  '/platform/docs/quickstart',
  '/platform/docs/deployment',
  '/platform/docs/billing',
  '/platform/changelog',
  '/platform/roadmap',
  '/next',
  '/compare/platform-vs-next',
  '/go-saas-boilerplate',
  '/best-go-saas-boilerplates',
  '/features/multi-tenancy',
  '/features/rbac',
  '/features/stripe-billing',
  '/architecture',
  '/security',
  '/pricing',
  '/license',
  '/docs',
  '/open-source',
  '/about'
];

const escapeXml = (value: string) => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');

export const GET: APIRoute = () => {
  const entries = routes.map((route) => `  <url>\n    <loc>${escapeXml(absoluteUrl(route))}</loc>\n    <lastmod>2026-09-03</lastmod>\n  </url>`).join('\n');
  return new Response(`<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${entries}\n</urlset>\n`, {
    headers: { 'Content-Type': 'application/xml; charset=utf-8' }
  });
};
