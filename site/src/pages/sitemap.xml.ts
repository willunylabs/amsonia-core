import type { APIRoute } from 'astro';
import { SITE_ORIGIN } from '../lib/site';

const routes = [
  '/',
  '/core',
  '/core/docs',
  '/core/docs/getting-started',
  '/core/api',
  '/core/security',
  '/core/license',
  '/docs',
  '/releases',
  '/open-source',
  '/about'
];

const escapeXml = (value: string) => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');

export const GET: APIRoute = () => {
  const entries = routes.map((route) => `  <url>\n    <loc>${escapeXml(new URL(route, SITE_ORIGIN).toString())}</loc>\n    <lastmod>2026-08-17</lastmod>\n  </url>`).join('\n');
  return new Response(`<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${entries}\n</urlset>\n`, {
    headers: { 'Content-Type': 'application/xml; charset=utf-8' }
  });
};
