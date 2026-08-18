package contracts

import (
	"errors"
	"fmt"
	"time"
)

// CommandOutcome is the exhaustive result of crossing a public Connector
// command boundary. It deliberately distinguishes durable acceptance from a
// transport outcome whose acceptance cannot be established.
type CommandOutcome string

const (
	CommandAccepted  CommandOutcome = "accepted"
	CommandCompleted CommandOutcome = "completed"
	CommandRejected  CommandOutcome = "rejected"
	CommandUncertain CommandOutcome = "uncertain"
)

// CommandFailure is safe to expose across process boundaries. Message must be
// a non-secret diagnostic key or message; implementation causes stay internal.
type CommandFailure struct {
	Code      ErrorCode `json:"code"`
	Retryable bool      `json:"retryable"`
	Message   string    `json:"message"`
}

// CommandResult is the single public result for Connector mutations. Connector
// and the full Operation snapshot are retained for the one-version wire
// compatibility window; outcome/failure are the authoritative command facts.
type CommandResult struct {
	Outcome   CommandOutcome  `json:"outcome"`
	Revision  uint64          `json:"revision"`
	Connector *Connector      `json:"connector,omitempty"`
	Operation *Operation      `json:"operation,omitempty"`
	Failure   *CommandFailure `json:"failure,omitempty"`
}

// AuthorizationCommandResult adds the renderer-owned interaction envelope to
// the same command result without creating a second success/error model.
type AuthorizationCommandResult struct {
	CommandResult
	AuthorizationURL       string                     `json:"authorizationUrl,omitempty"`
	AuthorizationView      *AuthorizationViewEnvelope `json:"authorizationView,omitempty"`
	AuthorizationExpiresAt *time.Time                 `json:"authorizationExpiresAt,omitempty"`
}

// CancelAuthorizationCommand fences one exact active authorization operation.
// ConnectorKey alone is intentionally insufficient because a later attempt may
// already own the same Connector lane.
type CancelAuthorizationCommand struct {
	ConnectorMutation
	OperationID string `json:"operationId"`
}

func (result CommandResult) Validate() error {
	switch result.Outcome {
	case CommandAccepted:
		if result.Operation == nil || result.Failure != nil {
			return errors.New("accepted command result requires operation and forbids failure")
		}
		if result.Operation.State != OperationStateAccepted && result.Operation.State != OperationStateRunning {
			return errors.New("accepted command result requires an accepted or running operation")
		}
	case CommandCompleted:
		if result.Failure != nil {
			return errors.New("completed command result forbids failure")
		}
		if result.Operation != nil && result.Operation.State != OperationStateCompleted {
			return errors.New("completed command result operation must be completed")
		}
	case CommandRejected, CommandUncertain:
		if result.Failure == nil || result.Operation != nil {
			return fmt.Errorf("%s command result requires failure and forbids operation", result.Outcome)
		}
		if result.Failure.Code == "" || result.Failure.Message == "" {
			return fmt.Errorf("%s command result requires a closed failure", result.Outcome)
		}
		if result.Outcome == CommandUncertain && !result.Failure.Retryable {
			return errors.New("uncertain command result must be retryable through authoritative query")
		}
		if result.Failure.Code == ErrorCodeRevisionConflict && result.Failure.Retryable {
			return errors.New("revision conflict must not be retryable")
		}
	default:
		return fmt.Errorf("unknown command outcome %q", result.Outcome)
	}
	return nil
}
