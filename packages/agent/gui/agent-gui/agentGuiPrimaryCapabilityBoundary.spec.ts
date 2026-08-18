import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const domainWord = `${"connect"}${"or"}`;

describe("AgentGUI primary capability boundary", () => {
  it("has no dependency on a product-owned capability package", () => {
    const packageJson = readFileSync(join(packageRoot, "package.json"), "utf8");
    expect(packageJson).not.toContain(`@tutti-os/${domainWord}-`);
  });

  it("keeps product domain names and wire fields out of production sources", () => {
    const roots = [packageRoot];
    for (const filePath of roots.flatMap(sourceFiles)) {
      expect(filePath.toLowerCase(), filePath).not.toContain(domainWord);
      expect(
        readFileSync(filePath, "utf8").toLowerCase(),
        filePath,
      ).not.toContain(domainWord);
    }
  });
});

function sourceFiles(path: string): string[] {
  if (/\.(?:ts|tsx)$/u.test(path) && !/\.spec\./u.test(path)) return [path];
  try {
    return readdirSync(path, { withFileTypes: true }).flatMap((entry) => {
      if (entry.name === "dist" || entry.name === "node_modules") return [];
      return sourceFiles(join(path, entry.name));
    });
  } catch {
    return [];
  }
}
