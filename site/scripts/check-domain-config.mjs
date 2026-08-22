import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, extname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const siteRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = join(siteRoot, '..');
const roots = [join(siteRoot, 'src'), join(repoRoot, 'web', 'src')];
const extensions = new Set(['.astro', '.js', '.mjs', '.ts', '.tsx']);
const productionDomain = /(?:willuny\.(?:xyz|com)|amsonia\.dev)/i;

function collect(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    if (statSync(path).isDirectory()) return collect(path);
    return extensions.has(extname(name)) ? [path] : [];
  });
}

for (const root of roots) {
  for (const file of collect(root)) {
    assert.doesNotMatch(
      readFileSync(file, 'utf8'),
      productionDomain,
      `${relative(repoRoot, file)} must receive production domains from build configuration`
    );
  }
}

console.log('Amsonia runtime source contains no production domain defaults.');
