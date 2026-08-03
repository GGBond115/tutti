import {
  canonicalInteractionKey,
  type AgentActivityInteraction,
  type AgentActivitySessionSettings
} from "@tutti-os/agent-activity-core";
import { resolveAgentConversationNavigationAction } from "@tutti-os/agent-gui/conversation-projection";
import { createAgentConversationFollowEndController } from "@tutti-os/agent-gui/agent-conversation/follow-end";
import {
  NativeButton,
  NativeIconButton,
  type NativeTheme,
  useNativeTheme
} from "@tutti-os/ui-system/native";
import { useEffect, useRef, useState } from "react";
import { Linking, ScrollView, StyleSheet, Text, View } from "react-native";
import { MobileInteractionCard } from "../components/MobileConversationRows";
import { MobileComposerDock } from "../components/MobileComposerDock";
import { MobileConversationTimeline } from "../components/MobileConversationTimeline";
import {
  MobileKeyboardAvoidingView,
  mobileKeyboardDismissMode
} from "../components/MobileKeyboardAvoidingView";
import {
  MobileComputerGlyph,
  MobileFolderGlyph
} from "../components/MobileLocationGlyphs";
import { t } from "../i18n";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";
import type { WorkspaceMediaSnapshot } from "../services/workspaceMediaService";
import type { MobileQuickPromptLibrarySnapshot } from "../services/mobileQuickPromptLibraryService";

