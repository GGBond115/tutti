import assert from "node:assert/strict";
import test from "node:test";

import {
  buildOwnerDigest,
  normalizeTask,
  rankTasks,
  renderTaskReport,
  summarizeTasks,
} from "./monolith.mjs";

const tasks = [
  {
    title: "Ship release",
    owner: "Ada",
    priority: 10,
    labels: ["release"],
  },
  {
    title: "Write notes",
    owner: "Ben",
    priority: 4,
    completed: true,
  },
  {
    title: "Fix tests",
    owner: "Ada",
    priority: 8,
    labels: ["ci", "urgent"],
  },
];

test("normalizes a task and rejects an empty title", () => {
  assert.deepEqual(normalizeTask({ title: "  Demo  " }), {
    title: "Demo",
    owner: "unassigned",
    priority: 0,
    labels: [],
    completed: false,
  });
  assert.throws(() => normalizeTask({}), /title is required/i);
});

test("ranks open tasks by priority before completed tasks", () => {
  assert.deepEqual(
    rankTasks(tasks).map((task) => task.title),
    ["Ship release", "Fix tests", "Write notes"],
  );
});

test("summarizes tasks", () => {
  assert.deepEqual(summarizeTasks(tasks), {
    total: 3,
    completed: 1,
    open: 2,
    ownerCount: 2,
    highestPriority: 10,
  });
});

test("renders a markdown report", () => {
  const report = renderTaskReport(tasks);
  assert.match(report, /# Task report/);
  assert.match(report, /\| Ship release \| Ada \| 10 \| open \| release \|/);
});

test("builds a digest for each owner", () => {
  assert.deepEqual(buildOwnerDigest(tasks), [
    { owner: "Ada", open: 2, completed: 0, nextTask: "Ship release" },
    { owner: "Ben", open: 0, completed: 1, nextTask: null },
  ]);
});
