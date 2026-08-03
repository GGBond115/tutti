import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  countWorkspaceAppsInCategory,
  filterWorkspaceAppsByCategory,
  normalizeWorkspaceAppCategoryId,
  resolveWorkspaceAppCategoryId
} from "./workspaceAppCategory.ts";

describe("resolveWorkspaceAppCategoryId", () => {
  it("normalizes app ids before lookup", () => {
    assert.equal(resolveWorkspaceAppCategoryId(" AI-DOC "), "office");
  });
});

describe("normalizeWorkspaceAppCategoryId", () => {
  it("accepts stable ids and rejects presentation labels", () => {
    assert.equal(normalizeWorkspaceAppCategoryId(" TOOLS "), "tools");
    assert.equal(normalizeWorkspaceAppCategoryId("其他工具"), null);
  });
});

describe("workspace app category projection", () => {
  const apps = [
    { categoryId: "product-design" as const, id: "stable" },
    { category: "Product design", id: "legacy-en" },
    { category: "产品设计", id: "legacy-zh" },
    { category: "Unknown", id: "unknown" },
    {
      category: "Product design",
      categoryId: "tools" as const,
      id: "stable-wins"
    }
  ];

  it("counts stable ids and English legacy labels without mixing categories", () => {
    assert.equal(
      countWorkspaceAppsInCategory(apps, "product-design", "Product design"),
      2
    );
  });

  it("counts stable ids and Chinese legacy labels without language-dependent ids", () => {
    assert.equal(
      countWorkspaceAppsInCategory(apps, "product-design", "产品设计"),
      2
    );
  });

  it("filters by stable id while preserving legacy host compatibility", () => {
    assert.deepEqual(
      filterWorkspaceAppsByCategory(
        apps,
        "product-design",
        "Product design"
      ).map((app) => app.id),
      ["stable", "legacy-en"]
    );
    assert.deepEqual(
      filterWorkspaceAppsByCategory(apps, "tools", "Other tools").map(
        (app) => app.id
      ),
      ["stable-wins"]
    );
  });
});
