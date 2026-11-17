import assert from 'node:assert/strict';

export const retiredProductRoutes = [
  '/platform', '/platform/docs', '/platform/docs/quickstart',
  '/platform/docs/deployment', '/platform/docs/billing', '/platform/changelog',
  '/platform/roadmap', '/compare/platform-vs-next', '/go-saas-boilerplate',
  '/best-go-saas-boilerplates', '/features/multi-tenancy', '/features/rbac',
  '/features/stripe-billing', '/architecture', '/security'
];

export function checkRedirects(contents) {
  const actual = contents.split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'))
    .map((line) => line.split(/\s+/).join(' '))
    .sort();
  const repository = 'https://github.com/willunylabs/amsonia-core';
  // Keep the published redirect manifest explicit: an extra/duplicate rule can
  // shadow a valid rule, so checking only for its presence is insufficient.
  const expected = [
    `/releases ${repository}/releases 301`,
    `/releases/ ${repository}/releases 301`,
    `/core ${repository} 301`,
    `/core/* ${repository} 301`,
    ...retiredProductRoutes.flatMap((route) => [
      `${route} /next/ 301`, `${route}/ /next/ 301`
    ]),
    '/platform/* /next/ 301'
  ].sort();
  assert.deepEqual(actual, expected, 'public redirects must match the reviewed source, destination and status contract');
}
