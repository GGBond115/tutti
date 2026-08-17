package storesqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SubmitIntentAdmission contains exactly one canonical user message. The
// claim and activity report are committed by one SQLite transaction; callers
// must not separately report the same canonical message after admission.
type SubmitIntentAdmission struct {
	Claim    SubmitClaimPrepare
	Activity ActivityStateReport
}

type SubmitIntentAdmissionResult struct {
	Claim    SubmitClaim
	Activity ActivityStateReportResult
}

// AdmitSubmitIntent is the canonical-message ownership boundary. It creates
// or replays one submit claim and one canonical user message atomically. A
// replay with a different turn, message identity, or content is rejected.
func (s *Store) AdmitSubmitIntent(ctx context.Context, input SubmitIntentAdmission) (SubmitIntentAdmissionResult, error) {
	message, err := validateSubmitIntentAdmission(&input)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	if s == nil || s.db == nil {
		return SubmitIntentAdmissionResult{}, errors.New("workspace database is not initialized")
	}
	if err := s.ensureWorkspaceExists(ctx, input.Claim.WorkspaceID); err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	var result SubmitIntentAdmissionResult
	err = retrySQLiteBusy(ctx, func(attemptCtx context.Context) error {
		var err error
		result, err = s.admitSubmitIntentOnce(attemptCtx, input, message)
		return err
	})
	return result, err
}

func validateSubmitIntentAdmission(input *SubmitIntentAdmission) (MessageUpdate, error) {
	input.Claim.WorkspaceID = strings.TrimSpace(input.Claim.WorkspaceID)
	input.Claim.AgentSessionID = strings.TrimSpace(input.Claim.AgentSessionID)
	input.Claim.ClientSubmitID = strings.TrimSpace(input.Claim.ClientSubmitID)
	input.Claim.CanonicalTurnID = strings.TrimSpace(input.Claim.CanonicalTurnID)
	input.Claim.CanonicalMessageID = strings.TrimSpace(input.Claim.CanonicalMessageID)
	input.Claim.CanonicalContentHash = strings.TrimSpace(input.Claim.CanonicalContentHash)
	input.Activity.Session.WorkspaceID = strings.TrimSpace(input.Activity.Session.WorkspaceID)
	input.Activity.Session.AgentSessionID = strings.TrimSpace(input.Activity.Session.AgentSessionID)
	if input.Claim.WorkspaceID == "" || input.Claim.AgentSessionID == "" || input.Claim.ClientSubmitID == "" ||
		input.Claim.CanonicalTurnID == "" || input.Claim.NowUnixMS <= 0 {
		return MessageUpdate{}, errors.New("submit intent claim identity is incomplete")
	}
	if input.Activity.Session.WorkspaceID != input.Claim.WorkspaceID || input.Activity.Session.AgentSessionID != input.Claim.AgentSessionID {
		return MessageUpdate{}, errors.New("submit intent activity scope does not match claim")
	}
	if input.Activity.Turn == nil || strings.TrimSpace(input.Activity.Turn.TurnID) != input.Claim.CanonicalTurnID {
		return MessageUpdate{}, errors.New("submit intent admission requires the claimed canonical turn")
	}
	if len(input.Activity.Messages) != 1 {
		return MessageUpdate{}, errors.New("submit intent admission requires exactly one canonical message")
	}
	message := input.Activity.Messages[0]
	message.MessageID = strings.TrimSpace(message.MessageID)
	message.TurnID = strings.TrimSpace(message.TurnID)
	if message.MessageID == "" || message.TurnID == "" || message.TurnID != input.Claim.CanonicalTurnID {
		return MessageUpdate{}, errors.New("submit intent canonical message identity does not match claim")
	}
	if strings.TrimSpace(message.Role) != "user" {
		return MessageUpdate{}, errors.New("submit intent canonical message must have user role")
	}
	if payloadString(message.Payload, "clientSubmitId") != input.Claim.ClientSubmitID {
		return MessageUpdate{}, errors.New("submit intent canonical message client submit id does not match claim")
	}
	hash, err := canonicalSubmitContentHash(message)
	if err != nil {
		return MessageUpdate{}, err
	}
	if input.Claim.CanonicalMessageID != "" && input.Claim.CanonicalMessageID != message.MessageID {
		return MessageUpdate{}, fmt.Errorf("%w: canonical message does not match claim", ErrSubmitClaimIdentityConflict)
	}
	if input.Claim.CanonicalContentHash != "" && input.Claim.CanonicalContentHash != hash {
		return MessageUpdate{}, fmt.Errorf("%w: canonical content does not match claim", ErrSubmitClaimIdentityConflict)
	}
	input.Claim.CanonicalMessageID = message.MessageID
	input.Claim.CanonicalContentHash = hash
	input.Activity.Messages[0] = message
	return message, nil
}

