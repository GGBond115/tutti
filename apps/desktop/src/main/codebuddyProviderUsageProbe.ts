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
  PackageCode?: unknown;
  PackageName?: unknown;
  Status?: unknown;
  CycleCapacityRemainPrecise?: unknown;
  CycleCapacitySizePrecise?: unknown;
  CycleEndTime?: unknown;
  DeductionEndTime?: unknown;
  ExpiredTime?: unknown;
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

const WORKBUDDY_PACKAGE_NAMES: Record<string, string> = {
  TCACA_code_001_PqouKr6QWV: "免费版",
  TCACA_code_002_AkiJS3ZHF5: "Pro 月付版",
  TCACA_code_005_maRGyrHhw1: "Pro 月付版",
  TCACA_code_006_DbXS0lrypC: "Pro 试用版",
  TCACA_code_007_nzdH5h4Nl0: "成长计划积分",
  TCACA_code_003_FAnt7lcmRT: "Pro 年付版",
  TCACA_code_008_cfWoLwvjU4: "体验版（每日积分）",
  TCACA_code_009_0XmEQc2xOf: "积分加油包",
  TCACA_code_023_4xbGhMrE6q: "青年版",
  TCACA_code_026_BaESVICNoi: "高级版",
  TCACA_code_027_0FCGVA6vSa: "旗舰版",
  TCACA_code_028_NtpWi0jzXs: "奖励积分包",
  TCACA_code_029_6wCGEWquYy: "奖励积分包",
  TCACA_code_030_BjSt89qTvr: "奖励积分包",
  TCACA_code_035_ArVxJcGDsm: "体验版（每日积分）",
  TCACA_code_036_lupO5WgNdG: "积分加油包",
  TCACA_code_037_WxOD3MpI2o: "奖励积分包",
  TCACA_code_038_OhvqZtiPKr: "积分加油包",
  TCACA_code_039_KRcQj7wUat: "Pro 试用包",
  TCACA_code_040_mi9rCYg46x: "Pro 试用包"
};

const ACTIVE_PLAN_PRIORITY: string[] = [
  "TCACA_code_027_0FCGVA6vSa",
  "TCACA_code_026_BaESVICNoi",
  "TCACA_code_023_4xbGhMrE6q",
  "TCACA_code_003_FAnt7lcmRT",
  "TCACA_code_002_AkiJS3ZHF5",
  "TCACA_code_005_maRGyrHhw1",
  "TCACA_code_006_DbXS0lrypC",
  "TCACA_code_008_cfWoLwvjU4",
  "TCACA_code_039_KRcQj7wUat",
  "TCACA_code_040_mi9rCYg46x"
];

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
  const now = new Date();
  const pad = (n: number): string => n.toString().padStart(2, "0");
  const formatDate = (d: Date): string =>
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
      d.getHours()
    )}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  const response = await outboundFetch(WORKBUDDY_BILLING_URL, {
    method: "POST",
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${accessToken}`,
      "Content-Type": "application/json",
      "User-Agent": "Tutti"
    },
    body: JSON.stringify({
      PageNumber: 1,
      PageSize: 100,
      ProductCode: "p_tcaca",
      Status: [0, 3],
      PackageStartTimeRangeBegin: "2024-12-01 21:25:00",
      PackageStartTimeRangeEnd: formatDate(now)
    })
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
  let activePlan: WorkBuddyBillingAccount | null = null;
  let activePlanPriority = Number.POSITIVE_INFINITY;
  let nearestResetUnixMs: number | undefined;
  for (const account of accounts) {
    const remain = preciseNumberValue(account.CycleCapacityRemainPrecise);
    const size = preciseNumberValue(account.CycleCapacitySizePrecise);
    if (remain === undefined || size === undefined) continue;
    amountRemaining += remain;
    amountLimit += size;
    const packageCode = stringValue(account.PackageCode);
    const priority = ACTIVE_PLAN_PRIORITY.indexOf(packageCode);
    if (priority >= 0 && priority < activePlanPriority) {
      activePlanPriority = priority;
      activePlan = account;
    }
    const cycleEnd = cycleEndTimeToUnixMs(account.CycleEndTime);
    if (
      remain > 0 &&
      cycleEnd &&
      (nearestResetUnixMs === undefined || cycleEnd < nearestResetUnixMs)
    ) {
      nearestResetUnixMs = cycleEnd;
    }
  }
  if (amountLimit <= 0) {
    return null;
  }
  const percentRemaining = Math.max(
    0,
    Math.min(100, (amountRemaining / amountLimit) * 100)
  );
  const tierName = activePlan
    ? WORKBUDDY_PACKAGE_NAMES[stringValue(activePlan.PackageCode)] ||
      stringValue(activePlan.PackageName) ||
      "WorkBuddy"
    : "WorkBuddy";
  const resetFromActivePlan = activePlan
    ? cycleEndTimeToUnixMs(activePlan.CycleEndTime)
    : undefined;
  const resetsAtUnixMs = resetFromActivePlan ?? nearestResetUnixMs;
  const quota: AgentUsageQuota = {
    quotaType: "credits",
    percentRemaining,
    amountRemaining: Math.round(amountRemaining * 100) / 100,
    amountLimit: Math.round(amountLimit * 100) / 100,
    amountUnit: "credits",
    ...(resetsAtUnixMs ? { resetsAtUnixMs } : {})
  };
  return {
    accountTier: tierName,
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

function preciseNumberValue(value: unknown): number | undefined {
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
