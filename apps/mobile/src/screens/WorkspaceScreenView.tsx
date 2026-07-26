import type {
  AgentActivityInteraction,
  AgentActivitySessionSettings
} from "@tutti-os/agent-activity-core";
import { resolveAgentConversationNavigationAction } from "@tutti-os/agent-gui/conversation-projection";
import type { WorkspaceSummary } from "@tutti-os/client-tuttid-ts";
import {
  NativeButton,
  NativeIconButton,
  type NativeTheme,
  useNativeTheme
} from "@tutti-os/ui-system/native";
import { useEffect, useRef, useState } from "react";
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Linking,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View
} from "react-native";
import { MobileInteractionCard } from "../components/MobileConversationRows";
import { MobileConversationDrawer } from "../components/MobileConversationDrawer";
import { MobileConversationTimeline } from "../components/MobileConversationTimeline";
import { MobileComposerSettingsSheet } from "../components/MobileComposerSettingsSheet";
import { PrimaryButton } from "../components/PrimaryButton";
import { t } from "../i18n";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";
import type { WorkspaceCatalogSnapshot } from "../services/workspaceCatalogService";
import type { WorkspaceMediaSnapshot } from "../services/workspaceMediaService";

export function WorkspacePickerView({
  deviceName,
  model,
  onDisconnect,
  onRetry,
  onSelect
}: {
  deviceName: string;
  model: WorkspaceCatalogSnapshot;
  onDisconnect(): void;
  onRetry(): void;
  onSelect(workspace: WorkspaceSummary): void;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  return (
    <View style={styles.root}>
      <View style={styles.pageHeader}>
        <View>
          <Text style={styles.eyebrow}>{deviceName}</Text>
          <Text style={styles.pageTitle}>{t("sessions")}</Text>
        </View>
        <PrimaryButton
          label={t("cancel")}
          onPress={onDisconnect}
          secondary
          size="compact"
        />
      </View>
      {model.status !== "ready" ? (
        <View style={styles.center}>
          <ActivityIndicator color={theme.color.accent} size="large" />
        </View>
      ) : model.errorCode ? (
        <View style={styles.center}>
          <Text style={styles.error}>{t("genericError")}</Text>
          <PrimaryButton label={t("retry")} onPress={onRetry} />
        </View>
      ) : model.workspaces.length === 0 ? (
        <View style={styles.center}>
          <Text style={styles.emptyText}>{t("noWorkspace")}</Text>
        </View>
      ) : (
        <ScrollView contentContainerStyle={styles.workspaceList}>
          {model.workspaces.map((workspace) => (
            <Pressable
              key={workspace.id}
              onPress={() => onSelect(workspace)}
              style={({ pressed }) => [
                styles.workspaceCard,
                pressed && styles.pressed
              ]}
            >
              <Text style={styles.workspaceName}>{workspace.name}</Text>
              <Text style={styles.chevron}>›</Text>
            </Pressable>
          ))}
        </ScrollView>
      )}
    </View>
  );
}

export function ConversationWorkspaceView({
  deviceName,
  media,
  model,
  onBack,
  onDeleteSession,
  onDraftChange,
  onLoadOlder,
  onLoadMoreSessions,
  onNewSession,
  onRenameSession,
  onRefreshSessions,
  onRespond,
  onSelectSession,
  onSelectTarget,
  onSend,
  onStop,
  onTogglePinned,
  onUpdateComposerSettings,
  workspace
}: {
  deviceName: string;
  model: WorkspaceActivitySnapshot;
  media: WorkspaceMediaSnapshot;
  onBack(): void;
  onDeleteSession(id: string): Promise<void>;
  onDraftChange(value: string): void;
  onLoadOlder(): void;
  onLoadMoreSessions(sectionId: string): void;
  onNewSession(): void;
  onRenameSession(id: string, title: string): Promise<void>;
  onRefreshSessions(): Promise<void>;
  onRespond(
    interaction: AgentActivityInteraction,
    input: {
      action?: string;
      optionId?: string;
      payload?: Readonly<Record<string, unknown>>;
    }
  ): void;
  onSelectSession(id: string): void;
  onSelectTarget(id: string): void;
  onSend(): void;
  onStop(): void;
  onTogglePinned(id: string): Promise<void>;
  onUpdateComposerSettings(settings: AgentActivitySessionSettings): void;
  workspace: WorkspaceSummary;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);
  const scroll = useRef<ScrollView>(null);
  const shouldStickToBottom = useRef(true);
  const messages = model.selectedAgentSessionId
    ? (model.activity.sessionMessagesById[model.selectedAgentSessionId] ?? [])
    : [];
  const window = model.selectedAgentSessionId
    ? model.activity.sessionMessageWindowsById?.[model.selectedAgentSessionId]
    : null;

  useEffect(() => {
    shouldStickToBottom.current = true;
    setShowScrollToBottom(false);
    const frame = requestAnimationFrame(() => {
      scroll.current?.scrollToEnd({ animated: false });
    });
    return () => cancelAnimationFrame(frame);
  }, [model.selectedAgentSessionId]);

  const scrollToBottom = (animated: boolean) => {
    shouldStickToBottom.current = true;
    setShowScrollToBottom(false);
    scroll.current?.scrollToEnd({ animated });
  };
  const openConversationLink = (href: string): boolean => {
    if (!model.conversation) return false;
    const action = resolveAgentConversationNavigationAction({
      href,
      source: "agent-markdown"
    });
    if (!action) return false;
    if (action.type === "open-url") {
      void Linking.openURL(action.url).catch(() => undefined);
      return true;
    }
    if (
      action.type === "open-agent-session" &&
      action.workspaceId === workspace.id
    ) {
      onSelectSession(action.agentSessionId);
      return true;
    }
    return true;
  };

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      style={styles.root}
    >
      <View style={styles.conversationHeader}>
        <NativeIconButton
          accessibilityLabel={t("sessions")}
          onPress={() => setDrawerOpen(true)}
          icon={<Text style={styles.iconText}>☰</Text>}
          style={styles.iconButton}
        />
        <View style={styles.conversationTitle}>
          <Text numberOfLines={1} style={styles.sessionTitle}>
            {model.selectedSession?.title || workspace.name}
          </Text>
          <Text numberOfLines={1} style={styles.deviceCaption}>
            {deviceName || t("desktopFallback")} · {workspace.name}
          </Text>
        </View>
        <View style={styles.onlineDot} />
      </View>

      {model.loading ? (
        <View style={styles.center}>
          <ActivityIndicator color={theme.color.accent} size="large" />
        </View>
      ) : model.selectedSession && !model.creating ? (
        <View style={styles.conversationBody}>
          <ScrollView
            contentContainerStyle={styles.messageList}
            keyboardDismissMode="interactive"
            keyboardShouldPersistTaps="handled"
            maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
            onContentSizeChange={() => {
              if (shouldStickToBottom.current) {
                scrollToBottom(false);
              }
            }}
            onLayout={() => {
              if (shouldStickToBottom.current) {
                scrollToBottom(false);
              }
            }}
            onScrollBeginDrag={() => {
              shouldStickToBottom.current = false;
            }}
            onScroll={({ nativeEvent }) => {
              if (
                nativeEvent.contentOffset.y < 48 &&
                window?.hasOlderMessages
              ) {
                onLoadOlder();
              }
              const distanceFromBottom =
                nativeEvent.contentSize.height -
                nativeEvent.layoutMeasurement.height -
                nativeEvent.contentOffset.y;
              const nearBottom = distanceFromBottom <= 72;
              shouldStickToBottom.current = nearBottom;
              setShowScrollToBottom(!nearBottom);
            }}
            ref={scroll}
            scrollEventThrottle={16}
            style={styles.messageScroller}
          >
            {window?.hasOlderMessages ? (
              <Text style={styles.loadOlder}>{t("loading")}</Text>
            ) : null}
            {messages.length === 0 || !model.conversation ? (
              <Text style={styles.emptyText}>{t("emptyConversation")}</Text>
            ) : (
              <MobileConversationTimeline
                conversation={model.conversation}
                media={media}
                onLinkPress={openConversationLink}
              />
            )}
            {model.selectedSession.pendingInteractions.map((interaction) => (
              <MobileInteractionCard
                interaction={interaction}
                key={`${interaction.agentSessionId}:${interaction.turnId}:${interaction.requestId}`}
                onSubmit={async (input) => onRespond(interaction, input)}
              />
            ))}
          </ScrollView>
          {showScrollToBottom ? (
            <NativeButton
              label={t("scrollToBottom")}
              onPress={() => scrollToBottom(true)}
              size="compact"
              style={styles.scrollToBottom}
              variant="secondary"
            />
          ) : null}
        </View>
      ) : model.creating ? (
        <View style={styles.center}>
          <Text style={styles.emptyText}>{t("newSessionHint")}</Text>
          <ScrollView
            contentContainerStyle={styles.targetList}
            horizontal
            showsHorizontalScrollIndicator={false}
          >
            {model.targets.map((target) => (
              <Pressable
                key={target.id}
                onPress={() => onSelectTarget(target.id)}
                style={[
                  styles.targetChip,
                  target.id === model.selectedAgentTargetId &&
                    styles.targetChipSelected
                ]}
              >
                <Text style={styles.targetChipText}>{target.name}</Text>
              </Pressable>
            ))}
          </ScrollView>
        </View>
      ) : (
        <View style={styles.center}>
          <Text style={styles.emptyText}>{t("emptySessions")}</Text>
        </View>
      )}

      {model.errorCode ? (
        <Text style={styles.inlineError}>{t("genericError")}</Text>
      ) : null}
      {model.selectedSession || model.creating ? (
        <View style={styles.composer}>
          <MobileComposerSettingsSheet
            model={model}
            onUpdate={onUpdateComposerSettings}
          />
          <TextInput
            editable={!model.sending}
            multiline
            onChangeText={onDraftChange}
            placeholder={t("messageHint")}
            placeholderTextColor={theme.color.muted}
            style={styles.input}
            value={model.draft}
          />
          {model.selectedSession?.activeTurnId && !model.creating ? (
            <PrimaryButton
              label={t("stop")}
              onPress={onStop}
              secondary
              style={styles.sendButton}
            />
          ) : (
            <PrimaryButton
              disabled={
                !model.draft.trim() ||
                (model.creating && !model.selectedAgentTargetId)
              }
              label={model.ambiguousSubmission ? t("retry") : t("send")}
              loading={model.sending}
              onPress={onSend}
              style={styles.sendButton}
            />
          )}
        </View>
      ) : null}

      {drawerOpen ? (
        <MobileConversationDrawer
          model={model}
          onBack={onBack}
          onClose={() => setDrawerOpen(false)}
          onDeleteSession={onDeleteSession}
          onLoadMoreSessions={onLoadMoreSessions}
          onNewSession={onNewSession}
          onRenameSession={onRenameSession}
          onRefreshSessions={onRefreshSessions}
          onSelectSession={onSelectSession}
          onTogglePinned={onTogglePinned}
          workspaceName={workspace.name}
        />
      ) : null}
    </KeyboardAvoidingView>
  );
}

