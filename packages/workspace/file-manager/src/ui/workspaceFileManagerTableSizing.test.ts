import assert from "node:assert/strict";
import test from "node:test";
import {
  resolveWorkspaceFileManagerPreservedNameColumnWidth,
  workspaceFileManagerTableNameMinWidthProperty
} from "./workspaceFileManagerTableSizing.ts";

test("workspace file manager preserves the current name width when sidebar resizing starts", () => {
  assert.equal(
    workspaceFileManagerTableNameMinWidthProperty,
    "--workspace-file-manager-table-name-min-width"
  );
  assert.equal(resolveWorkspaceFileManagerPreservedNameColumnWidth(386.4), 386);
  assert.equal(resolveWorkspaceFileManagerPreservedNameColumnWidth(120), 240);
  assert.equal(
    resolveWorkspaceFileManagerPreservedNameColumnWidth(Number.NaN),
    240
  );
});
