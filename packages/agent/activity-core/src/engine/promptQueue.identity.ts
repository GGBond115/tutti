/** Allocates a queue-local identity distinct from clientSubmitId. */
export function createPromptQueueId(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return `prompt:${globalThis.crypto.randomUUID()}`;
  }
  const fallbackHex = Math.random().toString(16).slice(2).padEnd(12, "0");
  return `prompt:00000000-0000-4000-8000-${fallbackHex.slice(0, 12)}`;
}
