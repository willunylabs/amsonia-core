import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('Pages shadow deployment is manual and protected', () => {
  const workflow = read('.github/workflows/deploy-site-pages.yml');

  assert.match(workflow, /^on:\n  workflow_dispatch:/m);
  assert.doesNotMatch(workflow, /^  push:/m);
  assert.doesNotMatch(workflow, /^  workflow_run:/m);
  assert.match(workflow, /environment: amsonia-production/);
  assert.match(workflow, /CLOUDFLARE_API_TOKEN: \$\{\{ secrets\.CLOUDFLARE_API_TOKEN \}\}/);
  assert.match(workflow, /npx --yes wrangler@4\.84\.1 pages deploy dist/);
  assert.match(workflow, /--branch main/);
  assert.doesNotMatch(workflow, /aws |ssm |EC2|INSTANCE_ID/);
});

test('Pages deployment retains real 404s and security headers', () => {
  const workflow = read('.github/workflows/deploy-site-pages.yml');
  const headers = read('site/public/_headers');

  assert.match(workflow, /deployment-probe-not-found/);
  assert.match(workflow, /wait_for_status 200/);
  assert.match(workflow, /wait_for_status 404/);
  assert.match(workflow, /for attempt in \$\(seq 1 15\)/);
  assert.match(headers, /Strict-Transport-Security: max-age=31536000; includeSubDomains/);
  assert.match(headers, /X-Content-Type-Options: nosniff/);
  assert.match(headers, /X-Frame-Options: DENY/);
  assert.match(headers, /\/_astro\/\*/);
  assert.match(headers, /Cache-Control: public, max-age=31536000, immutable/);
});