function createStyles(theme: NativeTheme) {
  return StyleSheet.create({
    center: {
      alignItems: "center",
      flex: 1,
      gap: theme.space.medium,
      justifyContent: "center",
      padding: theme.space.large
    },
    chevron: { color: theme.color.muted, fontSize: 30 },
    composer: {
      alignItems: "flex-end",
      borderTopColor: theme.color.border,
      borderTopWidth: StyleSheet.hairlineWidth,
      flexDirection: "row",
      flexWrap: "wrap",
      gap: theme.space.small,
      padding: theme.space.medium
    },
    conversationHeader: {
      alignItems: "center",
      borderBottomColor: theme.color.border,
      borderBottomWidth: StyleSheet.hairlineWidth,
      flexDirection: "row",
      minHeight: 64,
      paddingHorizontal: theme.space.medium
    },
    conversationBody: { flex: 1, position: "relative" },
    conversationTitle: { flex: 1, marginHorizontal: theme.space.small },
    deviceCaption: { color: theme.color.muted, fontSize: 12, marginTop: 3 },
    emptyText: {
      color: theme.color.textSecondary,
      fontSize: 15,
      lineHeight: 22,
      textAlign: "center"
    },
    error: { color: theme.color.danger, fontSize: 14 },
    eyebrow: { color: theme.color.accent, fontSize: 12, fontWeight: "700" },
    iconButton: {
      alignItems: "center",
      height: 44,
      justifyContent: "center",
      width: 44
    },
    iconText: { color: theme.color.text, fontSize: 22 },
    inlineError: {
      backgroundColor: theme.color.panel,
      color: theme.color.danger,
      fontSize: 12,
      padding: theme.space.small,
      textAlign: "center"
    },
    input: {
      backgroundColor: theme.color.panel,
      borderColor: theme.color.border,
      borderRadius: theme.radius.large,
      borderWidth: StyleSheet.hairlineWidth,
      color: theme.color.text,
      flex: 1,
      fontSize: 16,
      maxHeight: 132,
      minHeight: 48,
      paddingHorizontal: theme.space.medium,
      paddingVertical: 12
    },
    loadOlder: { color: theme.color.muted, fontSize: 12, textAlign: "center" },
    messageList: { gap: theme.space.medium, padding: theme.space.large },
    messageScroller: { flex: 1 },
    onlineDot: {
      backgroundColor: theme.color.success,
      borderRadius: 5,
      height: 10,
      width: 10
    },
    pageHeader: {
      alignItems: "center",
      borderBottomColor: theme.color.border,
      borderBottomWidth: StyleSheet.hairlineWidth,
      flexDirection: "row",
      justifyContent: "space-between",
      padding: theme.space.large
    },
    pageTitle: {
      color: theme.color.text,
      fontSize: 27,
      fontWeight: "700",
      marginTop: 4
    },
    pressed: { opacity: 0.7 },
    root: { backgroundColor: theme.color.background, flex: 1 },
    scrollToBottom: {
      bottom: theme.space.medium,
      position: "absolute",
      right: theme.space.medium
    },
    sendButton: { minWidth: 76 },
    sessionTitle: { color: theme.color.text, fontSize: 16, fontWeight: "700" },
    targetChip: {
      borderColor: theme.color.border,
      borderRadius: theme.radius.large,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: theme.space.medium,
      paddingVertical: theme.space.small
    },
    targetChipSelected: { borderColor: theme.color.accent },
    targetChipText: { color: theme.color.text, fontSize: 13 },
    targetList: { gap: theme.space.small },
    workspaceCard: {
      alignItems: "center",
      backgroundColor: theme.color.panel,
      borderColor: theme.color.border,
      borderRadius: theme.radius.large,
      borderWidth: StyleSheet.hairlineWidth,
      flexDirection: "row",
      justifyContent: "space-between",
      padding: theme.space.large
    },
    workspaceList: {
      gap: theme.space.medium,
      padding: theme.space.large
    },
    workspaceName: {
      color: theme.color.text,
      flex: 1,
      fontSize: 17,
      fontWeight: "700"
    }
  });
}
