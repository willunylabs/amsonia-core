export const SITE_ORIGIN = 'https://amsonia.dev';
export const SITE_NAME = 'Amsonia';
export const COMPANY_NAME = 'Willuny Labs LLC';
export const GITHUB_URL = 'https://github.com/willunylabs/amsonia-core';
export const DEMO_URL = 'https://demo.amsonia.dev';
export const COMMERCIAL_URL = 'https://willuny.xyz/amsonia';

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
  '@id': 'https://willuny.xyz/#organization',
  name: COMPANY_NAME,
  url: 'https://willuny.xyz/'
};

export const amsoniaBrandReference = {
  '@type': 'Brand',
  '@id': `${SITE_ORIGIN}/#brand`,
  name: SITE_NAME,
  url: `${SITE_ORIGIN}/`
};
