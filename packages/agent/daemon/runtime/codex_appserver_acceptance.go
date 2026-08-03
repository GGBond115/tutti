package agentruntime

import (
	"errors"
	"strings"
)

func reportCodexDispatchFailure(report ProviderDispatchSink, err error) {
	if report == nil {
		return
	}
	disposition := DispatchDispositionOutcomeUnknown
	var callErr *acpCallError
	if errors.As(err, &callErr) {
		disposition = DispatchDispositionRejected
	}
	report(ProviderDispatchResult{Disposition: disposition})
}

func reportCodexAppliedWithoutProviderTurn(report ProviderDispatchSink) {
	if report != nil {
		report(ProviderDispatchResult{
			Disposition: DispatchDispositionAppliedWithoutProviderTurn,
		})
	}
}

func reportCodexProviderTurnAccepted(
	report ProviderDispatchSink,
	providerSessionID string,
	providerTurnID string,
) {
	if report == nil {
		return
	}
	providerSessionID = strings.TrimSpace(providerSessionID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if providerSessionID == "" || providerTurnID == "" {
		report(ProviderDispatchResult{
			Disposition: DispatchDispositionOutcomeUnknown,
		})
		return
	}
	report(ProviderDispatchResult{
		Disposition: DispatchDispositionApplied,
		Acceptance: &ProviderAcceptanceReceipt{
			Source:            AcceptanceSourceTurnStartResponse,
			ProviderSessionID: providerSessionID,
			ProviderTurnID:    providerTurnID,
		},
	})
}

func (options codexTurnExecOptions) confirmProviderTurnAcceptance(
	providerSessionID string,
	providerTurnID string,
) error {
	if options.acceptProviderTurn == nil {
		reportCodexProviderTurnAccepted(
			options.reportDispatch,
			providerSessionID,
			providerTurnID,
		)
		return nil
	}
	providerSessionID = strings.TrimSpace(providerSessionID)
	providerTurnID = strings.TrimSpace(providerTurnID)
	if providerSessionID == "" || providerTurnID == "" {
		options.report(ProviderDispatchResult{
			Disposition: DispatchDispositionOutcomeUnknown,
		})
		return errors.New("codex provider turn acceptance omitted identity")
	}
	return options.acceptProviderTurn(ProviderAcceptanceReceipt{
		Source:            AcceptanceSourceTurnStartResponse,
		ProviderSessionID: providerSessionID,
		ProviderTurnID:    providerTurnID,
	})
}
