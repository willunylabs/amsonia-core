import { defineConfig } from 'astro/config';

export default defineConfig({
  site: 'https://amsonia.dev',
  output: 'static',
  trailingSlash: 'never',
  build: {
    format: 'directory'
  }
});
