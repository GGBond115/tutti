export function normalizeTask(rawTask) {
  const title = String(rawTask?.title ?? "").trim();
  const owner = String(rawTask?.owner ?? "unassigned").trim();
  const priority = Number(rawTask?.priority ?? 0);
  if (!title) throw new Error("Task title is required");
  return {
    title,
    owner,
    priority: Number.isFinite(priority) ? priority : 0,
    labels: Array.isArray(rawTask?.labels)
      ? rawTask.labels.map((label) => String(label).trim()).filter(Boolean)
      : [],
    completed: Boolean(rawTask?.completed)
  };
}

function groupNormalizedTasks(tasks) {
  return tasks.reduce((groups, task) => {
    const ownerTasks = groups.get(task.owner) ?? [];
    ownerTasks.push(task);
    groups.set(task.owner, ownerTasks);
    return groups;
  }, new Map());
}
function rankNormalizedTasks(tasks) {
  return [...tasks].sort(
    (left, right) =>
      Number(left.completed) - Number(right.completed) ||
      right.priority - left.priority ||
      left.title.localeCompare(right.title)
  );
}

function summarizeNormalizedTasks(tasks) {
  const openTasks = tasks.filter((task) => !task.completed);
  let highest = 0;
  for (const task of openTasks) highest = Math.max(highest, task.priority);
  return {
    total: tasks.length,
    completed: tasks.length - openTasks.length,
    open: openTasks.length,
    ownerCount: groupNormalizedTasks(tasks).size,
    highestPriority: highest
  };
}

export function groupTasksByOwner(rawTasks) {
  return groupNormalizedTasks(rawTasks.map(normalizeTask));
}
export function rankTasks(rawTasks) {
  return rankNormalizedTasks(rawTasks.map(normalizeTask));
}
export function summarizeTasks(rawTasks) {
  return summarizeNormalizedTasks(rawTasks.map(normalizeTask));
}

export function renderTaskReport(rawTasks) {
  const rankedTasks = rankNormalizedTasks(rawTasks.map(normalizeTask));
  const summary = summarizeNormalizedTasks(rankedTasks);
  const rows = rankedTasks.map((task) => {
    const status = task.completed ? "done" : "open";
    const labels = task.labels.length > 0 ? task.labels.join(", ") : "none";
    return `| ${task.title} | ${task.owner} | ${task.priority} | ${status} | ${labels} |`;
  });
  return `# Task report

Open: ${summary.open}; completed: ${summary.completed}; owners: ${summary.ownerCount}.

| Task | Owner | Priority | Status | Labels |
| --- | --- | ---: | --- | --- |
${rows.join("\n")}${rows.length > 0 ? "\n" : ""}`;
}

export function buildOwnerDigest(rawTasks) {
  const tasks = rawTasks.map(normalizeTask);
  return [...groupNormalizedTasks(tasks).entries()]
    .sort(([leftOwner], [rightOwner]) => leftOwner.localeCompare(rightOwner))
    .map(([owner, tasks]) => {
      const { open, completed } = summarizeNormalizedTasks(tasks);
      const rankedTasks = rankNormalizedTasks(tasks);
      const nextTask = rankedTasks.find((task) => !task.completed);
      return {
        owner,
        open,
        completed,
        nextTask: nextTask?.title ?? null
      };
    });
}
