package agentruntime

import (
	"context"
	"errors"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func (a *CodexAppServerAdapter) newGuidanceContinuation(
	session Session,
	turnID string,
) (activityshared.Event, *codexGuidanceContinuationAdmission, error) {
	attemptID := "continuation:" + newID()
	eventContext, ok := activityEventContext(session, "root-provider-turn-started:"+attemptID, turnID)
	if !ok {
		return activityshared.Event{}, nil, ErrSessionDisconnected
	}
	started := activityshared.NewRootProviderTurnStarted(eventContext, turnID, attemptID)
	if binding, err := a.WriteProviderTurnBinding(ProviderTurnBindingWriteInput{
		Kind:           ProviderTurnBindingWriteStarted,
		ProviderTurnID: attemptID,
	}); err == nil {
		started.Payload.ProviderTurnBindingJSON = binding
	}
	started.Payload.Metadata = map[string]any{"guidanceContinuation": true}
	return started, newCodexGuidanceContinuationAdmission(attemptID), nil
}

func (a *CodexAppServerAdapter) startGuidanceContinuation(
	ctx context.Context,
	session Session,
	content []PromptContentBlock,
	displayPrompt string,
	turnID string,
	emit EventSink,
	emitCommands CommandSnapshotSink,
	reportDispatch ProviderDispatchSink,
) ([]activityshared.Event, error) {
	started, continuation, err := a.newGuidanceContinuation(session, turnID)
	if err != nil {
		reportGuidancePreconditionFailure(reportDispatch, err)
		return nil, err
	}
	dispatchObserver := newProviderDispatchObserver()
	if err := a.execAsync(
		context.WithoutCancel(ctx), session, content, displayPrompt, turnID,
		emit, emitCommands, continuation, dispatchObserver.Report,
	); err != nil {
		reportGuidancePreconditionFailure(reportDispatch, err)
		return nil, err
	}
	if err := <-continuation.admitted; err != nil {
		select {
		case observation := <-dispatchObserver.result:
			if reportDispatch != nil {
				reportDispatch(observation.dispatch)
			}
		default:
			reportGuidancePreconditionFailure(reportDispatch, err)
		}
		return nil, err
	}
	if emit != nil {
		emit([]activityshared.Event{started})
	}
	close(continuation.provisionalStarted)
	// Preserve the legacy/local adapter contract: GuideActiveTurn returns after
	// local continuation admission. Host callers use the additive dispatch-aware
	// method below this boundary and wait for a typed provider outcome.
	if reportDispatch == nil {
		return []activityshared.Event{started}, nil
	}
	select {
	case observation := <-dispatchObserver.result:
		if reportDispatch != nil {
			reportDispatch(observation.dispatch)
		}
		if observation.err != nil {
			return []activityshared.Event{started}, observation.err
		}
		if observation.dispatch.GuidanceDisposition != GuidanceDeliveryDispositionApplied {
			if observation.dispatch.Failure != nil {
				return []activityshared.Event{started}, observation.dispatch.Failure
			}
			return []activityshared.Event{started}, errors.New("guidance continuation was not applied")
		}
		return []activityshared.Event{started}, nil
	case <-ctx.Done():
		dispatch := ProviderDispatchResult{
			Disposition:         DispatchDispositionOutcomeUnknown,
			GuidanceDisposition: GuidanceDeliveryDispositionOutcomeUnknown,
			Failure:             ctx.Err(),
		}
		if reportDispatch != nil {
			reportDispatch(dispatch)
		}
		return []activityshared.Event{started}, ctx.Err()
	}
}

func reportGuidancePreconditionFailure(reportDispatch ProviderDispatchSink, err error) {
	if reportDispatch == nil {
		return
	}
	reportDispatch(ProviderDispatchResult{
		Disposition:         DispatchDispositionNotDispatched,
		GuidanceDisposition: GuidanceDeliveryDispositionPreconditionFailed,
		Failure:             err,
	})
}
