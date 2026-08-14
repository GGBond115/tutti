package agentruntime

import (
	"errors"
	"sync"
)

// guidanceDispatchReporter keeps provider adapters on one exactly-once typed
// delivery seam. It intentionally classifies every error observed after a
// provider request begins as outcome unknown unless the provider returned an
// explicit protocol rejection.
type guidanceDispatchReporter struct {
	once sync.Once
	sink ProviderDispatchSink
}

func newGuidanceDispatchReporter(sink ProviderDispatchSink) *guidanceDispatchReporter {
	return &guidanceDispatchReporter{sink: sink}
}

func (r *guidanceDispatchReporter) report(result ProviderDispatchResult) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.sink != nil {
			r.sink(result)
		}
	})
}

func (r *guidanceDispatchReporter) applied() {
	r.report(ProviderDispatchResult{
		Disposition:         DispatchDispositionAppliedWithoutProviderTurn,
		GuidanceDisposition: GuidanceDeliveryDispositionApplied,
	})
}

func (r *guidanceDispatchReporter) preconditionFailed() {
	r.report(ProviderDispatchResult{
		Disposition:         DispatchDispositionNotDispatched,
		GuidanceDisposition: GuidanceDeliveryDispositionPreconditionFailed,
	})
}

func (r *guidanceDispatchReporter) explicitRejection(err error) {
	r.report(ProviderDispatchResult{
		Disposition:         DispatchDispositionRejected,
		GuidanceDisposition: GuidanceDeliveryDispositionExplicitRejection,
		Failure:             err,
	})
}

func (r *guidanceDispatchReporter) outcomeUnknown() {
	r.report(ProviderDispatchResult{
		Disposition:         DispatchDispositionOutcomeUnknown,
		GuidanceDisposition: GuidanceDeliveryDispositionOutcomeUnknown,
	})
}

func (r *guidanceDispatchReporter) providerError(err error) {
	var callErr *acpCallError
	if errors.As(err, &callErr) {
		r.explicitRejection(err)
		return
	}
	r.report(ProviderDispatchResult{
		Disposition:         DispatchDispositionOutcomeUnknown,
		GuidanceDisposition: GuidanceDeliveryDispositionOutcomeUnknown,
		Failure:             err,
	})
}
