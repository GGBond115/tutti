import assert from "node:assert/strict";
import test from "node:test";
import { showDesktopStartupFailureDialog } from "./desktopStartupFailureDialog.ts";

test("startup failure dialog explains preserved data and opens logs", async () => {
  const opened: string[] = [];
  let detail = "";
  await showDesktopStartupFailureDialog({
    locale: "zh-CN",
    logsDirectory: "C:\\Users\\demo\\.tutti\\logs",
    platform: "win32",
    async openPath(path) {
      opened.push(path);
      return "";
    },
    async showMessageBox(options) {
      detail = options.detail;
      return { response: 0 };
    }
  });

  assert.match(detail, /数据未被删除/);
  assert.match(detail, /卸载时选择删除全部用户数据/);
  assert.deepEqual(opened, ["C:\\Users\\demo\\.tutti\\logs"]);
});

test("startup failure dialog does not suggest a Windows-only reset elsewhere", async () => {
  let detail = "";
  await showDesktopStartupFailureDialog({
    locale: "en-US",
    logsDirectory: "/tmp/tutti/logs",
    platform: "darwin",
    async openPath() {
      return "";
    },
    async showMessageBox(options) {
      detail = options.detail;
      return { response: 1 };
    }
  });

  assert.doesNotMatch(detail, /uninstall/i);
  assert.match(detail, /Open the logs folder/);
});
