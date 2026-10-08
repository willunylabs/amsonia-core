function requiredURL(name: string, rawValue: string | undefined): string {
  const value = String(rawValue || '').trim();
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') throw new Error('unsupported protocol');
    return parsed.toString().replace(/\/$/, '');
  } catch {
    throw new Error(`${name} must be configured with an absolute HTTP(S) URL`);
  }
}

export const SITE_ORIGIN = requiredURL('PUBLIC_SITE_ORIGIN', import.meta.env.PUBLIC_SITE_ORIGIN);
export const SITE_NAME = 'Amsonia';
export const COMPANY_NAME = 'Willuny Labs LLC';
export const COMPANY_DISPLAY_NAME = 'Willuny Labs';
export const COMPANY_FOUNDING_DATE = '2026-01-07';
export const COMPANY_OPENCORPORATES_URL = 'https://opencorporates.com/companies/us_wy/2026-001860652';
export const COMPANY_CRUNCHBASE_URL = 'https://www.crunchbase.com/organization/willuny-labs';
export const COMPANY_DESCRIPTION = 'Willuny Labs builds Amsonia Next, a private Next.js SaaS starter, and maintains practical engineering work and open-source foundations.';
export const GITHUB_ORGANIZATION_URL = 'https://github.com/willunylabs';
export const COMPANY_ORIGIN = requiredURL('PUBLIC_COMPANY_ORIGIN', import.meta.env.PUBLIC_COMPANY_ORIGIN);
export const GITHUB_URL = requiredURL('PUBLIC_GITHUB_URL', import.meta.env.PUBLIC_GITHUB_URL);
export const COMMERCIAL_URL = requiredURL('PUBLIC_COMMERCIAL_URL', import.meta.env.PUBLIC_COMMERCIAL_URL);
export const COMPANY_CONTACT_URL = `${COMPANY_ORIGIN}/shop/contact`;

export const primaryNav = [
  { href: '/next/', label: 'Next' },
  { href: '/platform/', label: 'Platform' },
  { href: '/compare/platform-vs-next/', label: 'Compare' },
  { href: '/docs/', label: 'Docs' },
  { href: '/open-source/', label: 'Open source' }
];

export type Breadcrumb = {
  name: string;
  href: string;
};

export function absoluteUrl(path: string): string {
  const url = new URL(path, SITE_ORIGIN);
  const lastSegment = url.pathname.split('/').at(-1) || '';
  const isFile = lastSegment.includes('.');
  if (url.pathname !== '/' && !url.pathname.endsWith('/') && !isFile) {
    url.pathname += '/';
  }
  return url.toString();
}

export function breadcrumbSchema(items: Breadcrumb[]) {
  return {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: items.map((item, index) => ({
      '@type': 'ListItem',
      position: index + 1,
      name: item.name,
      item: absoluteUrl(item.href)
    }))
  };
}

export const publisherReference = {
  '@type': 'Organization',
  '@id': `${COMPANY_ORIGIN}/#organization`,
  name: COMPANY_DISPLAY_NAME,
  legalName: COMPANY_NAME,
  alternateName: ['Willuny', COMPANY_NAME],
  url: `${COMPANY_ORIGIN}/`,
  logo: `${COMPANY_ORIGIN}/logo`,
  description: COMPANY_DESCRIPTION,
  foundingDate: COMPANY_FOUNDING_DATE,
  sameAs: [GITHUB_ORGANIZATION_URL, COMPANY_OPENCORPORATES_URL, COMPANY_CRUNCHBASE_URL]
};

export const amsoniaBrandReference = {
  '@type': 'Brand',
  '@id': `${SITE_ORIGIN}/#brand`,
  name: SITE_NAME,
  url: `${SITE_ORIGIN}/`
};

export const amsoniaProductReference = {
  '@type': 'CollectionPage',
  '@id': `${SITE_ORIGIN}/#product-family`,
  name: SITE_NAME,
  url: `${SITE_ORIGIN}/`
};

export function softwareProductSchema({
  name,
  path,
  description,
  license,
  isAccessibleForFree
}: {
  name: string;
  path: string;
  description: string;
  license?: string;
  isAccessibleForFree?: boolean;
}) {
  return {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    '@id': `${absoluteUrl(path)}#software`,
    name,
    url: absoluteUrl(path),
    description,
    applicationCategory: 'DeveloperApplication',
    operatingSystem: 'Self-hosted',
    ...(license ? { license } : {}),
    ...(isAccessibleForFree !== undefined ? { isAccessibleForFree } : {}),
    brand: { '@id': `${SITE_ORIGIN}/#brand` },
    publisher: { '@id': publisherReference['@id'] },
    isPartOf: { '@id': `${SITE_ORIGIN}/#product-family` }
  };
}
