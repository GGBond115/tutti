package agentruntime

import (
	"context"
	"log/slog"
	"strings"
)

// scheduleProviderGoalAdoption moves provider-native create_goal persistence
// off the app-server read loop. The in-flight generation marker keeps any
// immediately following continuation buffered until Host accepts or rejects
// the generation.
func (a *CodexAppServerAdapter) scheduleProviderGoalAdoption(session Session, goal map[string]any) {
	fingerprint := codexGoalGenerationFingerprint(goal)
	if a == nil || fingerprint == "" ||
		strings.TrimSpace(asString(goal["status"])) != "active" {
		return
	}
	agentSessionID := strings.TrimSpace(session.AgentSessionID)
	a.mu.Lock()
	appSession := a.sessions[agentSessionID]
	if appSession == nil || appSession.provenanceDegraded {
		a.mu.Unlock()
		return
	}
	current := goalOperationIdentity{
		operationID: appSession.goalOperationID,
		revision:    appSession.goalRevision,
		repairEpoch: appSession.goalRepairEpoch,
	}
	if binding, found := appSession.goalGenerationBindings[fingerprint]; found &&
		!binding.ambiguous && binding.identity == current &&
		appSession.currentGoalGenerationFingerprint == fingerprint {
		a.mu.Unlock()
		return
	}
	sink := a.providerGoalAdoptionSink
	threadID := appSession.threadID
	if sink == nil || strings.TrimSpace(asString(goal["threadId"])) != threadID {
		a.mu.Unlock()
		return
	}
	if appSession.providerGoalAdoptionsInFlight == nil {
		appSession.providerGoalAdoptionsInFlight = make(map[string]struct{})
	}
	if _, inFlight := appSession.providerGoalAdoptionsInFlight[fingerprint]; inFlight {
		a.mu.Unlock()
		return
	}
	appSession.providerGoalAdoptionsInFlight[fingerprint] = struct{}{}
	a.mu.Unlock()
	session.ProviderSessionID = threadID
	go a.adoptProviderGoalGeneration(session, clonePayload(goal), fingerprint, sink, threadID)
}

// adoptProviderGoalGeneration gives a provider-native create_goal call a
// durable Host identity before any server-started continuation can inherit it.
// It does not weaken the existing fail-closed path: unavailable, conflicting,
// or malformed adoption leaves the continuation unproven.
func (a *CodexAppServerAdapter) adoptProviderGoalGeneration(
	session Session,
	goal map[string]any,
	fingerprint string,
	sink ProviderGoalAdoptionSink,
	threadID string,
) {
	agentSessionID := strings.TrimSpace(session.AgentSessionID)
	defer a.finishProviderGoalAdoption(agentSessionID, fingerprint)
	ackCtx, cancel := context.WithTimeout(context.Background(), goalProvenanceDurableAckTimeout)
	binding, err := sink(ackCtx, session, ProviderGoalAdoptionRequest{
		Fingerprint: fingerprint,
		Goal:        normalizedCodexGoal(goal),
	})
	cancel()
	if err != nil {
		slog.Warn("agent session app-server provider Goal adoption failed",
			"event", "agent_session.app_server.goal.provider_adoption_failed",
			"agent_session_id", agentSessionID,
			"error", err.Error(),
		)
		return
	}
	identity := goalOperationIdentity{
		operationID: strings.TrimSpace(binding.OperationID),
		revision:    binding.Revision,
		repairEpoch: binding.RepairEpoch,
	}
	if binding.Ambiguous || !identity.valid() {
		slog.Warn("agent session app-server provider Goal adoption returned invalid identity",
			"event", "agent_session.app_server.goal.provider_adoption_invalid",
			"agent_session_id", agentSessionID,
		)
		return
	}
	shouldArm, installed := a.installProviderGoalIdentity(agentSessionID, threadID, fingerprint, identity)
	if !installed {
		return
	}
	if err := a.bindGoalGeneration(context.Background(), session, goal, identity); err != nil {
		slog.Warn("agent session app-server provider Goal provenance binding failed",
			"event", "agent_session.app_server.goal.provider_adoption_binding_failed",
			"agent_session_id", agentSessionID,
			"error", err.Error(),
		)
		return
	}
	if shouldArm {
		a.armGoalContinuationClaim(agentSessionID, identity)
	}
	slog.Info("agent session app-server provider Goal adopted",
		"event", "agent_session.app_server.goal.provider_adopted",
		"agent_session_id", agentSessionID,
		"operation_id", identity.operationID,
		"revision", identity.revision,
	)
}

func (a *CodexAppServerAdapter) finishProviderGoalAdoption(agentSessionID, fingerprint string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	appSession := a.sessions[strings.TrimSpace(agentSessionID)]
	if appSession == nil {
		return
	}
	delete(appSession.providerGoalAdoptionsInFlight, strings.TrimSpace(fingerprint))
}

func (a *CodexAppServerAdapter) installProviderGoalIdentity(
	agentSessionID string,
	threadID string,
	fingerprint string,
	identity goalOperationIdentity,
) (bool, bool) {
	if a == nil || !identity.valid() {
		return false, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	appSession := a.sessions[strings.TrimSpace(agentSessionID)]
	if appSession == nil || appSession.provenanceDegraded || appSession.threadID != strings.TrimSpace(threadID) {
		return false, false
	}
	current := goalOperationIdentity{
		operationID: appSession.goalOperationID,
		revision:    appSession.goalRevision,
		repairEpoch: appSession.goalRepairEpoch,
	}
	switch {
	case current == identity:
		return appSession.currentGoalGenerationFingerprint != strings.TrimSpace(fingerprint), true
	case current.valid() && identity.revision <= current.revision:
		slog.Warn("agent session app-server provider Goal adoption lost identity race",
			"event", "agent_session.app_server.goal.provider_adoption_superseded",
			"agent_session_id", agentSessionID,
			"current_operation_id", current.operationID,
			"adopted_operation_id", identity.operationID,
		)
		return false, false
	default:
		appSession.goalOperationID = identity.operationID
		appSession.goalRevision = identity.revision
		appSession.goalRepairEpoch = identity.repairEpoch
		return true, true
	}
}
