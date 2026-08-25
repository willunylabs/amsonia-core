import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const siteRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const styles = readFileSync(join(siteRoot, 'src', 'styles', 'global.css'), 'utf8');
const mark = readFileSync(join(siteRoot, 'public', 'amsonia-mark-v2.svg'), 'utf8');
const header = readFileSync(join(siteRoot, 'src', 'components', 'SiteHeader.astro'), 'utf8');
const footer = readFileSync(join(siteRoot, 'src', 'components', 'SiteFooter.astro'), 'utf8');

for (const token of ['#635bff', '#7a73ff', '#0f172a', '#1d1b3f']) {
  assert.ok(styles.toLowerCase().includes(token), `global.css must retain the shared Willuny token ${token}`);
}

for (const retired of ['#203b2e', '#d7ef72', '#f3efe4', '--serif']) {
  assert.ok(!styles.toLowerCase().includes(retired), `global.css must not restore retired Amsonia style ${retired}`);
}

const petalPath = 'M32 31C24.8 23.2 25.2 11.7 32 2.5C38.8 11.7 39.2 23.2 32 31Z';
assert.equal(mark.split(petalPath).length - 1, 5, 'Amsonia mark must keep the shared five-petal geometry');
for (const angle of [72, 144, 216, 288]) {
  assert.ok(mark.includes(`rotate(${angle} 32 32)`), `Amsonia mark must keep the ${angle}-degree petal rotation`);
}
assert.match(mark, /<g fill="#635bff">/, 'Amsonia petals must use the shared Willuny purple');
assert.match(mark, /fill="#7f9d7c"/, 'Amsonia mark must keep the shared leaf color');
assert.match(mark, /<circle cx="32" cy="32" r="4\.2" fill="#f8fafc"\/>/, 'Amsonia mark must keep the shared center');

for (const [name, source] of [['header', header], ['footer', footer]]) {
  assert.ok(source.includes('/amsonia-mark-v2.svg'), `${name} must use the versioned canonical shared mark`);
  assert.ok(!source.includes('/amsonia-mark.svg'), `${name} must not restore the immutable cached v1 mark path`);
}

console.log('Amsonia visual system matches the shared Willuny brand contract.');