func (s *Store) admitSubmitIntentOnce(
	ctx context.Context,
	input SubmitIntentAdmission,
	message MessageUpdate,
) (SubmitIntentAdmissionResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SubmitIntentAdmissionResult{}, fmt.Errorf("begin admit submit intent: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	claim, found, err := getSubmitClaimTx(ctx, tx, input.Claim.WorkspaceID, input.Claim.AgentSessionID, input.Claim.ClientSubmitID)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	if found {
		if err := validateSubmitClaimIdentity(claim, input.Claim.CanonicalTurnID, input.Claim.CanonicalMessageID, input.Claim.CanonicalContentHash); err != nil {
			return SubmitIntentAdmissionResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE workspace_agent_submit_claims
SET canonical_turn_id = COALESCE(NULLIF(canonical_turn_id, ''), ?),
    canonical_message_id = COALESCE(NULLIF(canonical_message_id, ''), ?),
    canonical_content_hash = COALESCE(NULLIF(canonical_content_hash, ''), ?)
WHERE workspace_id = ? AND agent_session_id = ? AND client_submit_id = ?
`, input.Claim.CanonicalTurnID, input.Claim.CanonicalMessageID, input.Claim.CanonicalContentHash,
			input.Claim.WorkspaceID, input.Claim.AgentSessionID, input.Claim.ClientSubmitID); err != nil {
			return SubmitIntentAdmissionResult{}, fmt.Errorf("bind existing submit intent identity: %w", err)
		}
	} else {
		if err := requireSessionForkSourceWritableTx(ctx, tx, input.Claim.WorkspaceID, input.Claim.AgentSessionID); err != nil {
			return SubmitIntentAdmissionResult{}, err
		}
		result, err := tx.ExecContext(ctx, `
INSERT INTO workspace_agent_submit_claims (
  workspace_id, agent_session_id, client_submit_id, status, turn_id,
  created_at_unix_ms, updated_at_unix_ms, canonical_turn_id,
  canonical_message_id, canonical_content_hash
) VALUES (?, ?, ?, 'prepared', NULL, ?, ?, ?, ?, ?)
`, input.Claim.WorkspaceID, input.Claim.AgentSessionID, input.Claim.ClientSubmitID,
			input.Claim.NowUnixMS, input.Claim.NowUnixMS, input.Claim.CanonicalTurnID,
			nullString(input.Claim.CanonicalMessageID), nullString(input.Claim.CanonicalContentHash))
		if err != nil {
			return SubmitIntentAdmissionResult{}, fmt.Errorf("admit submit intent claim: %w", err)
		}
		if _, err := rowsWereAffected(result, "admit submit intent claim"); err != nil {
			return SubmitIntentAdmissionResult{}, err
		}
	}

	activity := input.Activity
	now := input.Claim.NowUnixMS
	goalBefore, err := readSessionGoalProjectionTx(ctx, tx, activity.Session)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	accepted, stateApplied, lastEventUnixMS, session, err := s.upsertAgentSessionTx(ctx, tx, activity.Session, now)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	if !accepted {
		return SubmitIntentAdmissionResult{}, errors.New("submit intent activity session was rejected")
	}
	sessionWritable, err := sessionActivityWritableTx(ctx, tx, input.Claim.WorkspaceID, input.Claim.AgentSessionID)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	result := ActivityStateReportResult{State: StateReportResult{
		Accepted: accepted, StateApplied: stateApplied, LastEventUnixMS: lastEventUnixMS, Session: session,
	}}
	result.Messages.LatestVersion = session.MessageVersion
	result.Turn, result.TurnAccepted, err = s.recordTurnTransitionTx(ctx, tx, *activity.Turn, now)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	if !sessionWritable || (!result.TurnAccepted && !turnTransitionAlreadyApplied(result.Turn, *activity.Turn)) {
		return SubmitIntentAdmissionResult{}, errors.New("submit intent canonical turn was rejected")
	}
	acceptedMessage, messageAccepted, _, err := s.upsertAgentMessageTx(ctx, tx, input.Claim.WorkspaceID, input.Claim.AgentSessionID, message, now, false, true)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	if !messageAccepted {
		return SubmitIntentAdmissionResult{}, errors.New("submit intent canonical message was rejected")
	}
	result.Messages.AcceptedCount = 1
	result.Messages.LatestVersion = acceptedMessage.Version
	result.Messages.Messages = []Message{acceptedMessage}
	goalMutations, err := sessionGoalMutationsTx(ctx, tx, activity.Session, goalBefore)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	turnTerminalTransition := result.TurnAccepted &&
		strings.TrimSpace(activity.Turn.Phase) == TurnPhaseSettled
	mutations := activityStateMutations(result, turnTerminalTransition, false)
	mutations = append(mutations, goalMutations...)
	delta, err := s.commitTransaction(ctx, tx, input.Claim.WorkspaceID, mutations)
	if err != nil {
		return SubmitIntentAdmissionResult{}, fmt.Errorf("commit admit submit intent: %w", err)
	}
	committed = true
	result.TransactionID = delta.TransactionID
	result.CommitDelta = delta
	result.State.TransactionID = delta.TransactionID
	result.State.CommitDelta = delta
	result.State.Session.CommitTransactionID = delta.TransactionID
	result.State.Session.CommitDelta = delta
	claim, found, err = s.GetSubmitClaim(ctx, input.Claim.WorkspaceID, input.Claim.AgentSessionID, input.Claim.ClientSubmitID)
	if err != nil {
		return SubmitIntentAdmissionResult{}, err
	}
	if !found {
		return SubmitIntentAdmissionResult{}, errors.New("admitted submit claim disappeared before it could be read")
	}
	return SubmitIntentAdmissionResult{Claim: claim, Activity: result}, nil
}
