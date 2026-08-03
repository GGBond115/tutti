import {
  createServer,
  type IncomingMessage,
  type ServerResponse
} from "node:http";
import type {
  DesktopProduct,
  MinimumVersionCheckRequest
} from "../contracts/index.ts";
import { createDevelopmentMinimumVersionChecker } from "./policyChecker.ts";
import type { DesktopUpdateDevelopmentPolicyScenario } from "./policyScenario.ts";

const maximumRequestBodyBytes = 64 * 1_024;
const minimumVersionPath = "/api/desktop/v1/public/desktop-version/check";

export interface DesktopUpdateDevelopmentMockServer {
  readonly baseUrl: string;
  close(): Promise<void>;
}

class InvalidDevelopmentMockRequestError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "InvalidDevelopmentMockRequestError";
  }
}

function invalidRequest(message: string): never {
  throw new InvalidDevelopmentMockRequestError(message);
}

function writeJson(
  response: ServerResponse,
  statusCode: number,
  value: unknown
): void {
  const body = JSON.stringify(value);
  response.writeHead(statusCode, {
    "content-length": Buffer.byteLength(body),
    "content-type": "application/json; charset=utf-8"
  });
  response.end(body);
}

async function readJson(request: IncomingMessage): Promise<unknown> {
  let size = 0;
  const chunks: Buffer[] = [];
  for await (const value of request) {
    const chunk = Buffer.isBuffer(value) ? value : Buffer.from(value);
    size += chunk.byteLength;
    if (size > maximumRequestBodyBytes) {
      return invalidRequest("request body exceeds 64 KiB");
    }
    chunks.push(chunk);
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    return invalidRequest("request body must contain valid JSON");
  }
}

function normalizeRequest(
  value: unknown
): MinimumVersionCheckRequest<DesktopProduct> {
  if (!value || typeof value !== "object") {
    return invalidRequest("request must be an object");
  }
  const candidate = value as Record<string, unknown>;
  if (
    candidate.product !== "tsh-desktop" &&
    candidate.product !== "tutti-desktop"
  ) {
    return invalidRequest("request has an invalid product");
  }
  if (
    candidate.platform !== "macos" &&
    candidate.platform !== "windows" &&
    candidate.platform !== "linux"
  ) {
    return invalidRequest("request has an invalid platform");
  }
  if (candidate.architecture !== "arm64" && candidate.architecture !== "x64") {
    return invalidRequest("request has an invalid architecture");
  }
  if (
    typeof candidate.currentVersion !== "string" ||
    candidate.currentVersion.trim() === ""
  ) {
    return invalidRequest("request has an invalid currentVersion");
  }
  return {
    architecture: candidate.architecture,
    currentVersion: candidate.currentVersion,
    platform: candidate.platform,
    product: candidate.product
  };
}

export async function startDesktopUpdateDevelopmentMockServer(input: {
  port?: number;
  policy: DesktopUpdateDevelopmentPolicyScenario;
}): Promise<DesktopUpdateDevelopmentMockServer> {
  const checker = createDevelopmentMinimumVersionChecker(input.policy);
  const server = createServer(async (request, response) => {
    if (request.url === "/healthz") {
      if (request.method !== "GET") {
        response.writeHead(405, { allow: "GET" });
        response.end();
        return;
      }
      writeJson(response, 200, { status: "ok" });
      return;
    }
    if (request.url !== minimumVersionPath) {
      writeJson(response, 404, { error: "not found" });
      return;
    }
    if (request.method !== "POST") {
      response.writeHead(405, { allow: "POST" });
      response.end();
      return;
    }
    if (
      !String(request.headers["content-type"] ?? "")
        .toLowerCase()
        .startsWith("application/json")
    ) {
      writeJson(response, 415, {
        error: "content-type must be application/json"
      });
      return;
    }
    const abortController = new AbortController();
    request.once("aborted", () => abortController.abort());
    response.once("close", () => {
      if (!response.writableEnded) {
        abortController.abort();
      }
    });
    try {
      const policyRequest = normalizeRequest(await readJson(request));
      const result = await checker(policyRequest, abortController.signal);
      if (!response.destroyed) {
        writeJson(response, 200, result);
      }
    } catch (error) {
      if (abortController.signal.aborted || response.destroyed) {
        return;
      }
      const message = error instanceof Error ? error.message : String(error);
      const statusCode =
        error instanceof InvalidDevelopmentMockRequestError ? 400 : 500;
      writeJson(response, statusCode, { error: message });
    }
  });
  const port = input.port ?? 0;
  if (!Number.isSafeInteger(port) || port < 0 || port > 65_535) {
    throw new Error("development mock server port must be between 0 and 65535");
  }
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => {
      server.removeListener("error", reject);
      resolve();
    });
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    throw new Error("development mock server did not expose a TCP address");
  }
  return {
    baseUrl: `http://127.0.0.1:${address.port}`,
    close: () =>
      new Promise<void>((resolve, reject) => {
        server.close((error) => {
          if (error) {
            reject(error);
          } else {
            resolve();
          }
        });
      })
  };
}
