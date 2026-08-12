export interface DesktopStartupFailureDialogDependencies {
  locale: string;
  logsDirectory: string;
  platform: NodeJS.Platform;
  openPath(path: string): Promise<string>;
  showMessageBox(options: {
    buttons: string[];
    cancelId: number;
    defaultId: number;
    detail: string;
    message: string;
    title: string;
    type: "error";
  }): Promise<{ response: number }>;
}

export async function showDesktopStartupFailureDialog(
  dependencies: DesktopStartupFailureDialogDependencies
): Promise<void> {
  const chinese = dependencies.locale.toLowerCase().startsWith("zh");
  const openLogs = chinese ? "打开日志目录" : "Open logs folder";
  const exit = chinese ? "退出" : "Exit";
  const detail =
    dependencies.platform === "win32"
      ? chinese
        ? "本地服务启动失败。您的数据未被删除；可打开日志目录用于排查，或卸载时选择删除全部用户数据后重新安装。"
        : "The local service failed to start. Your data was not deleted. Open the logs folder for diagnosis, or choose to delete all user data when uninstalling before reinstalling."
      : chinese
        ? "本地服务启动失败。您的数据未被删除；请打开日志目录用于排查。"
        : "The local service failed to start. Your data was not deleted. Open the logs folder for diagnosis.";
  const result = await dependencies.showMessageBox({
    buttons: [openLogs, exit],
    cancelId: 1,
    defaultId: 0,
    detail,
    message: chinese ? "Tutti 无法启动" : "Tutti could not start",
    title: "Tutti",
    type: "error"
  });
  if (result.response === 0) {
    await dependencies.openPath(dependencies.logsDirectory);
  }
}
