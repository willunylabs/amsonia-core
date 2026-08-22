import type { APIRoute } from 'astro';
import { SITE_ORIGIN } from '../lib/site';

export const GET: APIRoute = () => new Response(
  `User-agent: *\nAllow: /\n\nSitemap: ${SITE_ORIGIN}/sitemap.xml\n`,
  { headers: { 'Content-Type': 'text/plain; charset=utf-8' } }
);
