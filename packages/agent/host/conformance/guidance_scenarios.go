package conformance

import (
	"context"
	"errors"
	"fmt"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

var (
	guidanceTargetRequiredScenario = Scenario{
		Name: "guidance requires exact target before dispatch",
		run:  runGuidanceTargetRequired,
	}
	guidanceExactTargetScenario = Scenario{
		Name: "guidance forwards exact target",
		run:  runGuidanceExactTarget,
	}
	guidanceTargetMismatchScenario = Scenario{
		Name: "guidance target mismatch does not dispatch provider and cleans claim",
		run:  runGuidanceTargetMismatch,
	}
	guidancePreconditionFenceScenario = Scenario{
		Name: "guidance precondition failure retains submit claim fence",
		run: func(ctx context.Context, driver Driver) error {
			return runGuidanceFailureFence(ctx, driver, agenthost.GuidanceDeliveryDispositionPreconditionFailed)
		},
	}
	guidanceExplicitRejectionFenceScenario = Scenario{
		Name: "guidance explicit rejection retains submit claim fence",
		run: func(ctx context.Context, driver Driver) error {
			return runGuidanceFailureFence(ctx, driver, agenthost.GuidanceDeliveryDispositionExplicitRejection)
		},
	}
	guidanceOutcomeUnknownFenceScenario = Scenario{
		Name: "guidance outcome unknown retains submit claim fence",
		run: func(ctx context.Context, driver Driver) error {
			return runGuidanceFailureFence(ctx, driver, agenthost.GuidanceDeliveryDispositionOutcomeUnknown)
		},
	}
	guidanceCleanupFailureFenceScenario = Scenario{
		Name: "guidance cleanup failure does not authorize ordinary reuse",
		run:  runGuidanceCleanupFailureFence,
	}
)

func GuidanceScenarios() []Scenario {
	return []Scenario{
		guidanceTargetRequiredScenario,
		guidanceExactTargetScenario,
		guidanceTargetMismatchScenario,
		guidancePreconditionFenceScenario,
		guidanceExplicitRejectionFenceScenario,
		guidanceOutcomeUnknownFenceScenario,
		guidanceCleanupFailureFenceScenario,
	}
}

func GuidanceRestartScenarios() []GuidanceRestartScenario {
	return []GuidanceRestartScenario{
		{
			Name: "guidance typed failures replay across Host restart without provider dispatch",
			run:  runGuidanceFailureRestartReplay,
		},
		{
			Name: "guidance applied local error replays across Host restart without provider dispatch",
			run:  runGuidanceAppliedErrorRestartReplay,
		},
	}
}

func GuidanceMutationAdmissionRestartScenarios() []GuidanceMutationAdmissionRestartScenario {
	return []GuidanceMutationAdmissionRestartScenario{{
		Name: "guidance canceled at mutation admission replays across Host restart",
		run:  runGuidanceMutationAdmissionCancellationRestartReplay,
	}}
}

func guidanceFixture(mismatch bool) Fixture {
	const (
		sessionID = "session-guidance"
		turnID    = "turn-guidance-active"
	)
	fixture := liveSessionFixture(sessionID, turnID)
	fixture.Turn = &TurnSeed{TurnID: turnID, Phase: canonical.TurnPhaseRunning}
	fixture.GuidanceTargetMismatch = mismatch
	return fixture
}

func guidanceInput(turnID, clientSubmitID string) agenthost.SendInput {
	return agenthost.SendInput{
		Content:  []agenthost.PromptContentBlock{{Type: "text", Text: "continue the active turn"}},
		Guidance: true, TurnID: turnID, ClientSubmitID: clientSubmitID,
	}
}

func runGuidanceTargetRequired(ctx context.Context, driver Driver) error {
	if err := driver.Reset(ctx, guidanceFixture(false)); err != nil {
		return err
	}
	result, err := driver.SendInput(ctx,
		agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"},
		guidanceInput("", "guidance-required"),
	)
	if !errors.Is(err, agenthost.ErrActiveTurnTargetRequired) {
		return fmt.Errorf("missing guidance target error=%v, want ErrActiveTurnTargetRequired", err)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		return fmt.Errorf("missing guidance target disposition=%q, want precondition failed", result.GuidanceDisposition)
	}
	metrics := driver.Metrics()
	if metrics.ExecCalls != 0 || metrics.GuidanceProviderCalls != 0 {
		return fmt.Errorf("missing guidance target dispatched exec=%d provider=%d, want 0/0", metrics.ExecCalls, metrics.GuidanceProviderCalls)
	}
	return nil
}

func runGuidanceExactTarget(ctx context.Context, driver Driver) error {
	if err := driver.Reset(ctx, guidanceFixture(false)); err != nil {
		return err
	}
	result, err := driver.SendInput(ctx,
		agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"},
		guidanceInput("turn-guidance-active", "guidance-exact"),
	)
	if err != nil {
		return fmt.Errorf("exact guidance target: %w", err)
	}
	if result.TurnID != "turn-guidance-active" {
		return fmt.Errorf("exact guidance target result turn=%q, want turn-guidance-active", result.TurnID)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionApplied {
		return fmt.Errorf("exact guidance disposition=%q, want applied", result.GuidanceDisposition)
	}
	metrics := driver.Metrics()
	if metrics.ExecCalls != 1 || metrics.GuidanceProviderCalls != 1 {
		return fmt.Errorf("exact guidance target dispatched exec=%d provider=%d, want 1/1", metrics.ExecCalls, metrics.GuidanceProviderCalls)
	}
	return nil
}

func runGuidanceTargetMismatch(ctx context.Context, driver Driver) error {
	if err := driver.Reset(ctx, guidanceFixture(true)); err != nil {
		return err
	}
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"}
	result, err := driver.SendInput(ctx, ref, guidanceInput("turn-guidance-stale", "guidance-mismatch"))
	if !errors.Is(err, agenthost.ErrActiveTurnTargetMismatch) {
		return fmt.Errorf("stale guidance target error=%v, want ErrActiveTurnTargetMismatch", err)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionTargetInactive {
		return fmt.Errorf("stale guidance disposition=%q, want target inactive", result.GuidanceDisposition)
	}
	metrics := driver.Metrics()
	if metrics.GuidanceProviderCalls != 0 {
		return fmt.Errorf("stale guidance target dispatched provider=%d, want 0", metrics.GuidanceProviderCalls)
	}

	// Reusing the same client submit id for ordinary work must prepare a fresh
	// claim after the known pre-provider rejection. This is the conversion
	// permission consumed by an adaptive steer-or-work owner.
	retried, err := driver.SendInput(ctx, ref, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "continue as ordinary work"}},
		ClientSubmitID: "guidance-mismatch",
	})
	if err != nil {
		return fmt.Errorf("ordinary work after target mismatch: %w", err)
	}
	if retried.TurnID == "" || retried.TurnID == "turn-guidance-stale" {
		return fmt.Errorf("ordinary work turn=%q, want a fresh canonical turn", retried.TurnID)
	}
	metrics = driver.Metrics()
	if metrics.GuidanceProviderCalls != 0 || metrics.ExecCalls != 2 {
		return fmt.Errorf("ordinary conversion dispatches exec=%d guidance=%d, want 2/0", metrics.ExecCalls, metrics.GuidanceProviderCalls)
	}
	return nil
}

