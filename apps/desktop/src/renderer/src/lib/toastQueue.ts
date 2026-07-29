export type DesktopToastTone = "default" | "destructive" | "success";

export const desktopToastMaximumVisibleDurationMs = 8000;

export interface DesktopToastItem {
  /** Hard deadline for settled toasts when Radix's interaction timer stays paused. */
  autoDismissAtUnixMs?: number;
  /** True while a loading toast's async work has not settled yet. */
  busy?: boolean;
  description?: string;
  id: string;
  title: string;
  tone: DesktopToastTone;
}

export function desktopToastMountKey(toast: DesktopToastItem): string {
  return `${toast.id}:${toast.busy ? "busy" : "settled"}`;
}

export function desktopToastAutoDismissDelayMs(
  toast: DesktopToastItem,
  nowUnixMs: number
): number | null {
  if (toast.busy || toast.autoDismissAtUnixMs === undefined) {
    return null;
  }
  return Math.max(0, toast.autoDismissAtUnixMs - nowUnixMs);
}

export function enqueueDesktopToast(
  current: DesktopToastItem[],
  next: DesktopToastItem,
  limit: number
): DesktopToastItem[] {
  const duplicate = current.some(
    (toast) =>
      toast.tone === next.tone &&
      toast.title === next.title &&
      toast.description === next.description
  );
  if (duplicate) {
    return current;
  }
  return [next, ...current].slice(0, limit);
}
