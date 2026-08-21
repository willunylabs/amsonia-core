import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("public brand links", () => {
  const source = readFileSync("src/App.tsx", "utf8");

  it("links the commercial product directly on the canonical domain", () => {
    expect(source).toContain("https://willuny.com/amsonia");
    expect(source).toContain("Amsonia Source Distribution");
    expect(source).not.toContain("willuny.xyz");
    expect(source).not.toMatch(/Complete Amsonia|Commercial Amsonia|Amsonia Full/i);
  });
});