func runGuidanceFailureFence(
	ctx context.Context,
	driver Driver,
	disposition agenthost.GuidanceDeliveryDisposition,
) error {
	fixture := guidanceFixture(false)
	fixture.GuidanceFailureDisposition = disposition
	if err := driver.Reset(ctx, fixture); err != nil {
		return err
	}
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"}
	clientSubmitID := "guidance-fence-" + string(disposition)
	result, err := driver.SendInput(ctx, ref, guidanceInput("turn-guidance-active", clientSubmitID))
	if err == nil {
		return fmt.Errorf("guidance disposition %q error=nil, want failure", disposition)
	}
	if result.GuidanceDisposition != disposition {
		return fmt.Errorf("guidance disposition=%q, want %q", result.GuidanceDisposition, disposition)
	}
	beforeReuse := driver.Metrics().ExecCalls
	_, reuseErr := driver.SendInput(ctx, ref, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "ordinary retry must stay fenced"}},
		ClientSubmitID: clientSubmitID,
	})
	if !errors.Is(reuseErr, agenthost.ErrSubmitDeliveryUnknown) {
		return fmt.Errorf("ordinary reuse after %q error=%v, want ErrSubmitDeliveryUnknown", disposition, reuseErr)
	}
	if got := driver.Metrics().ExecCalls; got != beforeReuse {
		return fmt.Errorf("ordinary reuse after %q exec calls=%d, want retained %d", disposition, got, beforeReuse)
	}
	return nil
}

func runGuidanceCleanupFailureFence(ctx context.Context, driver Driver) error {
	cleanupErr := errors.New("guidance claim cleanup unavailable")
	fixture := guidanceFixture(true)
	fixture.GuidanceDeleteClaimErr = cleanupErr
	if err := driver.Reset(ctx, fixture); err != nil {
		return err
	}
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"}
	const clientSubmitID = "guidance-cleanup-fence"
	result, err := driver.SendInput(ctx, ref, guidanceInput("turn-guidance-stale", clientSubmitID))
	if !errors.Is(err, agenthost.ErrGuidanceSubmitClaimCleanupFailed) || !errors.Is(err, cleanupErr) {
		return fmt.Errorf("guidance cleanup error=%v, want cleanup barrier failure", err)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		return fmt.Errorf("guidance cleanup disposition=%q, want precondition failed", result.GuidanceDisposition)
	}
	beforeReuse := driver.Metrics().ExecCalls
	_, reuseErr := driver.SendInput(ctx, ref, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "ordinary retry must stay fenced"}},
		ClientSubmitID: clientSubmitID,
	})
	if !errors.Is(reuseErr, agenthost.ErrSubmitDeliveryUnknown) {
		return fmt.Errorf("ordinary reuse after cleanup failure error=%v, want ErrSubmitDeliveryUnknown", reuseErr)
	}
	if got := driver.Metrics().ExecCalls; got != beforeReuse {
		return fmt.Errorf("ordinary reuse after cleanup failure exec calls=%d, want retained %d", got, beforeReuse)
	}
	return nil
}

