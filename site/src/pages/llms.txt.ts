import type { APIRoute } from 'astro';
import { AMSONIA_NEXT_GITHUB_URL, COMMERCIAL_URL, SITE_ORIGIN } from '../lib/site';

export const GET: APIRoute = () => {
  const content = [
    '# Amsonia',
    '',
    'Amsonia is a product family for source-owned SaaS foundations.',
    '',
    `- Product home: ${SITE_ORIGIN}/`,
    `- Products: ${SITE_ORIGIN}/products`,
    `- Amsonia Platform: ${SITE_ORIGIN}/platform`,
    `- Amsonia Next: ${SITE_ORIGIN}/next`,
    `- Amsonia Next source: ${AMSONIA_NEXT_GITHUB_URL}`,
    `- Documentation: ${SITE_ORIGIN}/docs`,
    `- Platform architecture: ${SITE_ORIGIN}/architecture`,
    `- Platform security: ${SITE_ORIGIN}/security`,
    `- Commercial distribution: ${COMMERCIAL_URL}`,
    '',
    'This file is a convenience index for developer tools. Canonical HTML pages and sitemap.xml remain the authoritative public sources.',
    ''
  ].join('\n');
  return new Response(content, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
