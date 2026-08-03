export type AppCenterAppTab = "recommended" | "community" | "my";

const defaultVisibleAppTabs: readonly AppCenterAppTab[] = [
  "recommended",
  "community",
  "my"
];

export function resolveVisibleAppCenterTabs(
  configuredTabs: readonly AppCenterAppTab[] | undefined
): readonly AppCenterAppTab[] {
  if (configuredTabs === undefined) {
    return defaultVisibleAppTabs;
  }

  const visibleTabs = configuredTabs.filter(
    (tab, index) =>
      defaultVisibleAppTabs.includes(tab) &&
      configuredTabs.indexOf(tab) === index
  );
  return visibleTabs.length > 0 ? visibleTabs : ["recommended"];
}

export function resolveActiveAppCenterTab(
  requestedTab: AppCenterAppTab,
  visibleTabs: readonly AppCenterAppTab[]
): AppCenterAppTab {
  return visibleTabs.includes(requestedTab)
    ? requestedTab
    : (visibleTabs[0] ?? "recommended");
}

interface CountedAppCenterCategoryTab<CategoryId extends string> {
  readonly count: number;
  readonly id: CategoryId;
}

export function resolveVisibleAppCenterCategoryTabs<
  Tab extends CountedAppCenterCategoryTab<string>
>(tabs: readonly Tab[], allTabId: Tab["id"]): Tab[] {
  return tabs.filter((tab) => tab.id === allTabId || tab.count > 0);
}

export function resolveActiveAppCenterCategoryTab<CategoryId extends string>(
  requestedTab: CategoryId,
  visibleTabs: readonly { readonly id: CategoryId }[],
  fallbackTab: CategoryId
): CategoryId {
  return visibleTabs.some((tab) => tab.id === requestedTab)
    ? requestedTab
    : fallbackTab;
}

export function handoffHiddenAppCenterCategoryTab<CategoryId extends string>(
  requestedTab: CategoryId,
  resolvedTab: CategoryId,
  fallbackTab: { readonly focus: () => void } | null,
  onValueChange: (value: CategoryId) => void
): void {
  if (requestedTab === resolvedTab) {
    return;
  }
  fallbackTab?.focus();
  onValueChange(resolvedTab);
}
