import type { AgentActivitySessionSettings } from "@tutti-os/agent-activity-core";
import {
  NativeListRow,
  NativeSheet,
  type NativeTheme,
  useNativeTheme
} from "@tutti-os/ui-system/native";
import { useState } from "react";
import { ScrollView, StyleSheet, Text, View } from "react-native";
import { t } from "../i18n";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";

type ComposerSettingMenu =
  | "model"
  | "reasoning"
  | "speed"
  | "permission"
  | "plan"
  | null;

export function MobileComposerSettingsSheet({
  model,
  onUpdate
}: {
  model: WorkspaceActivitySnapshot;
  onUpdate(settings: AgentActivitySessionSettings): void;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const [menu, setMenu] = useState<ComposerSettingMenu>(null);
  const options = model.composerOptions;
  const selectedModel = model.composerSettings.model ?? null;
  const reasoningOptions = selectedModel
    ? (options?.reasoningOptionsByModel?.[selectedModel]?.options ??
      options?.reasoningEfforts ??
      [])
    : (options?.reasoningEfforts ?? []);

  const closeWith = (settings: AgentActivitySessionSettings): void => {
    onUpdate(settings);
    setMenu(null);
  };

  return (
    <>
      <View style={styles.chips}>
        {model.composerSettingsSupport.model && options?.models.length ? (
          <ComposerChip
            label={
              selectedOptionLabel(options?.models ?? [], selectedModel) ??
              t("model")
            }
            onPress={() => setMenu("model")}
          />
        ) : null}
        {model.composerSettingsSupport.reasoning && reasoningOptions.length ? (
          <ComposerChip
            label={
              selectedOptionLabel(
                reasoningOptions,
                model.composerSettings.reasoningEffort ?? null
              ) ?? t("reasoning")
            }
            onPress={() => setMenu("reasoning")}
          />
        ) : null}
        {model.composerSettingsSupport.speed && options?.speeds.length ? (
          <ComposerChip
            label={
              selectedOptionLabel(
                options?.speeds ?? [],
                model.composerSettings.speed ?? null
              ) ?? t("speed")
            }
            onPress={() => setMenu("speed")}
          />
        ) : null}
        {model.composerSettingsSupport.permission &&
        options?.permissionConfig?.modes.length ? (
          <ComposerChip
            label={
              selectedOptionLabel(
                options?.permissionConfig?.modes ?? [],
                model.composerSettings.permissionModeId ?? null
              ) ?? t("permissions")
            }
            onPress={() => setMenu("permission")}
          />
        ) : null}
        {model.composerSettingsSupport.plan ? (
          <ComposerChip
            label={
              model.composerSettings.planMode ? t("planModeOn") : t("planMode")
            }
            onPress={() => setMenu("plan")}
          />
        ) : null}
      </View>

      <NativeSheet
        onOpenChange={(open) => !open && setMenu(null)}
        open={menu !== null}
      >
        <View style={styles.sheet}>
          <Text style={styles.sheetTitle}>{titleForMenu(menu)}</Text>
          <ScrollView contentContainerStyle={styles.options}>
            {menu === "model"
              ? options?.models.map((option) => (
                  <NativeListRow
                    key={option.value}
                    onPress={() => closeWith({ model: option.value })}
                    selected={option.value === selectedModel}
                    title={option.label}
                  />
                ))
              : null}
            {menu === "reasoning"
              ? reasoningOptions.map((option) => (
                  <NativeListRow
                    key={option.value}
                    onPress={() => closeWith({ reasoningEffort: option.value })}
                    selected={
                      option.value === model.composerSettings.reasoningEffort
                    }
                    title={option.label}
                  />
                ))
              : null}
            {menu === "speed"
              ? options?.speeds.map((option) => (
                  <NativeListRow
                    key={option.value}
                    onPress={() => closeWith({ speed: option.value })}
                    selected={option.value === model.composerSettings.speed}
                    title={option.label}
                  />
                ))
              : null}
            {menu === "permission"
              ? options?.permissionConfig?.modes.map((option) => (
                  <NativeListRow
                    description={option.description}
                    key={option.id}
                    onPress={() => closeWith({ permissionModeId: option.id })}
                    selected={
                      option.id === model.composerSettings.permissionModeId
                    }
                    title={option.label ?? option.id}
                  />
                ))
              : null}
            {menu === "plan" ? (
              <>
                <NativeListRow
                  onPress={() => closeWith({ planMode: false })}
                  selected={!model.composerSettings.planMode}
                  title={t("planModeOff")}
                />
                <NativeListRow
                  onPress={() => closeWith({ planMode: true })}
                  selected={model.composerSettings.planMode === true}
                  title={t("planModeOn")}
                />
              </>
            ) : null}
            {menu !== "plan" &&
            model.composerOptionsLoadStatus === "loading" ? (
              <Text style={styles.loading}>{t("loading")}</Text>
            ) : null}
          </ScrollView>
        </View>
      </NativeSheet>
    </>
  );
}

function ComposerChip({ label, onPress }: { label: string; onPress(): void }) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  return <NativeListRow onPress={onPress} style={styles.chip} title={label} />;
}

function selectedOptionLabel(
  options: readonly { label?: string; value?: string; id?: string }[],
  value: string | null
): string | null {
  if (!value) return null;
  const option = options.find(
    (candidate) => candidate.value === value || candidate.id === value
  );
  return option?.label ?? value;
}

function titleForMenu(menu: ComposerSettingMenu): string {
  switch (menu) {
    case "model":
      return t("model");
    case "reasoning":
      return t("reasoning");
    case "speed":
      return t("speed");
    case "permission":
      return t("permissions");
    case "plan":
      return t("planMode");
    default:
      return "";
  }
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    chip: {
      backgroundColor: theme.color.panel,
      borderColor: theme.color.border,
      borderRadius: theme.radius.large,
      borderWidth: StyleSheet.hairlineWidth,
      minHeight: theme.control.compact,
      paddingHorizontal: theme.space.small
    },
    chips: { flexDirection: "row", flexWrap: "wrap", gap: theme.space.small },
    loading: { color: theme.color.muted, padding: theme.space.medium },
    options: { padding: theme.space.small },
    sheet: { minHeight: 180, padding: theme.space.medium },
    sheetTitle: {
      color: theme.color.text,
      fontSize: 17,
      fontWeight: "700",
      marginBottom: theme.space.small
    }
  });
}