func runGuidanceFailureRestartReplay(ctx context.Context, driver GuidanceRestartDriver) error {
	for _, disposition := range []agenthost.GuidanceDeliveryDisposition{
		agenthost.GuidanceDeliveryDispositionPreconditionFailed,
		agenthost.GuidanceDeliveryDispositionExplicitRejection,
		agenthost.GuidanceDeliveryDispositionOutcomeUnknown,
	} {
		fixture := guidanceFixture(false)
		fixture.GuidanceFailureDisposition = disposition
		if err := driver.Reset(ctx, fixture); err != nil {
			return err
		}
		ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"}
		input := guidanceInput("turn-guidance-active", "restart-"+string(disposition))
		first, firstErr := driver.SendInput(ctx, ref, input)
		if firstErr == nil || first.GuidanceDisposition != disposition {
			return fmt.Errorf("first guidance %q result=%#v error=%v", disposition, first, firstErr)
		}
		beforeRestart := driver.Metrics().ExecCalls
		if err := driver.RestartApplicationHost(ctx); err != nil {
			return err
		}
		replayed, replayErr := driver.SendInput(ctx, ref, input)
		if replayErr == nil || replayed.GuidanceDisposition != disposition {
			return fmt.Errorf("restart guidance %q result=%#v error=%v", disposition, replayed, replayErr)
		}
		if got := driver.Metrics().ExecCalls; got != beforeRestart {
			return fmt.Errorf("restart guidance %q exec calls=%d, want %d", disposition, got, beforeRestart)
		}
	}
	return nil
}

func runGuidanceAppliedErrorRestartReplay(ctx context.Context, driver GuidanceRestartDriver) error {
	acceptErr := errors.New("guidance claim accept unavailable")
	fixture := guidanceFixture(false)
	fixture.GuidanceAcceptClaimErr = acceptErr
	if err := driver.Reset(ctx, fixture); err != nil {
		return err
	}
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"}
	input := guidanceInput("turn-guidance-active", "restart-applied")
	first, firstErr := driver.SendInput(ctx, ref, input)
	if !errors.Is(firstErr, acceptErr) || first.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionApplied {
		return fmt.Errorf("first applied guidance result=%#v error=%v", first, firstErr)
	}
	beforeRestart := driver.Metrics().ExecCalls
	if err := driver.RestartApplicationHost(ctx); err != nil {
		return err
	}
	replayed, replayErr := driver.SendInput(ctx, ref, input)
	if replayErr != nil || replayed.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionApplied {
		return fmt.Errorf("restart applied guidance result=%#v error=%v", replayed, replayErr)
	}
	if got := driver.Metrics().ExecCalls; got != beforeRestart {
		return fmt.Errorf("restart applied guidance exec calls=%d, want %d", got, beforeRestart)
	}
	return nil
}

func runGuidanceMutationAdmissionCancellationRestartReplay(
	ctx context.Context,
	driver GuidanceMutationAdmissionRestartDriver,
) error {
	if err := driver.Reset(ctx, guidanceFixture(false)); err != nil {
		return err
	}
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-guidance"}
	input := guidanceInput("turn-guidance-active", "restart-mutation-admission-canceled")
	first, firstErr := driver.SendGuidanceCanceledWhileWaitingForMutation(ctx, ref, input)
	if !errors.Is(firstErr, context.Canceled) ||
		first.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		return fmt.Errorf("canceled guidance result=%#v error=%v, want durable precondition", first, firstErr)
	}
	if got := driver.Metrics().ExecCalls; got != 0 {
		return fmt.Errorf("canceled guidance exec calls=%d, want 0", got)
	}
	if err := driver.RestartApplicationHost(ctx); err != nil {
		return err
	}
	replayed, replayErr := driver.SendInput(ctx, ref, input)
	if !errors.Is(replayErr, agenthost.ErrGuidancePreconditionFailed) ||
		replayed.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		return fmt.Errorf("restart canceled guidance result=%#v error=%v, want original precondition", replayed, replayErr)
	}
	if got := driver.Metrics().ExecCalls; got != 0 {
		return fmt.Errorf("restart canceled guidance exec calls=%d, want 0", got)
	}
	return nil
}
