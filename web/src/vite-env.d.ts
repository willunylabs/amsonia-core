/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_PUBLIC_DEMO_VIEWER_EMAIL?: string;
  readonly VITE_PUBLIC_DEMO_VIEWER_PASSWORD?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
