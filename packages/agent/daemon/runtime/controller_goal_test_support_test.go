package agentruntime

import (
	"context"
	"sync"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
)

type goalPrepareBarrierReporter struct {
	mu       sync.Mutex
	prepared chan struct{}
	release  chan struct{}
	once     sync.Once
	phases   []string
}

type blockingGoalReconcileReporter struct{}

func (blockingGoalReconcileReporter) Report(ctx context.Context, _ agentsessionstore.ReportActivityInput) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingGoalReconcileReporter) AdmitSubmitIntent(_ context.Context, _ agentsessionstore.SubmitIntentInput) error {
	return nil
}

func (blockingGoalReconcileReporter) UpdateSubmitProvenance(context.Context, agentsessionstore.SubmitProvenanceInput) error {
	return nil
}

func (r *goalPrepareBarrierReporter) Report(ctx context.Context, report agentsessionstore.ReportActivityInput) error {
	for _, request := range report.GoalReconcileRequests {
		r.mu.Lock()
		r.phases = append(r.phases, request.Phase)
		r.mu.Unlock()
		if request.Phase == "quiesce_pending" {
			r.once.Do(func() { close(r.prepared) })
			select {
			case <-r.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (*goalPrepareBarrierReporter) AdmitSubmitIntent(_ context.Context, _ agentsessionstore.SubmitIntentInput) error {
	return nil
}

func (*goalPrepareBarrierReporter) UpdateSubmitProvenance(context.Context, agentsessionstore.SubmitProvenanceInput) error {
	return nil
}

func (r *goalPrepareBarrierReporter) phaseSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.phases...)
}
