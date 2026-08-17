package agentsessionstore

import (
	"context"

	replay "github.com/tutti-os/tutti/packages/agent/session-replay"
)

type ActivityReporter interface {
	Report(ctx context.Context, input ReportActivityInput) error
}

// SubmitIntentReporter is the explicit durable submit boundary. It is kept
// separate from SessionActivityReporter so ordinary activity implementations
// cannot accidentally manufacture canonical user messages.
type SubmitIntentReporter interface {
	AdmitSubmitIntent(context.Context, SubmitIntentInput) (SubmitIntentReply, error)
	UpdateSubmitProvenance(context.Context, SubmitProvenanceInput) (SubmitProvenanceReply, error)
}

type SessionActivityReporter interface {
	ReportSessionState(context.Context, ReportSessionStateInput) (ReportSessionStateReply, error)
	ReportSessionMessages(context.Context, ReportSessionMessagesInput) (ReportSessionMessagesReply, error)
}

// SessionActivityCommitReporter joins noncanonical Replay observation
// metadata to the Host post-commit notification without putting that metadata
// in canonical Store command DTOs.
type SessionActivityCommitReporter interface {
	ReportSessionStateWithCommitContext(
		context.Context,
		ReportSessionStateInput,
		replay.ProviderObservationCommitContext,
	) (ReportSessionStateReply, error)
	ReportSessionMessagesWithCommitContext(
		context.Context,
		ReportSessionMessagesInput,
		replay.ProviderObservationCommitContext,
	) (ReportSessionMessagesReply, error)
}

// GoalReconcileRequestReporter is an internal control-plane extension. Unlike
// session audits it never persists in or hydrates from the transcript.
type GoalReconcileRequestReporter interface {
	ReportGoalReconcileRequired(context.Context, ReportGoalReconcileRequiredInput) (ReportGoalReconcileRequiredReply, error)
}

// GoalProvenanceLedger is deliberately synchronous: a provider adapter must
// fail closed when durable provenance cannot be bound or resolved.
type GoalProvenanceLedger interface {
	BindGoalProvenance(context.Context, BindGoalProvenanceInput) (GoalProvenanceBinding, error)
	LookupGoalProvenance(context.Context, LookupGoalProvenanceInput) (GoalProvenanceBinding, bool, error)
}
