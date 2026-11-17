import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { checkRedirects } from './check-redirects.mjs';

const delivered = readFileSync(new URL('../public/_redirects', import.meta.url), 'utf8');
const rule = '/architecture /next/ 301';

test('accepts the shipped redirect manifest, comments and whitespace', () => {
  checkRedirects(delivered);
  checkRedirects('# Retired public routes\r\n\r\n' + delivered.replaceAll(' ', '\t').replaceAll('\n', '\r\n'));
});

const invalid = {
  'commented-out route': delivered.replace(rule, `# ${rule}`),
  'missing trailing-slash route': delivered.replace('/architecture/ /next/ 301\n', ''),
  'malformed status': delivered.replace(rule, '/architecture /next/ 3010'),
  'temporary redirect': delivered.replace(rule, '/architecture /next/ 302'),
  'wrong destination': delivered.replace(rule, '/architecture /products/ 301'),
  'duplicate with wrong target first': `/architecture /wrong/ 302\n${delivered}`,
  'duplicate with wrong target last': `${delivered}/architecture /wrong/ 302\n`,
  'missing Platform wildcard': delivered.replace('/platform/* /next/ 301\n', ''),
  'missing release route': delivered.replace(/^\/releases .*\n/m, ''),
  'missing Core descendants': delivered.replace(/^\/core\/\* .*\n/m, ''),
  'unreviewed catch-all': `/* /next/ 301\n${delivered}`
};

for (const [name, contents] of Object.entries(invalid)) {
  test(`rejects ${name}`, () => {
    assert.notEqual(contents, delivered, 'the fixture must change the shipped configuration');
    assert.throws(() => checkRedirects(contents), /public redirects must match/);
  });
}
