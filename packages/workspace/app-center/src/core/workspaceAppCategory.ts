import type { WorkspaceAppCategoryId } from "../contracts/catalog.ts";

export const workspaceAppCategoryIds = [
  "product-design",
  "content-creation",
  "office",
  "tools"
] as const satisfies readonly WorkspaceAppCategoryId[];

const workspaceAppCategoryById = new Map<string, WorkspaceAppCategoryId>([
  ["product-competition", "product-design"],
  ["daily-product-radar", "product-design"],
  ["daily-tech-radar", "product-design"],
  ["radar", "product-design"],
  ["design-review", "product-design"],
  ["vibe-design", "product-design"],
  ["ai-media-canvas", "content-creation"],
  ["media-canvas", "content-creation"],
  ["open-cut", "content-creation"],
  ["ai-slide", "office"],
  ["ai-doc", "office"],
  ["ai-sheet", "office"],
  ["automation", "tools"],
  ["tutti-onboarding", "tools"],
  ["group-chat", "tools"],
  ["issue", "tools"],
  ["issues", "tools"],
  ["issue-manager", "tools"],
  ["workspace-issue", "tools"],
  ["workspace-issue-manager", "tools"],
  ["draw-topic-app", "tools"],
  ["answer-book", "tools"],
  ["app_answer_book", "tools"],
  ["idea-draw", "tools"],
  ["omni-catcher", "tools"]
]);

const workspaceAppCategoryIdSet = new Set<string>(workspaceAppCategoryIds);

export function resolveWorkspaceAppCategoryId(
  appId: string
): WorkspaceAppCategoryId | null {
  return workspaceAppCategoryById.get(appId.trim().toLowerCase()) ?? null;
}

export function normalizeWorkspaceAppCategoryId(
  categoryId: string | null | undefined
): WorkspaceAppCategoryId | null {
  const normalized = categoryId?.trim().toLowerCase() ?? "";
  return workspaceAppCategoryIdSet.has(normalized)
    ? (normalized as WorkspaceAppCategoryId)
    : null;
}

interface WorkspaceAppCategoryProjection {
  readonly category?: string | null;
  readonly categoryId?: WorkspaceAppCategoryId | null;
}

export function matchesWorkspaceAppCategory(
  app: WorkspaceAppCategoryProjection,
  categoryId: WorkspaceAppCategoryId,
  legacyCategoryLabel?: string | null
): boolean {
  if (app.categoryId === categoryId) {
    return true;
  }
  if (app.categoryId) {
    return false;
  }
  const legacyCategory = app.category?.trim();
  return Boolean(
    legacyCategory &&
    legacyCategoryLabel &&
    legacyCategory === legacyCategoryLabel.trim()
  );
}

export function countWorkspaceAppsInCategory(
  apps: readonly WorkspaceAppCategoryProjection[],
  categoryId: WorkspaceAppCategoryId,
  legacyCategoryLabel?: string | null
): number {
  return apps.filter((app) =>
    matchesWorkspaceAppCategory(app, categoryId, legacyCategoryLabel)
  ).length;
}

export function filterWorkspaceAppsByCategory<
  App extends WorkspaceAppCategoryProjection
>(
  apps: readonly App[],
  categoryId: WorkspaceAppCategoryId,
  legacyCategoryLabel?: string | null
): App[] {
  return apps.filter((app) =>
    matchesWorkspaceAppCategory(app, categoryId, legacyCategoryLabel)
  );
}
