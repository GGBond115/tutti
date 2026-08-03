import assert from "node:assert/strict";
import test from "node:test";
import {
  handoffHiddenAppCenterCategoryTab,
  resolveActiveAppCenterCategoryTab,
  resolveActiveAppCenterTab,
  resolveVisibleAppCenterCategoryTabs,
  resolveVisibleAppCenterTabs
} from "./appCenterTabs.ts";

test("all app tabs are visible by default", () => {
  assert.deepEqual(resolveVisibleAppCenterTabs(undefined), [
    "recommended",
    "community",
    "my"
  ]);
});

test("hosts can select and order the visible app tabs", () => {
  assert.deepEqual(resolveVisibleAppCenterTabs(["my", "recommended", "my"]), [
    "my",
    "recommended"
  ]);
});

test("an empty visible tab configuration keeps the panel usable", () => {
  assert.deepEqual(resolveVisibleAppCenterTabs([]), ["recommended"]);
});

test("a hidden active tab falls back to the first visible tab", () => {
  assert.equal(
    resolveActiveAppCenterTab("community", ["recommended"]),
    "recommended"
  );
});

test("category tabs keep all and hide empty categories", () => {
  assert.deepEqual(
    resolveVisibleAppCenterCategoryTabs(
      [
        { count: 4, id: "all", label: "All" },
        { count: 1, id: "design", label: "Design" },
        { count: 0, id: "office", label: "Office" }
      ],
      "all"
    ),
    [
      { count: 4, id: "all", label: "All" },
      { count: 1, id: "design", label: "Design" }
    ]
  );
});

test("all remains visible when every category is empty", () => {
  assert.deepEqual(
    resolveVisibleAppCenterCategoryTabs(
      [
        { count: 0, id: "all" },
        { count: 0, id: "design" }
      ],
      "all"
    ),
    [{ count: 0, id: "all" }]
  );
});

test("a category that becomes hidden falls back to all", () => {
  assert.equal(
    resolveActiveAppCenterCategoryTab(
      "office",
      [{ id: "all" }, { id: "design" }],
      "all"
    ),
    "all"
  );
});

test("a hidden focused category hands focus to all before updating state", () => {
  const events: string[] = [];
  handoffHiddenAppCenterCategoryTab(
    "office",
    "all",
    { focus: () => events.push("focus:all") },
    (value) => events.push(`change:${value}`)
  );
  assert.deepEqual(events, ["focus:all", "change:all"]);
});

test("a hidden unfocused category updates state without stealing focus", () => {
  const events: string[] = [];
  handoffHiddenAppCenterCategoryTab("office", "all", null, (value) =>
    events.push(`change:${value}`)
  );
  assert.deepEqual(events, ["change:all"]);
});

test("a category that remains visible does not move focus", () => {
  const events: string[] = [];
  handoffHiddenAppCenterCategoryTab(
    "office",
    "office",
    { focus: () => events.push("focus:all") },
    (value) => events.push(`change:${value}`)
  );
  assert.deepEqual(events, []);
});
