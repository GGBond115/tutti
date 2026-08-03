import type { DesktopRuntimeApi } from "@preload/types";

type BackendConfigRuntimeApi = Pick<DesktopRuntimeApi, "getBackendConfig"> &
  Partial<Pick<DesktopRuntimeApi, "logRendererDiagnostic">>;

const diagnosticDeduplicationWindowMs = 5_000;

export function createRestartAwareFetch(
  runtimeApi: BackendConfigRuntimeApi,
  nativeFetch: typeof fetch = globalThis.fetch.bind(globalThis)
): typeof fetch {
  let mostRecentFailure: { key: string; loggedAtUnixMs: number } | null = null;

  return async (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init);
    const config = await runtimeApi.getBackendConfig();
    const requestUrl = new URL(request.url);
    const backendUrl = new URL(config.baseUrl);
    const rewrittenUrl = new URL(
      `${requestUrl.pathname}${requestUrl.search}${requestUrl.hash}`,
      backendUrl.origin
    );
    const headers = new Headers(request.headers);
    const body =
      request.body === null ? undefined : await request.clone().arrayBuffer();

    headers.set("Authorization", `Bearer ${config.accessToken}`);

    // Passing a Request as RequestInit preserves its body as a ReadableStream.
    // Chromium only sends streaming uploads over HTTP/2 or QUIC, while the
    // managed loopback daemon intentionally serves HTTP/1.1. Materialize the
    // already-serialized client body before rebuilding the request so POST and
    // PUT calls remain ordinary HTTP/1.1 uploads after the origin changes.
    const rewrittenRequest = new Request(rewrittenUrl, {
      body,
      cache: request.cache,
      credentials: request.credentials,
      headers,
      integrity: request.integrity,
      keepalive: request.keepalive,
      method: request.method,
      mode: request.mode,
      redirect: request.redirect,
      referrer: request.referrer,
      referrerPolicy: request.referrerPolicy,
      signal: request.signal
    });

    try {
      const response = await nativeFetch(rewrittenRequest);
      if (!response.ok) {
        reportTuttidRequestFailure(runtimeApi, mostRecentFailure, {
          backendOrigin: backendUrl.origin,
          httpStatus: response.status,
          method: rewrittenRequest.method,
          requestBodyBytes: body?.byteLength ?? 0,
          requestPath: rewrittenUrl.pathname,
          stage: "response"
        });
        mostRecentFailure = latestTuttidRequestFailure({
          httpStatus: response.status,
          method: rewrittenRequest.method,
          requestPath: rewrittenUrl.pathname,
          stage: "response"
        });
      }
      return response;
    } catch (error) {
      reportTuttidRequestFailure(runtimeApi, mostRecentFailure, {
        backendOrigin: backendUrl.origin,
        ...tuttidFetchErrorDetails(error),
        method: rewrittenRequest.method,
        requestBodyBytes: body?.byteLength ?? 0,
        requestPath: rewrittenUrl.pathname,
        stage: "fetch"
      });
      mostRecentFailure = latestTuttidRequestFailure({
        errorName: error instanceof Error ? error.name : typeof error,
        method: rewrittenRequest.method,
        requestPath: rewrittenUrl.pathname,
        stage: "fetch"
      });
      throw error;
    }
  };
}

type TuttidRequestFailure = {
  backendOrigin: string;
  errorCauseMessage?: string;
  errorCauseName?: string;
  errorMessage?: string;
  errorName?: string;
  httpStatus?: number;
  method: string;
  requestBodyBytes: number;
  requestPath: string;
  stage: "fetch" | "response";
};

function reportTuttidRequestFailure(
  runtimeApi: BackendConfigRuntimeApi,
  mostRecentFailure: { key: string; loggedAtUnixMs: number } | null,
  failure: TuttidRequestFailure
): void {
  if (!runtimeApi.logRendererDiagnostic) {
    return;
  }
  const now = Date.now();
  const key = tuttidRequestFailureKey(failure);
  if (
    mostRecentFailure?.key === key &&
    now - mostRecentFailure.loggedAtUnixMs < diagnosticDeduplicationWindowMs
  ) {
    return;
  }
  try {
    void runtimeApi
      .logRendererDiagnostic({
        details: failure,
        event: "tuttid.http.request_failed",
        level: "warn",
        source: "tuttid-fetch"
      })
      .catch(() => {});
  } catch {
    // Diagnostic logging must not affect daemon requests.
  }
}

function latestTuttidRequestFailure(
  failure: Pick<
    TuttidRequestFailure,
    "errorName" | "httpStatus" | "method" | "requestPath" | "stage"
  >
): { key: string; loggedAtUnixMs: number } | null {
  return {
    key: tuttidRequestFailureKey(failure),
    loggedAtUnixMs: Date.now()
  };
}

function tuttidRequestFailureKey(
  failure: Pick<
    TuttidRequestFailure,
    "errorName" | "httpStatus" | "method" | "requestPath" | "stage"
  >
): string {
  return [
    failure.stage,
    failure.method,
    failure.requestPath,
    failure.httpStatus ?? "",
    failure.errorName ?? ""
  ].join("|");
}

function tuttidFetchErrorDetails(
  error: unknown
): Pick<
  TuttidRequestFailure,
  "errorCauseMessage" | "errorCauseName" | "errorMessage" | "errorName"
> {
  if (!(error instanceof Error)) {
    return { errorName: typeof error };
  }
  const cause = (error as Error & { cause?: unknown }).cause;
  return {
    ...(cause instanceof Error
      ? {
          errorCauseMessage: truncateDiagnosticText(cause.message),
          errorCauseName: cause.name
        }
      : {}),
    errorMessage: truncateDiagnosticText(error.message),
    errorName: error.name
  };
}

function truncateDiagnosticText(value: string): string {
  return value
    .replace(/\bBearer\s+\S+/gi, "Bearer [redacted]")
    .replace(/\bhttps?:\/\/\S+/gi, "[url]")
    .slice(0, 240);
}
