import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const source = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), "WorkspaceAgentEditor.tsx"),
  "utf8"
);

test("custom Agent name keeps IME composition local until commit", () => {
  assert.match(source, /const nameInput = useComposedInputValue\(\{/);
  assert.match(source, /onCommit: \(name\) => onUpdate\(\{ name \}\)/);
  assert.match(source, /value: draft\.name/);

  const nameInputStart = source.indexOf(
    "<Input",
    source.indexOf("workspace.settings.apps.agents.nameLabel")
  );
  const nameInputEnd = source.indexOf("/>", nameInputStart);
  const nameInputSource = source.slice(nameInputStart, nameInputEnd);

  assert.match(nameInputSource, /value=\{nameInput\.value\}/);
  assert.match(nameInputSource, /onBlur=\{nameInput\.onBlur\}/);
  assert.match(nameInputSource, /onChange=\{nameInput\.onChange\}/);
  assert.match(
    nameInputSource,
    /onCompositionEnd=\{nameInput\.onCompositionEnd\}/
  );
  assert.match(
    nameInputSource,
    /onCompositionStart=\{nameInput\.onCompositionStart\}/
  );
  assert.doesNotMatch(nameInputSource, /value=\{draft\.name\}/);
});
