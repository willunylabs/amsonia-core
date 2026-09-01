import { defineConfig } from 'astro/config';

const site = String(process.env.PUBLIC_SITE_ORIGIN || '').trim();
if (!site) {
  throw new Error('PUBLIC_SITE_ORIGIN is required');
}

export default defineConfig({
  site,
  output: 'static',
  // Cloudflare Pages serves directory-format routes at their trailing-slash
  // URLs. Generate the same URL shape so canonicals never point at a 308.
  trailingSlash: 'always',
  build: {
    format: 'directory'
  }
});