export function ConversationScreenView({
  deviceName,
  media,
  model,
  onBack,
  onDraftChange,
  onLoadOlder,
  onRefreshQuickPrompts,
  onRespond,
  onOpenSession,
  onSelectProject,
  onSelectTarget,
  onSend,
  onStop,
  onUpdateComposerSettings,
  quickPromptLibrary,
  workspaceId,
  workspaceName
}: {
  deviceName: string;
  model: WorkspaceActivitySnapshot;
  media: WorkspaceMediaSnapshot;
  onBack(): void;
  onDraftChange(value: string): void;
  onLoadOlder(): void;
  onRefreshQuickPrompts(): Promise<void>;
  onRespond(
    interaction: AgentActivityInteraction,
    input: {
      action?: string;
      optionId?: string;
      payload?: Readonly<Record<string, unknown>>;
    }
  ): void;
  onOpenSession(id: string): void;
  onSelectProject(path: string | null): void;
  onSelectTarget(id: string): void;
  onSend(): void;
  onStop(): void;
  onUpdateComposerSettings(settings: AgentActivitySessionSettings): void;
  quickPromptLibrary: MobileQuickPromptLibrarySnapshot;
  workspaceId: string;
  workspaceName: string;
}) {
  const theme = useNativeTheme();
  const styles = createStyles(theme);
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);
  const scroll = useRef<ScrollView>(null);
  const followEndControllerRef = useRef(
    createAgentConversationFollowEndController()
  );
  const followEndController = followEndControllerRef.current;
  const lastScrollOffsetY = useRef(0);
  const window = model.selectedAgentSessionId
    ? model.activity.sessionMessageWindowsById?.[model.selectedAgentSessionId]
    : null;

  useEffect(() => {
    followEndController.dispatch("conversation-changed");
    lastScrollOffsetY.current = 0;
    setShowScrollToBottom(false);
    const frame = requestAnimationFrame(() => {
      scroll.current?.scrollToEnd({ animated: false });
    });
    return () => cancelAnimationFrame(frame);
  }, [followEndController, model.selectedAgentSessionId]);

  const scrollToBottom = (animated: boolean) => {
    followEndController.dispatch("scroll-to-end-requested");
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
      action.workspaceId === workspaceId
    ) {
      onOpenSession(action.agentSessionId);
      return true;
    }
    return true;
  };

  return (
    <MobileKeyboardAvoidingView style={styles.root}>
      <View style={styles.conversationHeader}>
        <View style={styles.headerButtonSlot}>
          <NativeIconButton
            accessibilityLabel={t("sessions")}
            onPress={onBack}
            icon={<Text style={styles.backIcon}>←</Text>}
            style={styles.headerCircleButton}
            variant="secondary"
          />
        </View>
        <View style={styles.conversationTitle}>
          <Text numberOfLines={1} style={styles.sessionTitle}>
            {model.creating
              ? t("newSession")
              : model.selectedSession?.title || workspaceName}
          </Text>
          <View style={styles.locationRow}>
            <View style={styles.locationItem}>
              <MobileFolderGlyph color={theme.color.textSecondary} size={14} />
              <Text numberOfLines={1} style={styles.locationLabel}>
                {workspaceName}
              </Text>
            </View>
            <View style={styles.locationItem}>
              <MobileComputerGlyph
                color={theme.color.textSecondary}
                size={14}
              />
              <Text numberOfLines={1} style={styles.locationLabel}>
                {deviceName || t("desktopFallback")}
              </Text>
            </View>
          </View>
        </View>
        <View style={styles.headerButtonSlot} />
      </View>

      {model.loading ? (
        <View
          accessibilityLabel={t("loading")}
          accessibilityLiveRegion="polite"
          style={styles.loadingSkeleton}
        >
          <View style={[styles.skeletonBlock, styles.skeletonShort]} />
          <View style={[styles.skeletonBlock, styles.skeletonLong]} />
        </View>
      ) : model.selectedSession && !model.creating ? (
        <View style={styles.conversationBody}>
          <ScrollView
            contentContainerStyle={styles.messageList}
            keyboardDismissMode={mobileKeyboardDismissMode}
            keyboardShouldPersistTaps="handled"
            maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
            onContentSizeChange={() => {
              if (followEndController.getSnapshot() === "following") {
                scrollToBottom(false);
              }
            }}
            onLayout={() => {
              if (followEndController.getSnapshot() === "following") {
                scrollToBottom(false);
              }
            }}
            onScrollBeginDrag={() => {
              followEndController.dispatch("user-scrolled-away");
              setShowScrollToBottom(true);
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
              const scrollingTowardEnd =
                nativeEvent.contentOffset.y > lastScrollOffsetY.current;
              lastScrollOffsetY.current = nativeEvent.contentOffset.y;
              if (
                followEndController.getSnapshot() === "detached" &&
                scrollingTowardEnd &&
                distanceFromBottom <= 1
              ) {
                followEndController.dispatch("user-reached-end");
              }
              setShowScrollToBottom(
                followEndController.getSnapshot() === "detached"
              );
            }}
            ref={scroll}
            scrollEventThrottle={16}
            style={styles.messageScroller}
          >
            {window?.hasOlderMessages ? (
              <Text style={styles.loadOlder}>{t("loading")}</Text>
            ) : null}
            {!model.conversation || model.conversation.rows.length === 0 ? (
              <Text style={styles.emptyText}>{t("emptyConversation")}</Text>
            ) : (
              <MobileConversationTimeline
                conversation={model.conversation}
                media={media}
                onLinkPress={openConversationLink}
              />
            )}
            {model.pendingInteractions.map((interaction) => {
              const interactionKey = canonicalInteractionKey(
                interaction.agentSessionId,
                interaction.turnId,
                interaction.requestId
              );
              const state = model.interactionStates[interactionKey] ?? {
                failed: false,
                runtimeAvailable: false,
                submitting: false
              };
              return (
                <MobileInteractionCard
                  failed={state.failed}
                  interaction={interaction}
                  key={interactionKey}
                  onSubmit={(input) => onRespond(interaction, input)}
                  runtimeAvailable={state.runtimeAvailable}
                  submitting={state.submitting}
                />
              );
            })}
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
        <MobileComposerDock
          model={model}
          quickPromptLibrary={quickPromptLibrary}
          onDraftChange={onDraftChange}
          onRefreshQuickPrompts={onRefreshQuickPrompts}
          onSelectProject={onSelectProject}
          onSelectTarget={onSelectTarget}
          onSend={onSend}
          onStop={onStop}
          onUpdate={onUpdateComposerSettings}
        />
      ) : null}
    </MobileKeyboardAvoidingView>
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
    conversationHeader: {
      alignItems: "center",
      flexDirection: "row",
      gap: theme.space.small,
      paddingHorizontal: theme.space.medium,
      paddingVertical: theme.space.small
    },
    conversationBody: { flex: 1, minHeight: 0, position: "relative" },
    conversationTitle: {
      alignItems: "center",
      flex: 1,
      justifyContent: "center",
      minWidth: 0
    },
    emptyText: {
      color: theme.color.textSecondary,
      fontSize: 15,
      lineHeight: 22,
      textAlign: "center"
    },
    backIcon: {
      color: theme.color.text,
      fontSize: 22,
      fontWeight: "400",
      lineHeight: 26
    },
    headerCircleButton: {
      alignItems: "center",
      backgroundColor: theme.color.panelRaised,
      borderColor: theme.color.border,
      borderRadius: 20,
      borderWidth: StyleSheet.hairlineWidth,
      flexShrink: 0,
      height: 40,
      justifyContent: "center",
      width: 40
    },
    headerButtonSlot: {
      flexShrink: 0,
      height: 40,
      width: 40
    },
    inlineError: {
      backgroundColor: theme.color.panel,
      color: theme.color.danger,
      fontSize: 12,
      padding: theme.space.small,
      textAlign: "center"
    },
    loadOlder: { color: theme.color.muted, fontSize: 12, textAlign: "center" },
    loadingSkeleton: {
      flex: 1,
      gap: theme.space.medium,
      paddingHorizontal: theme.space.large,
      paddingTop: theme.space.xlarge
    },
    locationLabel: {
      color: theme.color.textSecondary,
      flexShrink: 1,
      fontSize: 12
    },
    locationItem: {
      alignItems: "center",
      flexDirection: "row",
      flexShrink: 1,
      gap: 4,
      minWidth: 0
    },
    locationRow: {
      flexDirection: "row",
      gap: theme.space.small,
      justifyContent: "center",
      marginTop: 3,
      overflow: "hidden"
    },
    messageList: {
      gap: theme.space.medium,
      paddingBottom: theme.space.xlarge,
      paddingHorizontal: theme.space.medium,
      paddingTop: theme.space.medium
    },
    messageScroller: { flex: 1 },
    pressed: { opacity: 0.7 },
    root: { backgroundColor: theme.color.background, flex: 1 },
    scrollToBottom: {
      bottom: theme.space.medium,
      position: "absolute",
      right: theme.space.medium
    },
    skeletonBlock: {
      backgroundColor: theme.color.panel,
      borderRadius: theme.radius.large
    },
    skeletonLong: { height: 96 },
    skeletonShort: {
      alignSelf: "flex-end",
      height: 64,
      width: "78%"
    },
    sessionTitle: {
      color: theme.color.text,
      fontSize: 16,
      fontWeight: "700",
      textAlign: "center"
    }
  });
}
