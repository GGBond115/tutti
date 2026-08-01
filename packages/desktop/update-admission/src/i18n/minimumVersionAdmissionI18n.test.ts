import assert from "node:assert/strict";
import test from "node:test";
import { createMinimumVersionAdmissionI18nRuntime } from "./minimumVersionAdmissionI18n.ts";

test("uses the required-update copy in Simplified Chinese", () => {
  const i18n = createMinimumVersionAdmissionI18nRuntime("zh-CN");
  const params = { productName: "Tutti" };

  assert.equal(i18n.t("startupTitle", params), "需要更新 Tutti 后才能继续使用");
  assert.equal(
    i18n.t("startupDetail", params),
    "你的 Tutti 版本过低，请更新到最新版本。你的自动更新设置不会被修改。"
  );
  assert.equal(i18n.t("currentVersion"), "当前版本");
  assert.equal(i18n.t("minimumVersion"), "需要版本");
  assert.equal(i18n.t("upgrade"), "立即更新");
});

test("uses the equivalent required-update copy in English", () => {
  const i18n = createMinimumVersionAdmissionI18nRuntime("en");
  const params = { productName: "Tutti" };

  assert.equal(i18n.t("startupTitle", params), "Update Tutti to continue");
  assert.equal(
    i18n.t("startupDetail", params),
    "Your Tutti version is outdated. Update to the latest version. Your automatic update settings will not be changed."
  );
  assert.equal(i18n.t("currentVersion"), "Current version");
  assert.equal(i18n.t("minimumVersion"), "Required version");
  assert.equal(i18n.t("upgrade"), "Update now");
});
