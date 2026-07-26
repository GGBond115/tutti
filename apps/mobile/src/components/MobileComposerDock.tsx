import type { AgentActivitySessionSettings } from "@tutti-os/agent-activity-core";
import {
  NativeIconButton,
  NativeListRow,
  type NativeTheme,
  useNativeTheme
} from "@tutti-os/ui-system/native";
import { useState } from "react";
import {
  ActivityIndicator,
  Modal,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View
} from "react-native";
import { t } from "../i18n";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";
import { MobileComposerSettingsSheet } from "./MobileComposerSettingsSheet";

type ComposerToolsMenu = "tools" | "model" | "permission" | null;

export function MobileComposerDock({
  model,
  onDraftChange,
  onSend,
  onStop,
  onUpdate
}: {
  model: WorkspaceActivitySnapshot;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  onUpdate(settings: AgentActivitySessionSettings): void;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const [menu, setMenu] = useState<ComposerToolsMenu>(null);
  const hasActiveTurn = Boolean(
    model.selectedSession?.activeTurnId && !model.creating
  );
  const canSend = Boolean(
    model.draft.trim() &&
    (!model.creating || model.selectedAgentTargetId) &&
    !model.sending
  );
  const modelOptions = model.composerOptions?.models ?? [];
  const permissionOptions =
    model.composerOptions?.permissionConfig?.modes ?? [];
  const hasComposerTools =
    (model.composerSettingsSupport.model && modelOptions.length > 0) ||
    (model.composerSettingsSupport.permission &&
      permissionOptions.length > 0) ||
    model.composerSettingsSupport.plan ||
    model.composerOptionsLoadStatus === "loading";
  const selectedModelLabel =
    modelOptions.find((option) => option.value === model.composerSettings.model)
      ?.label ?? t("model");
  const selectedPermissionLabel =
    permissionOptions.find(
      (option) => option.id === model.composerSettings.permissionModeId
    )?.label ?? t("defaultPermissions");

  return (
    <View style={styles.dock}>
      <MobileComposerSettingsSheet model={model} onUpdate={onUpdate} />
      <View style={styles.inputRow}>
        <NativeIconButton
          accessibilityLabel={t("moreActions")}
          disabled={!hasComposerTools}
          icon={<Text style={styles.plus}>＋</Text>}
          onPress={() => setMenu((current) => (current ? null : "tools"))}
          style={styles.addButton}
          variant="secondary"
        />
        <View style={styles.inputPill}>
          <TextInput
            editable={!model.sending}
            multiline
            onChangeText={onDraftChange}
            placeholder={t("messageHint")}
            placeholderTextColor={theme.color.muted}
            style={styles.input}
            value={model.draft}
          />
          {hasActiveTurn ? (
            <NativeIconButton
              accessibilityLabel={t("stop")}
              icon={<Text style={styles.actionIcon}>■</Text>}
              onPress={onStop}
              style={styles.actionButton}
            />
          ) : canSend ? (
            <NativeIconButton
              accessibilityLabel={
                model.ambiguousSubmission ? t("retry") : t("send")
              }
              icon={<Text style={styles.sendIcon}>↑</Text>}
              onPress={onSend}
              style={styles.actionButton}
            />
          ) : model.sending ? (
            <ActivityIndicator color={theme.color.text} size="small" />
          ) : (
            <MicrophoneGlyph theme={theme} />
          )}
        </View>
      </View>

      <Modal
        animationType="fade"
        onRequestClose={() => setMenu(null)}
        presentationStyle="overFullScreen"
        statusBarTranslucent
        transparent
        visible={menu !== null}
      >
        <Pressable onPress={() => setMenu(null)} style={styles.backdrop}>
          <Pressable
            onPress={(event) => event.stopPropagation()}
            style={styles.menu}
          >
            {menu === "tools" ? (
              <>
                {model.composerSettingsSupport.model &&
                modelOptions.length > 0 ? (
                  <NativeListRow
                    description={selectedModelLabel}
                    onPress={() => setMenu("model")}
                    title={t("model")}
                    trailing={<Text style={styles.chevron}>›</Text>}
                  />
                ) : null}
                {model.composerSettingsSupport.permission &&
                permissionOptions.length > 0 ? (
                  <NativeListRow
                    description={selectedPermissionLabel}
                    onPress={() => setMenu("permission")}
                    title={t("permissions")}
                    trailing={<Text style={styles.chevron}>›</Text>}
                  />
                ) : null}
                {model.composerSettingsSupport.plan ? (
                  <NativeListRow
                    description={
                      model.composerSettings.planMode
                        ? t("planModeOn")
                        : t("planModeOff")
                    }
                    onPress={() => {
                      onUpdate({
                        planMode: !model.composerSettings.planMode
                      });
                      setMenu(null);
                    }}
                    selected={model.composerSettings.planMode === true}
                    title={t("planMode")}
                  />
                ) : null}
                {model.composerOptionsLoadStatus === "loading" ? (
                  <ActivityIndicator
                    color={theme.color.accent}
                    size="small"
                    style={styles.loading}
                  />
                ) : null}
              </>
            ) : (
              <View style={styles.menuHeader}>
                <NativeIconButton
                  accessibilityLabel={t("cancel")}
                  icon={<Text style={styles.menuBackIcon}>←</Text>}
                  onPress={() => setMenu("tools")}
                  style={styles.menuBackButton}
                />
                <Text style={styles.menuTitle}>
                  {menu === "model" ? t("model") : t("permissions")}
                </Text>
              </View>
            )}
            {menu === "model"
              ? modelOptions.map((option) => (
                  <NativeListRow
                    key={option.value}
                    onPress={() => {
                      onUpdate({ model: option.value });
                      setMenu(null);
                    }}
                    selected={option.value === model.composerSettings.model}
                    title={option.label}
                  />
                ))
              : null}
            {menu === "permission"
              ? permissionOptions.map((option) => (
                  <NativeListRow
                    description={option.description}
                    key={option.id}
                    onPress={() => {
                      onUpdate({ permissionModeId: option.id });
                      setMenu(null);
                    }}
                    selected={
                      option.id === model.composerSettings.permissionModeId
                    }
                    title={option.label ?? option.id}
                  />
                ))
              : null}
            {menu !== "tools" &&
            model.composerOptionsLoadStatus === "loading" ? (
              <ActivityIndicator
                color={theme.color.accent}
                size="small"
                style={styles.loading}
              />
            ) : null}
          </Pressable>
        </Pressable>
      </Modal>
    </View>
  );
}

function MicrophoneGlyph({ theme }: { theme: NativeTheme }) {
  const styles = createStyles(theme);
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no"
      style={styles.mic}
    >
      <View style={styles.micCapsule} />
      <View style={styles.micStem} />
      <View style={styles.micBase} />
    </View>
  );
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    actionButton: {
      alignItems: "center",
      backgroundColor: theme.color.text,
      borderRadius: 22,
      height: 44,
      justifyContent: "center",
      width: 44
    },
    actionIcon: {
      color: theme.color.background,
      fontSize: 11
    },
    backdrop: {
      bottom: 0,
      left: 0,
      position: "absolute",
      right: 0,
      top: 0
    },
    chevron: {
      color: theme.color.muted,
      fontSize: 25,
      lineHeight: 27
    },
    addButton: {
      alignItems: "center",
      backgroundColor: theme.color.panelRaised,
      borderColor: theme.color.border,
      borderRadius: 28,
      borderWidth: StyleSheet.hairlineWidth,
      height: 56,
      justifyContent: "center",
      width: 56
    },
    dock: {
      gap: theme.space.small,
      paddingBottom: theme.space.small,
      paddingHorizontal: theme.space.medium,
      paddingTop: theme.space.small
    },
    input: {
      color: theme.color.text,
      flex: 1,
      fontSize: 17,
      lineHeight: 22,
      maxHeight: 132,
      minHeight: 54,
      paddingLeft: theme.space.medium,
      paddingVertical: 15
    },
    inputPill: {
      alignItems: "center",
      backgroundColor: theme.color.panelRaised,
      borderColor: theme.color.border,
      borderRadius: 28,
      borderWidth: StyleSheet.hairlineWidth,
      flex: 1,
      flexDirection: "row",
      gap: theme.space.small,
      minHeight: 56,
      paddingRight: 6
    },
    inputRow: {
      alignItems: "flex-end",
      flexDirection: "row",
      gap: theme.space.small
    },
    loading: { marginVertical: theme.space.medium },
    mic: {
      alignItems: "center",
      height: 36,
      justifyContent: "center",
      marginRight: theme.space.small,
      width: 28
    },
    micBase: {
      backgroundColor: theme.color.textSecondary,
      borderRadius: 2,
      height: 2,
      marginTop: 3,
      width: 14
    },
    micCapsule: {
      borderColor: theme.color.textSecondary,
      borderRadius: 7,
      borderWidth: 2,
      height: 18,
      width: 12
    },
    micStem: {
      backgroundColor: theme.color.textSecondary,
      height: 5,
      width: 2
    },
    plus: {
      color: theme.color.text,
      fontSize: 31,
      fontWeight: "300",
      lineHeight: 34
    },
    sendIcon: {
      color: theme.color.background,
      fontSize: 25,
      fontWeight: "700",
      lineHeight: 27
    },
    menu: {
      backgroundColor: theme.color.panelRaised,
      borderColor: theme.color.border,
      borderRadius: 24,
      borderWidth: StyleSheet.hairlineWidth,
      bottom: 96,
      left: theme.space.medium,
      maxHeight: 560,
      padding: theme.space.small,
      position: "absolute",
      right: theme.space.medium,
      shadowColor: theme.color.text,
      shadowOffset: { height: 10, width: 0 },
      shadowOpacity: 0.16,
      shadowRadius: 24,
      elevation: 12
    },
    menuBackButton: {
      height: 40,
      width: 40
    },
    menuBackIcon: {
      color: theme.color.text,
      fontSize: 24
    },
    menuHeader: {
      alignItems: "center",
      flexDirection: "row",
      gap: theme.space.small,
      minHeight: 50
    },
    menuTitle: {
      color: theme.color.text,
      fontSize: 17,
      fontWeight: "700",
      paddingRight: theme.space.medium
    }
  });
}
