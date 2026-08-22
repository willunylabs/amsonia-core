import { defineConfig } from 'astro/config';

const site = String(process.env.PUBLIC_SITE_ORIGIN || '').trim();
if (!site) {
  throw new Error('PUBLIC_SITE_ORIGIN is required');
}

export default defineConfig({
  site,
  output: 'static',
  trailingSlash: 'never',
  build: {
    format: 'directory'
  }
});
