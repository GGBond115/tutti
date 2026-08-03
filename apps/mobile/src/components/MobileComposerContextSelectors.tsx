import {
  NativeButton,
  NativeListRow,
  NativeSheet,
  type NativeTheme,
  useNativeTheme
} from "@tutti-os/ui-system/native";
import { useState } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { t } from "../i18n";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";

type ContextMenu = "agent" | "directory" | null;

export function MobileComposerContextSelectors({
  model,
  onSelectProject,
  onSelectTarget
}: {
  model: WorkspaceActivitySnapshot;
  onSelectProject(path: string | null): void;
  onSelectTarget(agentTargetId: string): void;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const [menu, setMenu] = useState<ContextMenu>(null);
  const selectedTarget =
    model.targets.find((target) => target.id === model.selectedAgentTargetId) ??
    null;
  const selectedProject =
    model.userProjects.find(
      (project) => project.path === model.selectedProjectPath
    ) ?? null;
  const disabled =
    !model.commandsAvailable || model.sending || model.ambiguousSubmission;

  const selectTarget = (agentTargetId: string): void => {
    if (disabled) return;
    onSelectTarget(agentTargetId);
    setMenu(null);
  };
  const selectProject = (path: string | null): void => {
    if (disabled) return;
    onSelectProject(path);
    setMenu(null);
  };

  return (
    <>
      <View style={styles.selectors}>
        <NativeButton
          accessibilityLabel={t("selectAgent")}
          disabled={disabled}
          label={`${t("agent")} · ${selectedTarget?.name ?? t("selectAgent")}`}
          onPress={() => setMenu("agent")}
          size="compact"
          style={styles.selector}
          testID="mobile-composer-agent-select"
          variant="secondary"
        />
        <NativeButton
          accessibilityLabel={t("selectWorkingDirectory")}
          disabled={disabled}
          label={`${t("workingDirectory")} · ${selectedProject?.label ?? t("noProject")}`}
          loading={model.userProjectsStatus === "loading"}
          onPress={() => setMenu("directory")}
          size="compact"
          style={styles.selector}
          testID="mobile-composer-directory-select"
          variant="secondary"
        />
      </View>

      <NativeSheet
        closeAccessibilityLabel={t("closeSheet")}
        onOpenChange={(open) => !open && setMenu(null)}
        open={!disabled && menu !== null}
      >
        <View style={styles.sheet}>
          <Text style={styles.sheetTitle}>
            {menu === "agent" ? t("selectAgent") : t("workingDirectory")}
          </Text>
          <ScrollView contentContainerStyle={styles.options}>
            {menu === "agent"
              ? model.targets.map((target) => (
                  <NativeListRow
                    key={target.id}
                    onPress={() => selectTarget(target.id)}
                    selected={target.id === model.selectedAgentTargetId}
                    title={target.name}
                  />
                ))
              : null}
            {menu === "directory" ? (
              <NativeListRow
                description={t("noProjectDescription")}
                onPress={() => selectProject(null)}
                selected={model.selectedProjectPath === null}
                title={t("noProject")}
              />
            ) : null}
            {menu === "directory"
              ? model.userProjects.map((project) => (
                  <NativeListRow
                    description={project.path}
                    key={project.id}
                    onPress={() => selectProject(project.path)}
                    selected={project.path === model.selectedProjectPath}
                    title={project.label}
                  />
                ))
              : null}
            {menu === "directory" && model.userProjectErrorCode ? (
              <Text style={styles.error}>{t("projectDirectoryLoadError")}</Text>
            ) : null}
          </ScrollView>
        </View>
      </NativeSheet>
    </>
  );
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    error: {
      color: theme.color.danger,
      padding: theme.space.medium,
      textAlign: "center"
    },
    options: { padding: theme.space.small },
    selector: { flex: 1, minWidth: 0 },
    selectors: { flexDirection: "row", gap: theme.space.small },
    sheet: { minHeight: 220, padding: theme.space.medium },
    sheetTitle: {
      color: theme.color.text,
      fontSize: 17,
      fontWeight: "700",
      marginBottom: theme.space.small
    }
  });
}
