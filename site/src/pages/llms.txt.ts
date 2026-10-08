import type { APIRoute } from 'astro';
import { absoluteUrl, COMPANY_ORIGIN, COMPANY_CONTACT_URL, COMMERCIAL_URL, GITHUB_ORGANIZATION_URL, SITE_ORIGIN } from '../lib/site';

export const GET: APIRoute = () => {
  const content = [
    '# Amsonia',
    '',
    'Amsonia is the SaaS product family published by Willuny Labs LLC.',
    'Our main product focus is Amsonia Next: a private Next.js SaaS starter with authentication, Stripe billing, PostgreSQL, and admin in one TypeScript application.',
    'The current Next application is single-tenant. Contact Willuny Labs about source availability, pricing, licensing, and support. It is not an open-source distribution.',
    'Amsonia Platform is a separate commercial Go + Next.js foundation for multi-tenant SaaS.',
    '',
    `- Product home: ${absoluteUrl('/')}`,
    `- Products: ${absoluteUrl('/products')}`,
    `- Amsonia Next: ${absoluteUrl('/next')}`,
    `- Amsonia Platform: ${absoluteUrl('/platform')}`,
    `- Documentation: ${absoluteUrl('/docs')}`,
    `- Platform architecture: ${absoluteUrl('/architecture')}`,
    `- Platform security: ${absoluteUrl('/security')}`,
    `- Public engineering: ${absoluteUrl('/open-source')}`,
    `- Company: ${COMPANY_ORIGIN}/company`,
    `- Contact: ${COMPANY_CONTACT_URL}`,
    `- Willuny Labs on GitHub: ${GITHUB_ORGANIZATION_URL}`,
    `- Platform commercial packages: ${COMMERCIAL_URL}`,
    '',
    `Canonical origin: ${SITE_ORIGIN}`,
    'This file is a convenience index for developer tools. Canonical HTML pages and sitemap.xml remain the authoritative public sources.',
    ''
  ].join('\n');
  return new Response(content, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
