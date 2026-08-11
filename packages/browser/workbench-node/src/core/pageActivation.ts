import type { BrowserNodeFeature } from "./feature.ts";
import type { BrowserNodeTab } from "./tabsStore.ts";
import { normalizeBrowserComparableUrl } from "./url.ts";

export function findBrowserNodePageByUrl(
  feature: BrowserNodeFeature,
  surfaceNodeId: string,
  url: string
): BrowserNodeTab | null {
  const comparableUrl = normalizeBrowserComparableUrl(url);
  if (!comparableUrl) {
    return null;
  }

  const state = feature.tabsStore.getSurfaceState(surfaceNodeId);
  return (
    state?.tabs.find((tab) => {
      const runtimeUrl = feature.runtimeStore.getNodeState(tab.nodeId).url;
      const pageUrl = runtimeUrl?.trim() || tab.defaultUrl;
      return normalizeBrowserComparableUrl(pageUrl) === comparableUrl;
    }) ?? null
  );
}

export function activateBrowserNodePageByUrl(
  feature: BrowserNodeFeature,
  surfaceNodeId: string,
  url: string
): BrowserNodeTab | null {
  const page = findBrowserNodePageByUrl(feature, surfaceNodeId, url);
  if (!page) {
    return null;
  }
  feature.tabsStore.selectTab(surfaceNodeId, page.id);
  return page;
}
