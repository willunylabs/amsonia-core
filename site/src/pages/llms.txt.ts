import type { APIRoute } from 'astro';
import { absoluteUrl, COMPANY_ORIGIN, COMPANY_CONTACT_URL, COMMERCIAL_URL, GITHUB_ORGANIZATION_URL, SITE_ORIGIN } from '../lib/site';

export const GET: APIRoute = () => {
  const content = [
    '# Amsonia',
    '',
    'Amsonia is the SaaS product family published by Willuny Labs LLC.',
    'Amsonia Platform is the commercial Go + Next.js foundation for multi-tenant SaaS.',
    'Amsonia Next is a privately maintained single-tenant Next.js foundation, not an open-source distribution.',
    '',
    `- Product home: ${absoluteUrl('/')}`,
    `- Products: ${absoluteUrl('/products')}`,
    `- Amsonia Platform: ${absoluteUrl('/platform')}`,
    `- Amsonia Next: ${absoluteUrl('/next')}`,
    `- Documentation: ${absoluteUrl('/docs')}`,
    `- Platform architecture: ${absoluteUrl('/architecture')}`,
    `- Platform security: ${absoluteUrl('/security')}`,
    `- Public engineering: ${absoluteUrl('/open-source')}`,
    `- Company: ${COMPANY_ORIGIN}/company`,
    `- Contact: ${COMPANY_CONTACT_URL}`,
    `- Willuny Labs on GitHub: ${GITHUB_ORGANIZATION_URL}`,
    `- Commercial distribution: ${COMMERCIAL_URL}`,
    '',
    `Canonical origin: ${SITE_ORIGIN}`,
    'This file is a convenience index for developer tools. Canonical HTML pages and sitemap.xml remain the authoritative public sources.',
    ''
  ].join('\n');
  return new Response(content, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
