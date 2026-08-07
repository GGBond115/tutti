import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import test from "node:test";
import {
  createCaptureComposerPlacementStore,
  parseCaptureComposerPosition
} from "./captureComposerPlacement.ts";

test("capture composer placement validates persisted coordinates", () => {
  assert.deepEqual(parseCaptureComposerPosition({ x: 12.4, y: -8.6 }), {
    x: 12,
    y: -9
  });
  assert.equal(parseCaptureComposerPosition({ x: Number.NaN, y: 1 }), null);
  assert.equal(parseCaptureComposerPosition({ x: "12", y: 1 }), null);
});

test("capture composer placement store round-trips the last position", async () => {
  const directory = await mkdtemp(join(tmpdir(), "tutti-capture-placement-"));
  try {
    const filePath = join(directory, "placement.json");
    const store = createCaptureComposerPlacementStore(filePath);

    assert.equal(await store.read(), null);
    await store.write({ x: 321, y: 123 });

    assert.deepEqual(await store.read(), { x: 321, y: 123 });
    assert.deepEqual(JSON.parse(await readFile(filePath, "utf8")), {
      x: 321,
      y: 123
    });
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
});
