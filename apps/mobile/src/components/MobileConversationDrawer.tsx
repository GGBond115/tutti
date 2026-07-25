import { useState } from "react";
import {
  NativeButton,
  NativeIconButton,
  NativeListRow,
  NativeSheet,
  type NativeTheme,
  useNativeTheme
} from "@tutti-os/ui-system/native";
import {
  ActivityIndicator,
  Image,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View
} from "react-native";
import { t } from "../i18n";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";

type DrawerDialog =
  | { kind: "actions"; sessionId: string }
  | { kind: "delete"; sessionId: string }
  | { kind: "rename"; sessionId: string }
  | null;

export function MobileConversationDrawer({
  model,
  onBack,
  onClose,
  onDeleteSession,
  onLoadMoreSessions,
  onNewSession,
  onRenameSession,
  onRefreshSessions,
  onSelectSession,
  onTogglePinned,
  workspaceName
}: {
  model: WorkspaceActivitySnapshot;
  onBack(): void;
  onClose(): void;
  onDeleteSession(id: string): Promise<void>;
  onLoadMoreSessions(sectionId: string): void;
  onNewSession(): void;
  onRenameSession(id: string, title: string): Promise<void>;
  onRefreshSessions(): Promise<void>;
  onSelectSession(id: string): void;
  onTogglePinned(id: string): Promise<void>;
  workspaceName: string;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const [dialog, setDialog] = useState<DrawerDialog>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [actionPending, setActionPending] = useState(false);
  const [collapsedSectionIds, setCollapsedSectionIds] = useState<Set<string>>(
    () => new Set()
  );
  const [refreshing, setRefreshing] = useState(false);
  const actionSession = dialog
    ? model.railSections
        .flatMap((section) => section.items)
        .find((session) => session.id === dialog.sessionId)
    : null;

  const runAction = async (action: () => Promise<void>): Promise<void> => {
    if (actionPending) return;
    setActionPending(true);
    try {
      await action();
      setDialog(null);
    } finally {
      setActionPending(false);
    }
  };

  const toggleSection = (sectionId: string): void => {
    setCollapsedSectionIds((current) => {
      const next = new Set(current);
      if (next.has(sectionId)) {
        next.delete(sectionId);
      } else {
        next.add(sectionId);
      }
      return next;
    });
  };

  const refreshRail = async (): Promise<void> => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      await onRefreshSessions();
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <View style={styles.layer}>
      <Pressable onPress={onClose} style={styles.scrim} />
      <View style={styles.drawer}>
        <View style={styles.header}>
          <View style={styles.heading}>
            <Text numberOfLines={1} style={styles.workspace}>
              {workspaceName}
            </Text>
            <Text style={styles.title}>{t("sessions")}</Text>
          </View>
          <View style={styles.headerActions}>
            {refreshing ? (
              <ActivityIndicator color={theme.color.accent} size="small" />
            ) : (
              <NativeIconButton
                accessibilityLabel={t("refreshSessions")}
                icon={<Text style={styles.refresh}>↻</Text>}
                onPress={() => void refreshRail()}
                style={styles.closeButton}
              />
            )}
            <NativeIconButton
              accessibilityLabel={t("cancel")}
              icon={<Text style={styles.close}>×</Text>}
              onPress={onClose}
              style={styles.closeButton}
            />
          </View>
        </View>

        <NativeButton
          disabled={model.targets.length === 0}
          label={t("newSession")}
          leading={<Text style={styles.newSessionIcon}>＋</Text>}
          onPress={() => {
            onNewSession();
            onClose();
          }}
          style={styles.newSessionButton}
          variant="secondary"
        />

        <ScrollView contentContainerStyle={styles.list}>
          {model.railStatus === "loading" && model.railSections.length === 0 ? (
            <View style={styles.feedback}>
              <ActivityIndicator color={theme.color.accent} size="small" />
            </View>
          ) : model.railErrorCode && model.railSections.length === 0 ? (
            <View style={styles.feedback}>
              <Text style={styles.feedbackText}>{t("genericError")}</Text>
              <NativeButton
                label={t("retry")}
                onPress={() => void refreshRail()}
                size="compact"
                variant="ghost"
              />
            </View>
          ) : model.railSections.length === 0 ? (
            <Text style={styles.empty}>{t("emptySessions")}</Text>
          ) : (
            <>
              {model.railErrorCode ? (
                <View style={styles.inlineError}>
                  <Text style={styles.inlineErrorText}>
                    {t("genericError")}
                  </Text>
                  <NativeButton
                    label={t("retry")}
                    onPress={() => void refreshRail()}
                    size="compact"
                    variant="ghost"
                  />
                </View>
              ) : null}
              {model.railSections.map((section) => {
                const collapsed = collapsedSectionIds.has(section.id);
                return (
                  <View key={section.id} style={styles.section}>
                    <Pressable
                      accessibilityLabel={
                        collapsed ? t("expandSection") : t("collapseSection")
                      }
                      accessibilityRole="button"
                      accessibilityState={{ expanded: !collapsed }}
                      onPress={() => toggleSection(section.id)}
                      style={({ pressed }) => [
                        styles.sectionHeader,
                        pressed && styles.pressed
                      ]}
                    >
                      <Text numberOfLines={1} style={styles.sectionTitle}>
                        {sectionTitle(section)}
                      </Text>
                      <Text style={styles.sectionCount}>
                        {section.totalCount}
                      </Text>
                      <Text style={styles.sectionChevron}>
                        {collapsed ? "›" : "⌄"}
                      </Text>
                    </Pressable>
                    {!collapsed
                      ? section.items.map((session) => {
                          const selected =
                            session.id === model.selectedAgentSessionId;
                          const target =
                            model.targets.find(
                              (candidate) =>
                                candidate.id === session.agentTargetId
                            ) ??
                            model.targets.find(
                              (candidate) =>
                                candidate.provider === session.provider
                            );
                          return (
                            <NativeListRow
                              accessibilityLabel={
                                session.title || t("untitledSession")
                              }
                              key={session.id}
                              leading={
                                <View style={styles.agentIconFrame}>
                                  {target?.iconUrl ? (
                                    <Image
                                      source={{ uri: target.iconUrl }}
                                      style={styles.agentIcon}
                                    />
                                  ) : (
                                    <Text style={styles.agentIconFallback}>
                                      {session.provider
                                        .slice(0, 1)
                                        .toUpperCase()}
                                    </Text>
                                  )}
                                  <View
                                    style={[
                                      styles.statusDot,
                                      statusDotStyle(theme, session.status)
                                    ]}
                                  />
                                </View>
                              }
                              onPress={() => {
                                onSelectSession(session.id);
                                onClose();
                              }}
                              selected={selected}
                              description={
                                <View style={styles.sessionMeta}>
                                  <Text style={styles.sessionTime}>
                                    {formatSessionTime(
                                      session.sortTimeUnixMs ??
                                        session.updatedAtUnixMs
                                    )}
                                  </Text>
                                  <Text style={styles.sessionStatus}>
                                    {sessionStatusLabel(session.status)}
                                  </Text>
                                  {session.needsUserAction ? (
                                    <Text style={styles.attention}>
                                      {t("needsAttention")}
                                    </Text>
                                  ) : null}
                                </View>
                              }
                              title={session.title || t("untitledSession")}
                              trailing={
                                <NativeIconButton
                                  accessibilityLabel={t("moreActions")}
                                  icon={<Text style={styles.moreIcon}>⋯</Text>}
                                  onPress={(event) => {
                                    event.stopPropagation();
                                    setDialog({
                                      kind: "actions",
                                      sessionId: session.id
                                    });
                                  }}
                                  style={styles.moreButton}
                                />
                              }
                            />
                          );
                        })
                      : null}
                    {!collapsed && section.hasMore ? (
                      <Pressable
                        disabled={section.loadingMore}
                        onPress={() => onLoadMoreSessions(section.id)}
                        style={({ pressed }) => [
                          styles.loadMoreButton,
                          pressed && styles.pressed
                        ]}
                      >
                        {section.loadingMore ? (
                          <ActivityIndicator
                            color={theme.color.accent}
                            size="small"
                          />
                        ) : (
                          <Text style={styles.loadMoreLabel}>
                            {t("loadMoreSessions")}
                          </Text>
                        )}
                      </Pressable>
                    ) : null}
                  </View>
                );
              })}
            </>
          )}
        </ScrollView>

        <Pressable onPress={onBack} style={styles.workspaceBackButton}>
          <Text style={styles.workspaceBackLabel}>
            ‹ {t("backToWorkspaces")}
          </Text>
        </Pressable>
      </View>

      {dialog && actionSession ? (
        <NativeSheet
          onOpenChange={(open) => {
            if (!open && !actionPending) setDialog(null);
          }}
          open
        >
          <View style={styles.actionSheet}>
            <Text numberOfLines={2} style={styles.actionTitle}>
              {actionSession.title || t("untitledSession")}
            </Text>
            {dialog.kind === "actions" ? (
              <>
                <ActionButton
                  disabled={actionPending}
                  label={
                    actionSession.pinnedAtUnixMs
                      ? t("unpinSession")
                      : t("pinSession")
                  }
                  onPress={() =>
                    void runAction(() => onTogglePinned(actionSession.id))
                  }
                />
                <ActionButton
                  disabled={actionPending}
                  label={t("renameSession")}
                  onPress={() => {
                    setRenameDraft(actionSession.title);
                    setDialog({
                      kind: "rename",
                      sessionId: actionSession.id
                    });
                  }}
                />
                <ActionButton
                  danger
                  disabled={actionPending}
                  label={t("deleteSession")}
                  onPress={() =>
                    setDialog({
                      kind: "delete",
                      sessionId: actionSession.id
                    })
                  }
                />
              </>
            ) : dialog.kind === "rename" ? (
              <>
                <TextInput
                  autoFocus
                  editable={!actionPending}
                  onChangeText={setRenameDraft}
                  placeholder={t("untitledSession")}
                  placeholderTextColor={theme.color.muted}
                  selectTextOnFocus
                  style={styles.renameInput}
                  value={renameDraft}
                />
                <View style={styles.actionRow}>
                  <ActionButton
                    compact
                    disabled={actionPending}
                    label={t("cancel")}
                    onPress={() => setDialog(null)}
                  />
                  <ActionButton
                    compact
                    disabled={actionPending || !renameDraft.trim()}
                    label={t("save")}
                    onPress={() =>
                      void runAction(() =>
                        onRenameSession(actionSession.id, renameDraft)
                      )
                    }
                  />
                </View>
              </>
            ) : (
              <>
                <Text style={styles.deleteDescription}>
                  {t("deleteSessionDescription")}
                </Text>
                <View style={styles.actionRow}>
                  <ActionButton
                    compact
                    disabled={actionPending}
                    label={t("cancel")}
                    onPress={() => setDialog(null)}
                  />
                  <ActionButton
                    compact
                    danger
                    disabled={actionPending}
                    label={t("deleteSessionConfirm")}
                    onPress={() =>
                      void runAction(() => onDeleteSession(actionSession.id))
                    }
                  />
                </View>
              </>
            )}
            {actionPending ? (
              <ActivityIndicator
                color={theme.color.accent}
                size="small"
                style={styles.actionPending}
              />
            ) : null}
          </View>
        </NativeSheet>
      ) : null}
    </View>
  );
}

