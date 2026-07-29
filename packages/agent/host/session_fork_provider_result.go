package agenthost

import "strings"

func validSessionForkProviderResult(
	result RuntimeSessionForkResult,
) bool {
	switch result.StateBindingMode {
	case SessionForkStateBindingHostCopy:
		return len(result.TargetProviderTurnBindings) == 0 &&
			result.StateBindingReceipt == ""
	case SessionForkStateBindingProviderOwned:
		if result.StateBindingReceipt == "" ||
			len(result.TargetProviderTurnBindings) == 0 {
			return false
		}
		seenProviderTurnIDs := make(
			map[string]struct{},
			len(result.TargetProviderTurnBindings),
		)
		seenCheckpointMessageIDs := make(
			map[string]struct{},
			len(result.TargetProviderTurnBindings),
		)
		for _, binding := range result.TargetProviderTurnBindings {
			providerTurnID := strings.TrimSpace(binding.ProviderTurnID)
			checkpointMessageID := strings.TrimSpace(binding.CheckpointMessageID)
			if providerTurnID == "" || checkpointMessageID == "" {
				return false
			}
			if _, duplicate := seenProviderTurnIDs[providerTurnID]; duplicate {
				return false
			}
			if _, duplicate := seenCheckpointMessageIDs[checkpointMessageID]; duplicate {
				return false
			}
			seenProviderTurnIDs[providerTurnID] = struct{}{}
			seenCheckpointMessageIDs[checkpointMessageID] = struct{}{}
		}
		return true
	default:
		return false
	}
}
