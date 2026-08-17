import assert from "node:assert/strict";
import test from "node:test";

import type { ConnectorMarketI18nRuntime } from "../../i18n/connectorMarketI18n.ts";
import { resolveConnectorCategoryTitle } from "./connectorCategoryTitle.ts";

const i18n = {
  has: () => true,
  t: (key: string) => key,
  tFirst: (keys: readonly string[]) => keys[0] ?? ""
} as ConnectorMarketI18nRuntime;

test("uses the server-owned category name for the active locale", () => {
  assert.equal(
    resolveConnectorCategoryTitle({
      sectionId: "business-operations",
      displayNameZh: "商业与运营",
      displayNameEn: "Business & Operations",
      locale: "zh-CN",
      i18n
    }),
    "商业与运营"
  );
  assert.equal(
    resolveConnectorCategoryTitle({
      sectionId: "business-operations",
      displayNameZh: "商业与运营",
      displayNameEn: "Business & Operations",
      locale: "en-US",
      i18n
    }),
    "Business & Operations"
  );
});

test("falls back across server languages without changing category id", () => {
  assert.equal(
    resolveConnectorCategoryTitle({
      sectionId: "future-category",
      displayNameEn: "Future Category",
      locale: "zh-HK",
      i18n
    }),
    "Future Category"
  );
});

test("keeps only the released legacy category label fallback", () => {
  assert.equal(
    resolveConnectorCategoryTitle({
      sectionId: "development",
      locale: "en-US",
      i18n
    }),
    "categoryDevelopment"
  );
  assert.equal(
    resolveConnectorCategoryTitle({
      sectionId: "future-category",
      locale: "en-US",
      i18n
    }),
    "categoryUnnamed"
  );
});
