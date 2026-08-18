package conformance

import (
	"context"
	"fmt"
	"slices"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

func WorkspaceRuntimeDisconnectScenarios() []WorkspaceRuntimeDisconnectScenario {
	return []WorkspaceRuntimeDisconnectScenario{{
		Name: "disconnect workspace runtime without losing resumable sessions",
		run:  runDisconnectWorkspaceRuntime,
	}}
}

func WorkspaceRuntimeDisconnectCleanupScenarios() []WorkspaceRuntimeDisconnectCleanupScenario {
	return []WorkspaceRuntimeDisconnectCleanupScenario{{
		Name: "disconnect workspace runtime cleans preparation owners and preserves provider state",
		run:  runDisconnectWorkspaceRuntimeCleansPreparationOwners,
	}}
}

func runDisconnectWorkspaceRuntimeCleansPreparationOwners(
	ctx context.Context,
	driver WorkspaceRuntimeDisconnectCleanupDriver,
) error {
	const (
		workspaceID      = "workspace-1"
		failedSessionID  = "session-cleanup-failed"
		successSessionID = "session-cleanup-success"
	)
	if err := driver.Reset(ctx, Fixture{
		RuntimePreparationCleanupFailureSessionID: failedSessionID,
	}); err != nil {
		return err
	}
	for _, sessionID := range []string{failedSessionID, successSessionID} {
		if _, _, err := driver.Create(ctx, workspaceID, agenthost.CreateSessionInput{
			AgentSessionID: sessionID,
			AgentTargetID:  "target-1",
			Provider:       "codex",
		}); err != nil {
			return fmt.Errorf("create session %q: %w", sessionID, err)
		}
	}

	result, err := driver.DisconnectWorkspaceRuntime(ctx, workspaceID)
	if result.Scanned != 2 || result.Disconnected != 2 || result.Failed != 0 {
		return fmt.Errorf("disconnect result=%#v", result)
	}
	if err == nil {
		return fmt.Errorf("disconnect cleanup error=nil, want preparation cleanup failure to be aggregated")
	}
	if got, want := driver.RuntimePreparationCleanupSessionIDs(), []string{failedSessionID, successSessionID}; !slices.Equal(got, want) {
		return fmt.Errorf("preparation cleanup session IDs=%#v, want exactly once for each disconnected session=%#v", got, want)
	}
	for _, sessionID := range []string{failedSessionID, successSessionID} {
		canonical, getErr := driver.GetCanonicalSession(ctx, agenthost.SessionRef{
			WorkspaceID: workspaceID, AgentSessionID: sessionID,
		})
		if getErr != nil {
			return fmt.Errorf("get canonical session %q: %w", sessionID, getErr)
		}
		if canonical.ProviderSessionID == "" {
			return fmt.Errorf("canonical provider state lost for %q: %#v", sessionID, canonical)
		}
	}
	return nil
}

func runDisconnectWorkspaceRuntime(ctx context.Context, driver WorkspaceRuntimeDisconnectDriver) error {
	fixture := liveSessionFixture("session-disconnect-a", "")
	fixture.AdditionalSessions = []SessionSeed{
		{
			WorkspaceID: "workspace-1", AgentSessionID: "session-disconnect-b", Provider: "codex",
			ProviderSessionID: "provider-session-disconnect-b", Cwd: "/workspace", Live: true,
		},
		{
			WorkspaceID: "workspace-2", AgentSessionID: "session-other-workspace", Provider: "codex",
			ProviderSessionID: "provider-session-other-workspace", Cwd: "/workspace", Live: true,
		},
	}
	if err := driver.Reset(ctx, fixture); err != nil {
		return err
	}

	result, err := driver.DisconnectWorkspaceRuntime(ctx, "workspace-1")
	if err != nil {
		return fmt.Errorf("disconnect workspace runtime: %w", err)
	}
	if result.Scanned != 2 || result.Disconnected != 2 || result.Failed != 0 {
		return fmt.Errorf("disconnect result=%#v", result)
	}
	if metrics := driver.Metrics(); metrics.CloseCalls != 0 || metrics.ResumeCalls != 0 {
		return fmt.Errorf("disconnect performed destructive close or eager resume: %#v", metrics)
	}

	for _, ref := range []agenthost.SessionRef{
		{WorkspaceID: "workspace-1", AgentSessionID: "session-disconnect-a"},
		{WorkspaceID: "workspace-1", AgentSessionID: "session-disconnect-b"},
	} {
		session, getErr := driver.GetSession(ctx, ref)
		if getErr != nil {
			return fmt.Errorf("get disconnected session %q: %w", ref.AgentSessionID, getErr)
		}
		if session.Live || session.ProviderSessionID == "" {
			return fmt.Errorf("disconnected session=%#v", session)
		}
	}

	other, err := driver.GetSession(ctx, agenthost.SessionRef{
		WorkspaceID: "workspace-2", AgentSessionID: "session-other-workspace",
	})
	if err != nil {
		return fmt.Errorf("get other workspace session: %w", err)
	}
	if !other.Live {
		return fmt.Errorf("other workspace session was disconnected: %#v", other)
	}

	replay, err := driver.DisconnectWorkspaceRuntime(ctx, "workspace-1")
	if err != nil {
		return fmt.Errorf("replay workspace runtime disconnect: %w", err)
	}
	if replay.Scanned != 2 || replay.Disconnected != 0 || replay.Failed != 0 {
		return fmt.Errorf("replay disconnect result=%#v", replay)
	}

	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-disconnect-a"}
	model, reasoningEffort, permissionMode := "model-after-disconnect", "high", "auto"
	updated, err := driver.UpdateSettings(ctx, agenthost.UpdateSettingsInput{
		WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
		Settings: agenthost.ComposerSettingsPatch{
			Model: &model, ReasoningEffort: &reasoningEffort, PermissionModeID: &permissionMode,
		},
	})
	if err != nil {
		return fmt.Errorf("update settings while workspace runtime is disconnected: %w", err)
	}
	if updated.Settings.Model != model || updated.Settings.ReasoningEffort != reasoningEffort ||
		updated.Settings.PermissionModeID != permissionMode || driver.Metrics().ResumeCalls != 0 {
		return fmt.Errorf("disconnected settings update=%#v metrics=%#v", updated, driver.Metrics())
	}
	sent, err := driver.SendInput(ctx, ref, agenthost.SendInput{
		ClientSubmitID: "resume-after-workspace-disconnect",
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "resume once"}},
	})
	if err != nil {
		return fmt.Errorf("send after workspace runtime disconnect: %w", err)
	}
	if sent.Session.ProviderSessionID != fixture.Session.ProviderSessionID {
		return fmt.Errorf("provider session id=%q, want %q", sent.Session.ProviderSessionID, fixture.Session.ProviderSessionID)
	}
	if sent.Session.Settings.Model != model || sent.Session.Settings.ReasoningEffort != reasoningEffort ||
		sent.Session.Settings.PermissionModeID != permissionMode {
		return fmt.Errorf("resumed settings=%#v, want disconnected update", sent.Session.Settings)
	}
	if metrics := driver.Metrics(); metrics.ResumeCalls != 1 {
		return fmt.Errorf("resume calls=%d, want 1", metrics.ResumeCalls)
	}
	return nil
}
