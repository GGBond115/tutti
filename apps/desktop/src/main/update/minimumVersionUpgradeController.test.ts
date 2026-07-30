import assert from "node:assert/strict";
import test from "node:test";
import {
  releaseMeetsMinimum,
  resolveMinimumVersionRuntimeTarget,
  shouldCheckMinimumVersionAfterForeground
} from "./minimumVersionPolicy.ts";

test("forced update target must meet the exact stable or RC minimum", () => {
  assert.equal(releaseMeetsMinimum("1.6.0", "1.6.0"), true);
  assert.equal(releaseMeetsMinimum("1.6.1", "1.6.0"), true);
  assert.equal(releaseMeetsMinimum("1.7.0-rc.3", "1.7.0-rc.2"), true);
  assert.equal(releaseMeetsMinimum("1.7.0-rc.1", "1.7.0-rc.2"), false);
  assert.equal(releaseMeetsMinimum("1.6.0-beta.1", "1.6.0"), false);
});

test("unsupported runtime platforms and architectures skip policy requests", () => {
  assert.deepEqual(resolveMinimumVersionRuntimeTarget("darwin", "arm64"), {
    platform: "macos",
    architecture: "arm64"
  });
  assert.equal(resolveMinimumVersionRuntimeTarget("aix", "x64"), null);
  assert.equal(resolveMinimumVersionRuntimeTarget("linux", "ia32"), null);
});

test("foreground checks are limited to 30 minutes and stop after the first prompt", () => {
  assert.equal(
    shouldCheckMinimumVersionAfterForeground({
      disposed: false,
      packaged: true,
      foregroundPrompted: false,
      lastCheckAt: 1_000,
      now: 1_000 + 30 * 60 * 1_000 - 1
    }),
    false
  );
  assert.equal(
    shouldCheckMinimumVersionAfterForeground({
      disposed: false,
      packaged: true,
      foregroundPrompted: false,
      lastCheckAt: 1_000,
      now: 1_000 + 30 * 60 * 1_000
    }),
    true
  );
  assert.equal(
    shouldCheckMinimumVersionAfterForeground({
      disposed: false,
      packaged: true,
      foregroundPrompted: true,
      lastCheckAt: 0,
      now: Number.MAX_SAFE_INTEGER
    }),
    false
  );
});
