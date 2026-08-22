import type { APIRoute } from 'astro';
import { COMMERCIAL_URL, SITE_ORIGIN } from '../lib/site';

export const GET: APIRoute = () => {
  const content = [
    '# Amsonia',
    '',
    'Amsonia is a technical product family for modern Go applications.',
    '',
    `- Product home: ${SITE_ORIGIN}/`,
    `- Amsonia Core: ${SITE_ORIGIN}/core`,
    `- Documentation: ${SITE_ORIGIN}/docs`,
    `- Getting started: ${SITE_ORIGIN}/core/docs/getting-started`,
    `- API contract: ${SITE_ORIGIN}/core/api`,
    `- Security model: ${SITE_ORIGIN}/core/security`,
    `- Releases: ${SITE_ORIGIN}/releases`,
    `- Commercial distribution: ${COMMERCIAL_URL}`,
    '',
    'This file is a convenience index for developer tools. Canonical HTML pages and sitemap.xml remain the authoritative public sources.',
    ''
  ].join('\n');
  return new Response(content, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
