// Bake the same external API prefix into the static image and its proxy route.
import { readFileSync, writeFileSync } from "node:fs";

const prefix = process.env.VITE_API_URL ?? "";
if (!/^(?:\/[A-Za-z0-9._~-]+)*$/.test(prefix) || prefix.split("/").some(p => p === "." || p === "..")) {
  throw new Error("VITE_API_URL must be an absolute same-origin path prefix without a trailing slash");
}
const [file] = process.argv.slice(2);
const config = readFileSync(file, "utf8");
if (!config.includes("location /api/ {")) throw new Error("Missing API proxy route");
writeFileSync(file, config.replace("location /api/ {", `location ${prefix}/api/ {`));
