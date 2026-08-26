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
export const COMPANY_ORIGIN = requiredURL('PUBLIC_COMPANY_ORIGIN', import.meta.env.PUBLIC_COMPANY_ORIGIN);
export const GITHUB_URL = requiredURL('PUBLIC_GITHUB_URL', import.meta.env.PUBLIC_GITHUB_URL);
export const COMMERCIAL_URL = requiredURL('PUBLIC_COMMERCIAL_URL', import.meta.env.PUBLIC_COMMERCIAL_URL);

export const primaryNav = [
  { href: '/core', label: 'Core' },
  { href: '/docs', label: 'Docs' },
  { href: '/releases', label: 'Releases' },
  { href: '/open-source', label: 'Open source' },
  { href: '/about', label: 'About' }
];

export type Breadcrumb = {
  name: string;
  href: string;
};

export const absoluteUrl = (path: string) => new URL(path, SITE_ORIGIN).toString();

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
  name: COMPANY_NAME,
  url: `${COMPANY_ORIGIN}/`
};

export const amsoniaBrandReference = {
  '@type': 'Brand',
  '@id': `${SITE_ORIGIN}/#brand`,
  name: SITE_NAME,
  url: `${SITE_ORIGIN}/`
};

export const amsoniaProductReference = {
  '@type': 'SoftwareApplication',
  '@id': `${SITE_ORIGIN}/#product`,
  name: SITE_NAME,
  url: `${SITE_ORIGIN}/`
};
