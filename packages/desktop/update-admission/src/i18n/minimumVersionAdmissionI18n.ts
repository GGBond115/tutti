import {
  createI18nRuntime,
  type I18nDictionary,
  type I18nRuntime
} from "@tutti-os/ui-i18n-runtime";

export type MinimumVersionAdmissionLocale = "en" | "zh-CN";
export type MinimumVersionAdmissionI18nKey =
  | "startupTitle"
  | "startupDetail"
  | "foregroundTitle"
  | "foregroundDetail"
  | "checkingTitle"
  | "downloadingTitle"
  | "failedTitle"
  | "simulationCompleteTitle"
  | "simulationCompleteDetail"
  | "currentVersion"
  | "minimumVersion"
  | "downloadProgress"
  | "autoRestartNotice"
  | "upgrade"
  | "upgradeNow"
  | "later"
  | "retry"
  | "manualDownload"
  | "exit"
  | "errors.releaseBelowMinimum"
  | "errors.updateUnavailable"
  | "errors.policyCheckFailed"
  | "errors.installFailed"
  | "errors.updateFailed";

export type MinimumVersionAdmissionI18nRuntime =
  I18nRuntime<MinimumVersionAdmissionI18nKey>;

export const minimumVersionAdmissionEn = {
  startupTitle: "A required {{productName}} update is available",
  startupDetail:
    "Update {{productName}} before continuing. Your regular update preference will not be changed.",
  foregroundTitle: "A required {{productName}} update is available",
  foregroundDetail:
    "Update Now automatically installs and restarts after downloading. You can also continue this session and update next time.",
  checkingTitle: "Checking the required update",
  downloadingTitle: "Downloading the required update",
  failedTitle: "Unable to complete the required update",
  simulationCompleteTitle: "Development update simulation completed",
  simulationCompleteDetail:
    "The update reached the installation step. Development mode will not install or restart {{productName}}.",
  currentVersion: "Current version",
  minimumVersion: "Minimum version",
  downloadProgress: "Download progress",
  autoRestartNotice:
    "{{productName}} will install the update and restart automatically when the download completes.",
  upgrade: "Upgrade",
  upgradeNow: "Upgrade now",
  later: "Later",
  retry: "Retry",
  manualDownload: "Manual download",
  exit: "Exit",
  errors: {
    releaseBelowMinimum:
      "The latest published update is below the required minimum version.",
    updateUnavailable:
      "No suitable update is available from the release source.",
    policyCheckFailed: "The minimum-version policy could not be checked.",
    installFailed: "The update was downloaded but could not be installed.",
    updateFailed: "The update could not be downloaded or verified."
  }
} as const satisfies I18nDictionary;

export const minimumVersionAdmissionZhCN = {
  startupTitle: "{{productName}} 必须更新后才能继续",
  startupDetail: "请先更新 {{productName}}，普通更新偏好不会被修改",
  foregroundTitle: "{{productName}} 有必须安装的更新",
  foregroundDetail:
    "立即升级会在下载完成后自动安装并重启，你也可以继续本次会话并在下次启动时更新",
  checkingTitle: "正在检查必须安装的更新",
  downloadingTitle: "正在下载必须安装的更新",
  failedTitle: "无法完成必须安装的更新",
  simulationCompleteTitle: "开发更新模拟已完成",
  simulationCompleteDetail:
    "更新流程已到达安装阶段，开发模式不会实际安装或重启 {{productName}}",
  currentVersion: "当前版本",
  minimumVersion: "最低版本",
  downloadProgress: "下载进度",
  autoRestartNotice: "下载完成后，{{productName}} 将自动安装更新并重启",
  upgrade: "升级",
  upgradeNow: "立即升级",
  later: "稍后",
  retry: "重试",
  manualDownload: "手动下载",
  exit: "退出",
  errors: {
    releaseBelowMinimum: "当前发布的最新版本仍低于要求的最低版本",
    updateUnavailable: "发布源中没有可用的更新",
    policyCheckFailed: "无法检查最低版本策略，请检查网络后重试",
    installFailed: "更新已下载，但无法安装",
    updateFailed: "更新下载或校验失败"
  }
} as const satisfies I18nDictionary;

export function createMinimumVersionAdmissionI18nRuntime(
  locale: MinimumVersionAdmissionLocale,
  overrides?: I18nDictionary
): MinimumVersionAdmissionI18nRuntime {
  return createI18nRuntime<MinimumVersionAdmissionI18nKey>({
    dictionaries: [
      overrides ?? {},
      locale === "zh-CN"
        ? minimumVersionAdmissionZhCN
        : minimumVersionAdmissionEn
    ]
  });
}
