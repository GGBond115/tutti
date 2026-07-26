import assert from "node:assert/strict";
import test from "node:test";

import {
  buildOwnerDigest,
  groupTasksByOwner,
  normalizeTask,
  rankTasks,
  renderTaskReport,
  summarizeTasks
} from "./monolith.mjs";

const tasks = [
  {
    title: "Ship release",
    owner: "Ada",
    priority: 10,
    labels: ["release"]
  },
  {
    title: "Write notes",
    owner: "Ben",
    priority: 4,
    completed: true
  },
  {
    title: "Fix tests",
    owner: "Ada",
    priority: 8,
    labels: ["ci", "urgent"]
  }
];

test("normalizes task fields, defaults, and coercions", () => {
  assert.deepEqual(
    normalizeTask({
      title: "  Demo  ",
      owner: "  Ada  ",
      priority: "7",
      labels: [" release ", "", "  ", 42],
      completed: 1
    }),
    {
      title: "Demo",
      owner: "Ada",
      priority: 7,
      labels: ["release", "42"],
      completed: true
    }
  );
  assert.deepEqual(normalizeTask({ title: "Defaults", priority: "invalid" }), {
    title: "Defaults",
    owner: "unassigned",
    priority: 0,
    labels: [],
    completed: false
  });
  assert.throws(() => normalizeTask({ title: "  " }), /title is required/i);
});

test("groups normalized tasks by owner and preserves task order", () => {
  const groups = groupTasksByOwner(tasks);
  assert.ok(groups instanceof Map);
  assert.deepEqual([...groups.keys()], ["Ada", "Ben"]);
  assert.deepEqual(
    groups.get("Ada").map((task) => task.title),
    ["Ship release", "Fix tests"]
  );
});

test("public adapters normalize raw tasks", () => {
  const rawTasks = [{ title: "  Demo  ", owner: " Ada ", priority: "3" }];
  assert.deepEqual(groupTasksByOwner(rawTasks).get("Ada"), [
    {
      title: "Demo",
      owner: "Ada",
      priority: 3,
      labels: [],
      completed: false
    }
  ]);
  assert.deepEqual(rankTasks(rawTasks), [
    {
      title: "Demo",
      owner: "Ada",
      priority: 3,
      labels: [],
      completed: false
    }
  ]);
  assert.deepEqual(summarizeTasks(rawTasks), {
    total: 1,
    completed: 0,
    open: 1,
    ownerCount: 1,
    highestPriority: 3
  });
});

test("ranks by completion, descending priority, and title", () => {
  const rankedTasks = rankTasks([
    { title: "Zulu", priority: 5 },
    { title: "Alpha", priority: 5 },
    { title: "Done high", priority: 100, completed: true },
    { title: "Open high", priority: 10 },
    { title: "Done low", priority: 1, completed: true }
  ]);
  assert.deepEqual(
    rankedTasks.map((task) => task.title),
    ["Open high", "Alpha", "Zulu", "Done high", "Done low"]
  );
});

test("summarizes open tasks", () => {
  assert.deepEqual(summarizeTasks(tasks), {
    total: 3,
    completed: 1,
    open: 2,
    ownerCount: 2,
    highestPriority: 10
  });
});

test("summarizes empty and all-completed task lists", () => {
  assert.deepEqual(summarizeTasks([]), {
    total: 0,
    completed: 0,
    open: 0,
    ownerCount: 0,
    highestPriority: 0
  });
  assert.deepEqual(
    summarizeTasks([
      { title: "One", owner: "Ada", priority: 9, completed: true },
      { title: "Two", owner: "Ben", priority: 4, completed: true }
    ]),
    {
      total: 2,
      completed: 2,
      open: 0,
      ownerCount: 2,
      highestPriority: 0
    }
  );
});

test("renders the exact empty markdown report", () => {
  assert.equal(
    renderTaskReport([]),
    [
      "# Task report",
      "",
      "Open: 0; completed: 0; owners: 0.",
      "",
      "| Task | Owner | Priority | Status | Labels |",
      "| --- | --- | ---: | --- | --- |",
      ""
    ].join("\n")
  );
});

test("renders the exact populated markdown report", () => {
  assert.equal(
    renderTaskReport(tasks),
    [
      "# Task report",
      "",
      "Open: 2; completed: 1; owners: 2.",
      "",
      "| Task | Owner | Priority | Status | Labels |",
      "| --- | --- | ---: | --- | --- |",
      "| Ship release | Ada | 10 | open | release |",
      "| Fix tests | Ada | 8 | open | ci, urgent |",
      "| Write notes | Ben | 4 | done | none |",
      ""
    ].join("\n")
  );
});

test("builds owner-sorted digests with the highest-ranked open task", () => {
  assert.deepEqual(buildOwnerDigest(tasks), [
    { owner: "Ada", open: 2, completed: 0, nextTask: "Ship release" },
    { owner: "Ben", open: 0, completed: 1, nextTask: null }
  ]);
  assert.deepEqual(
    buildOwnerDigest([
      { title: "Lower", owner: "Zoe", priority: 1 },
      { title: "Done", owner: "Ada", priority: 100, completed: true },
      { title: "Higher", owner: "Zoe", priority: 5 }
    ]),
    [
      { owner: "Ada", open: 0, completed: 1, nextTask: null },
      { owner: "Zoe", open: 2, completed: 0, nextTask: "Higher" }
    ]
  );
});
