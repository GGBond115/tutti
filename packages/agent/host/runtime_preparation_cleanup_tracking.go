package agenthost

import (
	"strings"
	"sync"
)

type runtimePreparationCleanupState struct {
	mu      sync.Mutex
	pending map[string]RuntimeCleanupInput
}

func newRuntimePreparationCleanupState() *runtimePreparationCleanupState {
	return &runtimePreparationCleanupState{pending: make(map[string]RuntimeCleanupInput)}
}

func runtimePreparationCleanupKey(workspaceID, agentSessionID string) string {
	return strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(agentSessionID)
}

func (h *Host) rememberPendingRuntimePreparationCleanup(input RuntimeCleanupInput) {
	if h == nil || strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.AgentSessionID) == "" {
		return
	}
	if h.runtimeCleanupState == nil {
		return
	}
	h.runtimeCleanupState.mu.Lock()
	h.runtimeCleanupState.pending[runtimePreparationCleanupKey(input.WorkspaceID, input.AgentSessionID)] = input
	h.runtimeCleanupState.mu.Unlock()
}

func (h *Host) forgetPendingRuntimePreparationCleanup(input RuntimeCleanupInput) {
	if h == nil {
		return
	}
	if h.runtimeCleanupState == nil {
		return
	}
	h.runtimeCleanupState.mu.Lock()
	delete(h.runtimeCleanupState.pending, runtimePreparationCleanupKey(input.WorkspaceID, input.AgentSessionID))
	h.runtimeCleanupState.mu.Unlock()
}

func (h *Host) pendingRuntimePreparationCleanup(ref SessionRef) (RuntimeCleanupInput, bool) {
	if h == nil {
		return RuntimeCleanupInput{}, false
	}
	if h.runtimeCleanupState == nil {
		return RuntimeCleanupInput{}, false
	}
	h.runtimeCleanupState.mu.Lock()
	input, ok := h.runtimeCleanupState.pending[runtimePreparationCleanupKey(ref.WorkspaceID, ref.AgentSessionID)]
	h.runtimeCleanupState.mu.Unlock()
	return input, ok
}

func (h *Host) pendingRuntimePreparationCleanupSnapshot() []RuntimeCleanupInput {
	if h == nil {
		return nil
	}
	if h.runtimeCleanupState == nil {
		return nil
	}
	h.runtimeCleanupState.mu.Lock()
	result := make([]RuntimeCleanupInput, 0, len(h.runtimeCleanupState.pending))
	for _, input := range h.runtimeCleanupState.pending {
		result = append(result, input)
	}
	h.runtimeCleanupState.mu.Unlock()
	return result
}
