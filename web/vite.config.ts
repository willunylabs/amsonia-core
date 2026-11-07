import { defineConfig } from "vitest/config";
import { loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const proxyTarget = process.env.VITE_API_PROXY_TARGET || env.VITE_API_PROXY_TARGET || "http://127.0.0.1:8080";
  const prefix = process.env.VITE_API_URL || env.VITE_API_URL || "";
  if (!/^(?:\/[A-Za-z0-9._~-]+)*$/.test(prefix) || prefix.split("/").some(part => part === "." || part === "..")) {
    throw new Error("VITE_API_URL must be an absolute same-origin path prefix without a trailing slash");
  }
  return {
    plugins: [react()],
    test: {
      environment: "jsdom"
    },
    server: {
      proxy: {
        [`${prefix}/api`]: { target: proxyTarget, rewrite: (path) => path.slice(prefix.length) },
        "/health": proxyTarget
      }
    }
  };
});