function ActionButton({
  compact = false,
  danger = false,
  disabled = false,
  label,
  onPress
}: {
  compact?: boolean;
  danger?: boolean;
  disabled?: boolean;
  label: string;
  onPress(): void;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  return (
    <NativeButton
      disabled={disabled}
      label={label}
      onPress={onPress}
      size={compact ? "compact" : "regular"}
      style={compact ? styles.actionButtonCompact : undefined}
      variant={danger ? "destructiveGhost" : "ghost"}
    />
  );
}

function sectionTitle(
  section: WorkspaceActivitySnapshot["railSections"][number]
): string {
  if (section.kind === "pinned") return t("pinned");
  if (section.kind === "project") {
    const label = section.label || t("projects");
    return section.pinnedProject ? `${label} · ${t("pinned")}` : label;
  }
  return t("recentSessions");
}

function sessionStatusLabel(
  status: WorkspaceActivitySnapshot["railSections"][number]["items"][number]["status"]
): string {
  switch (status) {
    case "working":
      return t("running");
    case "waiting":
      return t("waiting");
    case "completed":
      return t("completed");
    case "failed":
      return t("failed");
    case "canceled":
      return t("canceled");
    default:
      return t("ready");
  }
}

function statusDotStyle(
  theme: NativeTheme,
  status: WorkspaceActivitySnapshot["railSections"][number]["items"][number]["status"]
): { backgroundColor: string } {
  switch (status) {
    case "working":
      return { backgroundColor: theme.color.accent };
    case "waiting":
    case "failed":
      return { backgroundColor: theme.color.danger };
    case "completed":
      return { backgroundColor: theme.color.success };
    default:
      return { backgroundColor: theme.color.muted };
  }
}

function formatSessionTime(unixMs: number): string {
  if (!unixMs) return "";
  const date = new Date(unixMs);
  const today = new Date();
  return date.toDateString() === today.toDateString()
    ? date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    : date.toLocaleDateString([], { day: "numeric", month: "short" });
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    actionButtonCompact: { flex: 1 },
    actionPending: { marginTop: theme.space.small },
    actionRow: {
      flexDirection: "row",
      gap: theme.space.small,
      marginTop: theme.space.small
    },
    actionSheet: {
      paddingBottom: theme.space.large,
      paddingHorizontal: theme.space.large,
      paddingTop: theme.space.medium
    },
    actionTitle: {
      color: theme.color.textSecondary,
      fontSize: 13,
      fontWeight: "600",
      lineHeight: 18,
      marginBottom: theme.space.small
    },
    agentIcon: { borderRadius: 8, height: 28, width: 28 },
    agentIconFallback: {
      color: theme.color.text,
      fontSize: 12,
      fontWeight: "800"
    },
    agentIconFrame: {
      alignItems: "center",
      backgroundColor: theme.color.panel,
      borderColor: theme.color.border,
      borderRadius: 9,
      borderWidth: StyleSheet.hairlineWidth,
      height: 30,
      justifyContent: "center",
      marginRight: theme.space.small,
      overflow: "visible",
      width: 30
    },
    attention: {
      color: theme.color.danger,
      fontSize: 10,
      fontWeight: "700"
    },
    close: { color: theme.color.textSecondary, fontSize: 32, lineHeight: 34 },
    closeButton: {
      alignItems: "center",
      height: 44,
      justifyContent: "center",
      width: 44
    },
    deleteDescription: {
      color: theme.color.textSecondary,
      fontSize: 14,
      lineHeight: 20
    },
    disabled: { opacity: 0.45 },
    drawer: {
      backgroundColor: theme.color.background,
      bottom: 0,
      left: 0,
      maxWidth: 420,
      paddingHorizontal: theme.space.medium,
      paddingTop: theme.space.large,
      position: "absolute",
      top: 0,
      width: "92%"
    },
    empty: {
      color: theme.color.muted,
      lineHeight: 22,
      paddingHorizontal: theme.space.small,
      paddingVertical: theme.space.xlarge,
      textAlign: "center"
    },
    header: {
      alignItems: "center",
      flexDirection: "row",
      justifyContent: "space-between"
    },
    headerActions: {
      alignItems: "center",
      flexDirection: "row",
      gap: theme.space.small
    },
    heading: { flex: 1 },
    feedback: {
      alignItems: "center",
      gap: theme.space.small,
      paddingVertical: theme.space.xlarge
    },
    feedbackText: { color: theme.color.textSecondary, fontSize: 14 },
    inlineError: {
      alignItems: "center",
      backgroundColor: theme.color.panel,
      borderColor: theme.color.border,
      borderRadius: theme.radius.medium,
      borderWidth: StyleSheet.hairlineWidth,
      flexDirection: "row",
      justifyContent: "space-between",
      paddingLeft: theme.space.medium
    },
    inlineErrorText: { color: theme.color.danger, flex: 1, fontSize: 12 },
    layer: {
      bottom: 0,
      left: 0,
      position: "absolute",
      right: 0,
      top: 0
    },
    list: {
      gap: theme.space.large,
      paddingBottom: theme.space.large,
      paddingTop: theme.space.medium
    },
    loadMoreButton: {
      alignItems: "center",
      minHeight: 40,
      paddingVertical: theme.space.small
    },
    loadMoreLabel: {
      color: theme.color.accent,
      fontSize: 13,
      fontWeight: "600"
    },
    moreButton: {
      alignItems: "center",
      height: 44,
      justifyContent: "center",
      width: 40
    },
    moreIcon: {
      color: theme.color.muted,
      fontSize: 13,
      fontWeight: "900",
      letterSpacing: 1
    },
    newSessionButton: {
      marginTop: theme.space.medium
    },
    newSessionIcon: { color: theme.color.accent, fontSize: 22 },
    pressed: { opacity: 0.7 },
    refresh: { color: theme.color.accent, fontSize: 24, lineHeight: 28 },
    renameInput: {
      backgroundColor: theme.color.panel,
      borderColor: theme.color.border,
      borderRadius: theme.radius.medium,
      borderWidth: StyleSheet.hairlineWidth,
      color: theme.color.text,
      fontSize: 16,
      minHeight: 48,
      paddingHorizontal: theme.space.medium
    },
    scrim: {
      backgroundColor: theme.color.scrim,
      bottom: 0,
      left: 0,
      position: "absolute",
      right: 0,
      top: 0
    },
    section: { gap: 2 },
    sectionCount: {
      color: theme.color.muted,
      fontSize: 11,
      fontWeight: "600"
    },
    sectionChevron: {
      color: theme.color.muted,
      fontSize: 18,
      lineHeight: 20
    },
    sectionHeader: {
      alignItems: "center",
      flexDirection: "row",
      gap: theme.space.small,
      minHeight: 28,
      paddingHorizontal: theme.space.small
    },
    sectionTitle: {
      color: theme.color.textSecondary,
      flex: 1,
      fontSize: 12,
      fontWeight: "700",
      letterSpacing: 0.2
    },
    sessionMeta: {
      alignItems: "center",
      flexDirection: "row",
      gap: theme.space.small,
      marginTop: 4
    },
    sessionTime: { color: theme.color.muted, fontSize: 10 },
    sessionStatus: { color: theme.color.textSecondary, fontSize: 10 },
    statusDot: {
      borderColor: theme.color.background,
      borderRadius: 4,
      borderWidth: 2,
      bottom: -2,
      height: 9,
      position: "absolute",
      right: -2,
      width: 9
    },
    title: { color: theme.color.text, fontSize: 24, fontWeight: "700" },
    workspace: {
      color: theme.color.muted,
      fontSize: 12,
      fontWeight: "600",
      marginBottom: 3
    },
    workspaceBackButton: {
      alignItems: "flex-start",
      borderTopColor: theme.color.border,
      borderTopWidth: StyleSheet.hairlineWidth,
      minHeight: 54,
      paddingHorizontal: theme.space.small,
      paddingVertical: theme.space.medium
    },
    workspaceBackLabel: {
      color: theme.color.textSecondary,
      fontSize: 14,
      fontWeight: "600"
    }
  });
}
