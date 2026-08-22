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
});
