import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("public brand links", () => {
  const source = readFileSync("src/App.tsx", "utf8");

  it("links the commercial product through deployment configuration", () => {
    expect(source).toContain('import { COMMERCIAL_URL, SOURCE_URL } from "./config"');
    expect(source).toContain('href={COMMERCIAL_URL}');
    expect(source).toContain("Amsonia Source Distribution");
    expect(source).not.toMatch(/Complete Amsonia|Commercial Amsonia|Amsonia Full/i);
  });

  it("uses the versioned Amsonia product mark across the console", () => {
    expect(source.match(/\/amsonia-mark-v2\.svg/g)).toHaveLength(3);
    expect(source).not.toContain("Sparkles");
  });

  it("shares the Amsonia product color system", () => {
    const styles = readFileSync("src/styles.css", "utf8");
    const document = readFileSync("index.html", "utf8");

    expect(styles).toContain("--brand: #635bff");
    expect(styles).toContain("--navy: #1d1b3f");
    expect(styles).not.toMatch(/#203b2e|#d7ef72|--moss|--acid/);
    expect(document).toContain('href="/amsonia-mark-v2.svg"');
    expect(document).not.toContain('href="/amsonia-mark.svg"');
  });
});
