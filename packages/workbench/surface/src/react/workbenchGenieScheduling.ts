export interface CachedWorkbenchGenieRestoreInput {
  launch(): void;
  requestFrame(callback: () => void): void;
  scheduleTask(callback: () => void): void;
  startAnimation(): void;
}

export function startCachedWorkbenchGenieRestore({
  launch,
  requestFrame,
  scheduleTask,
  startAnimation
}: CachedWorkbenchGenieRestoreInput): void {
  startAnimation();
  requestFrame(() => {
    scheduleTask(launch);
  });
}

export interface NativeFirstGenieTextureResult<TTexture> {
  nativeImageUrl: string | null;
  nativeStatus: "pending" | "resolved";
  texture: TTexture | null;
}

export async function resolveNativeFirstGenieTexture<TTexture>({
  nativeImageUrlPromise,
  renderDomFallback,
  renderNativeImage,
  timeoutMs
}: {
  nativeImageUrlPromise: Promise<string | null>;
  renderDomFallback(): Promise<TTexture | null> | TTexture | null;
  renderNativeImage(
    nativeImageUrl: string
  ): Promise<TTexture | null> | TTexture | null;
  timeoutMs: number;
}): Promise<NativeFirstGenieTextureResult<TTexture>> {
  const nativeCapture = await new Promise<{
    nativeImageUrl: string | null;
    nativeStatus: "pending" | "resolved";
  }>((resolve) => {
    let settled = false;
    const settleResolved = (nativeImageUrl: string | null) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      resolve({ nativeImageUrl, nativeStatus: "resolved" });
    };
    const timer = setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      resolve({ nativeImageUrl: null, nativeStatus: "pending" });
    }, timeoutMs);
    nativeImageUrlPromise.then(settleResolved, () => settleResolved(null));
  });

  const nativeTexture = nativeCapture.nativeImageUrl
    ? await Promise.resolve(
        renderNativeImage(nativeCapture.nativeImageUrl)
      ).catch(() => null)
    : null;
  const texture =
    nativeTexture ??
    (await Promise.resolve(renderDomFallback()).catch(() => null));

  return {
    ...nativeCapture,
    texture
  };
}
