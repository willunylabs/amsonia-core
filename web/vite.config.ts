import { defineConfig } from "vitest/config";
import { loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const proxyTarget = process.env.VITE_API_PROXY_TARGET || env.VITE_API_PROXY_TARGET || "http://127.0.0.1:8080";
  return {
    plugins: [react()],
    test: {
      environment: "jsdom"
    },
    server: {
      proxy: {
        "/api": proxyTarget,
        "/health": proxyTarget
      }
    }
  };
});
