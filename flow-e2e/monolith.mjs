export function normalizeTask(rawTask) {
  const title = String(rawTask?.title ?? "").trim();
  const owner = String(rawTask?.owner ?? "unassigned").trim();
  const priority = Number(rawTask?.priority ?? 0);

  if (!title) {
    throw new Error("Task title is required");
  }

  return {
    title,
    owner,
    priority: Number.isFinite(priority) ? priority : 0,
    labels: Array.isArray(rawTask?.labels)
      ? rawTask.labels.map((label) => String(label).trim()).filter(Boolean)
      : [],
    completed: Boolean(rawTask?.completed),
  };
}

export function groupTasksByOwner(rawTasks) {
  return rawTasks.map(normalizeTask).reduce((groups, task) => {
    const ownerTasks = groups.get(task.owner) ?? [];
    ownerTasks.push(task);
    groups.set(task.owner, ownerTasks);
    return groups;
  }, new Map());
}

export function rankTasks(rawTasks) {
  return rawTasks
    .map(normalizeTask)
    .sort((left, right) => {
      if (left.completed !== right.completed) {
        return Number(left.completed) - Number(right.completed);
      }

      if (left.priority !== right.priority) {
        return right.priority - left.priority;
      }

      return left.title.localeCompare(right.title);
    });
}

export function summarizeTasks(rawTasks) {
  const tasks = rawTasks.map(normalizeTask);
  const owners = groupTasksByOwner(tasks);
  const openTasks = tasks.filter((task) => !task.completed);

  return {
    total: tasks.length,
    completed: tasks.length - openTasks.length,
    open: openTasks.length,
    ownerCount: owners.size,
    highestPriority: openTasks.reduce(
      (highest, task) => Math.max(highest, task.priority),
      0,
    ),
  };
}

export function renderTaskReport(rawTasks) {
  const rankedTasks = rankTasks(rawTasks);
  const summary = summarizeTasks(rankedTasks);
  const rows = rankedTasks.map((task) => {
    const status = task.completed ? "done" : "open";
    const labels = task.labels.length > 0 ? task.labels.join(", ") : "none";
    return `| ${task.title} | ${task.owner} | ${task.priority} | ${status} | ${labels} |`;
  });

  return [
    "# Task report",
    "",
    `Open: ${summary.open}; completed: ${summary.completed}; owners: ${summary.ownerCount}.`,
    "",
    "| Task | Owner | Priority | Status | Labels |",
    "| --- | --- | ---: | --- | --- |",
    ...rows,
    "",
  ].join("\n");
}

export function buildOwnerDigest(rawTasks) {
  const groupedTasks = groupTasksByOwner(rawTasks);

  return [...groupedTasks.entries()]
    .sort(([leftOwner], [rightOwner]) => leftOwner.localeCompare(rightOwner))
    .map(([owner, tasks]) => {
      const summary = summarizeTasks(tasks);
      return {
        owner,
        open: summary.open,
        completed: summary.completed,
        nextTask: rankTasks(tasks).find((task) => !task.completed)?.title ?? null,
      };
    });
}
