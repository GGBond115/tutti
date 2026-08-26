import { readFile, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import type {
  AgentProbeProvider,
  AgentProviderProbeListInput,
  AgentUsageQuota
} from "@tutti-os/agent-gui";

import { outboundFetch } from "./net/outboundFetch.ts";

const WORKBUDDY_BILLING_URL =
  "https://copilot.tencent.com/billing/meter/get-user-resource";
const WORKBUDDY_AUTH_RELATIVE_PATH = join(
  "Library",
  "Application Support",
  "CodeBuddyExtension",
  "Data",
  "Public",
  "auth",
  "workbuddy-desktop.info"
);

interface WorkBuddyAuthSession {
  accessToken: string;
  expiresAt: number;
  nickname: string;
}

interface WorkBuddyBillingAccount {
  PackageName?: unknown;
  PackageCode?: unknown;
  CapacityUnit?: unknown;
  CapacityRemain?: unknown;
  CapacitySize?: unknown;
  CapacityUsed?: unknown;
  CycleEndTime?: unknown;
}

interface WorkBuddyBillingResponse {
  code?: unknown;
  data?: {
    Response?: {
      Data?: {
        Accounts?: WorkBuddyBillingAccount[] | null;
      } | null;
    } | null;
  } | null;
}

export async function probeCodeBuddyProvider(
  input: AgentProviderProbeListInput,
  capturedAtUnixMs: number
): Promise<AgentProbeProvider> {
  const attempts: AgentProbeProvider["attempts"] = [];
  let session: WorkBuddyAuthSession;
  try {
    session = await loadWorkBuddyAuthSession();
  } catch (error) {
    return {
      attempts: [
        {
          errorCode: "auth_required",
          errorMessage: errorMessage(error),
          strategy: "workbuddy-auth-file",
          success: false
        }
      ],
      availability: {
        checks: [{ name: "auth", passed: false, detail: errorMessage(error) }],
        detailsVisible: true,
        status: "unavailable"
      },
      lastError: { code: "auth_required", message: errorMessage(error) },
      provider: "codebuddy"
    };
  }

  if (!input.includeUsage) {
    return {
      availability: {
        checks: [{ name: "auth", passed: true }],
        detailsVisible: false,
        status: "available"
      },
      provider: "codebuddy"
    };
  }

  try {
    const response = await fetchWorkBuddyBilling(session.accessToken);
    attempts.push({ strategy: "workbuddy-billing-api", success: true });
    const usage = workBuddyUsageFromBilling(response, capturedAtUnixMs);
    if (!usage) {
      attempts.push({
        errorCode: "no_data",
        strategy: "workbuddy-billing-api",
        success: false
      });
      return {
        attempts,
        availability: {
          checks: [{ name: "auth", passed: true }],
          detailsVisible: false,
          status: "available"
        },
        lastError: { code: "no_data" },
        provider: "codebuddy",
        usage: {
          billingMode: "provider_account",
          quotaState: "unavailable",
          capturedAtUnixMs
        }
      };
    }
    return {
      attempts,
      availability: {
        checks: [{ name: "auth", passed: true }],
        detailsVisible: false,
        status: "available"
      },
      provider: "codebuddy",
      usage
    };
  } catch (error) {
    const code = workBuddyProbeErrorCode(error);
    attempts.push({
      errorCode: code,
      errorMessage: errorMessage(error),
      strategy: "workbuddy-billing-api",
      success: false
    });
    return {
      attempts,
      availability: {
        checks: [{ name: "auth", passed: true }],
        detailsVisible: false,
        status: "available"
      },
      lastError: { code, message: errorMessage(error) },
      provider: "codebuddy"
    };
  }
}

async function loadWorkBuddyAuthSession(): Promise<WorkBuddyAuthSession> {
  const authPath = join(homedir(), WORKBUDDY_AUTH_RELATIVE_PATH);
  const content = await readFile(authPath, "utf8");
  const parsed = JSON.parse(content) as Record<string, unknown>;
  const auth = objectValue(parsed.auth);
  const accessToken = stringValue(auth?.accessToken);
  if (!accessToken) {
    throw new Error("WorkBuddy 登录凭据中没有 accessToken。");
  }
  const expiresAt = Number(auth?.expiresAt) || 0;
  if (expiresAt && expiresAt <= Date.now()) {
    throw new Error(
      "WorkBuddy 登录凭据已过期，请在 WorkBuddy App 内重新登录。"
    );
  }
  const account = objectValue(parsed.account);
  return {
    accessToken,
    expiresAt,
    nickname: stringValue(account?.nickname) || "WorkBuddy"
  };
}

async function fetchWorkBuddyBilling(
  accessToken: string
): Promise<WorkBuddyBillingResponse> {
  const response = await outboundFetch(WORKBUDDY_BILLING_URL, {
    method: "POST",
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${accessToken}`,
      "Content-Type": "application/json",
      "User-Agent": "Tutti"
    },
    body: JSON.stringify({})
  });
  const text = await response.text();
  if (response.status === 401 || response.status === 403) {
    throw new Error(
      "WorkBuddy 计费接口返回未授权，请在 WorkBuddy App 内重新登录。"
    );
  }
  if (response.status === 429) {
    throw new Error("WorkBuddy 计费接口触发限流。");
  }
  if (!response.ok) {
    throw new Error(`WorkBuddy 计费接口返回 HTTP ${response.status}。`);
  }
  try {
    return JSON.parse(text) as WorkBuddyBillingResponse;
  } catch {
    throw new Error("WorkBuddy 计费接口返回了无效 JSON。");
  }
}

function workBuddyUsageFromBilling(
  response: WorkBuddyBillingResponse,
  capturedAtUnixMs: number
) {
  const accounts = response.data?.Response?.Data?.Accounts;
  if (!Array.isArray(accounts) || accounts.length === 0) {
    return null;
  }
  let amountRemaining = 0;
  let amountLimit = 0;
  let packageName = "";
  let resetsAtUnixMs: number | undefined;
  let sawCredits = false;
  for (const account of accounts) {
    const unit = stringValue(account.CapacityUnit).toLowerCase();
    const remain = numberValue(account.CapacityRemain);
    const size = numberValue(account.CapacitySize);
    if (unit && unit !== "credits") continue;
    if (remain === undefined || size === undefined) continue;
    sawCredits = true;
    amountRemaining += remain;
    amountLimit += size;
    packageName = packageName || stringValue(account.PackageName) || "";
    const cycleEnd = cycleEndTimeToUnixMs(account.CycleEndTime);
    if (
      cycleEnd &&
      (resetsAtUnixMs === undefined || cycleEnd > resetsAtUnixMs)
    ) {
      resetsAtUnixMs = cycleEnd;
    }
  }
  if (!sawCredits || amountLimit <= 0) {
    return null;
  }
  const percentRemaining = Math.max(
    0,
    Math.min(100, (amountRemaining / amountLimit) * 100)
  );
  const quota: AgentUsageQuota = {
    quotaType: "credits",
    percentRemaining,
    amountRemaining,
    amountLimit,
    amountUnit: "credits",
    ...(resetsAtUnixMs ? { resetsAtUnixMs } : {})
  };
  return {
    accountTier: packageName || undefined,
    billingMode: "provider_account" as const,
    quotaState: "complete" as const,
    capturedAtUnixMs,
    quotas: [quota]
  };
}

function cycleEndTimeToUnixMs(value: unknown): number | undefined {
  const text = stringValue(value);
  if (!text) return undefined;
  const parsed = Date.parse(`${text.replace(" ", "T")}+08:00`);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function workBuddyProbeErrorCode(error: unknown): string {
  const message = errorMessage(error);
  if (/未授权|重新登录|401|403/u.test(message)) return "auth_required";
  if (/限流|429/u.test(message)) return "rate_limited";
  if (/无效 JSON/u.test(message)) return "parse_failed";
  return "execution_failed";
}

function numberValue(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
}

function objectValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export async function workBuddyAuthFileFreshness(): Promise<{
  exists: boolean;
  modifiedAtUnixMs: number | null;
}> {
  try {
    const authPath = join(homedir(), WORKBUDDY_AUTH_RELATIVE_PATH);
    const stats = await stat(authPath);
    return { exists: true, modifiedAtUnixMs: stats.mtimeMs };
  } catch {
    return { exists: false, modifiedAtUnixMs: null };
  }
}
