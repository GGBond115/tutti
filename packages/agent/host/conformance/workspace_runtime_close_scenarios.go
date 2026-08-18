package conformance

import (
	"context"
	"fmt"
	"slices"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

func WorkspaceRuntimeCloseScenarios() []WorkspaceRuntimeCloseScenario {
	return []WorkspaceRuntimeCloseScenario{{
		Name: "close live runtime preserves canonical state and retries cleanup",
		run:  runCloseLiveRuntimePreservesCanonicalStateAndRetriesCleanup,
	}}
}

func runCloseLiveRuntimePreservesCanonicalStateAndRetriesCleanup(
	ctx context.Context,
	driver WorkspaceRuntimeCloseDriver,
) error {
	const (
		workspaceID     = "workspace-1"
		failedSessionID = "session-close-failed"
		successID       = "session-close-success"
	)
	fixture := Fixture{
		RuntimePreparationCleanupFailureSessionID: failedSessionID,
	}
	if err := driver.Reset(ctx, fixture); err != nil {
		return err
	}
	for _, sessionID := range []string{failedSessionID, successID} {
		if _, _, err := driver.Create(ctx, workspaceID, agenthost.CreateSessionInput{
			AgentSessionID: sessionID, AgentTargetID: "target-1", Provider: "codex",
		}); err != nil {
			return fmt.Errorf("create session %q: %w", sessionID, err)
		}
	}

	first, firstErr := driver.CloseLiveRuntimeSession(ctx, agenthost.SessionRef{
		WorkspaceID: workspaceID, AgentSessionID: failedSessionID,
	})
	if firstErr == nil || !first.Closed || !first.PreparationCleanupFailed {
		return fmt.Errorf("first close result/error=%#v/%v, want close success and cleanup failure", first, firstErr)
	}
	retry, retryErr := driver.CloseLiveRuntimeSession(ctx, agenthost.SessionRef{
		WorkspaceID: workspaceID, AgentSessionID: failedSessionID,
	})
	if retryErr != nil || retry.Closed || !retry.PreparationCleanupAttempted || retry.PreparationCleanupFailed {
		return fmt.Errorf("cleanup retry result/error=%#v/%v, want retry without runtime recreation", retry, retryErr)
	}
	if got, want := driver.RuntimePreparationCleanupSessionIDs(), []string{failedSessionID, failedSessionID}; !slices.Equal(got, want) {
		return fmt.Errorf("single close cleanup IDs=%#v, want fail-first then retry=%#v", got, want)
	}
	for _, sessionID := range []string{failedSessionID, successID} {
		canonical, err := driver.GetCanonicalSession(ctx, agenthost.SessionRef{
			WorkspaceID: workspaceID, AgentSessionID: sessionID,
		})
		if err != nil {
			return fmt.Errorf("get canonical session %q: %w", sessionID, err)
		}
		if canonical.SessionID == "" || !canonical.Resumable {
			return fmt.Errorf("canonical session state lost for %q: %#v", sessionID, canonical)
		}
	}

	if err := driver.Reset(ctx, fixture); err != nil {
		return err
	}
	for _, sessionID := range []string{failedSessionID, successID} {
		if _, _, err := driver.Create(ctx, workspaceID, agenthost.CreateSessionInput{
			AgentSessionID: sessionID, AgentTargetID: "target-1", Provider: "codex",
		}); err != nil {
			return fmt.Errorf("create session %q: %w", sessionID, err)
		}
	}
	all, allErr := driver.CloseAllLiveRuntimeSessions(ctx)
	if allErr == nil || all.Scanned != 2 || all.Closed != 2 || all.Failed != 1 ||
		all.PreparationCleanupAttempted != 2 || all.PreparationCleanupFailed != 1 {
		return fmt.Errorf("close-all result/error=%#v/%v, want both closes and aggregated cleanup failure", all, allErr)
	}
	retryAll, retryAllErr := driver.CloseAllLiveRuntimeSessions(ctx)
	if retryAllErr != nil || retryAll.Scanned != 1 || retryAll.Closed != 0 || retryAll.Failed != 0 ||
		retryAll.PreparationCleanupAttempted != 1 || retryAll.PreparationCleanupFailed != 0 {
		return fmt.Errorf("close-all cleanup retry result/error=%#v/%v", retryAll, retryAllErr)
	}
	if got, want := driver.RuntimePreparationCleanupSessionIDs(), []string{failedSessionID, successID, failedSessionID}; !slices.Equal(got, want) {
		return fmt.Errorf("close-all cleanup IDs=%#v, want both sessions then pending retry=%#v", got, want)
	}
	if !driver.Metrics().LastClosePreservedCanonicalState {
		return fmt.Errorf("last close canonical preservation=false, want true")
	}
	return nil
}
