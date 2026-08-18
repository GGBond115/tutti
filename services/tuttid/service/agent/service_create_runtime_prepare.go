package agent

import (
	"context"
	"strings"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func (s *Service) buildRuntimePrepareInput(
	ctx context.Context,
	workspaceID string,
	cwd string,
	input CreateSessionInput,
	effectiveEndpoint *runtimeprep.ModelEndpointConfig,
	effectiveBrowserUse bool,
	effectiveComputerUse bool,
) runtimeprep.PrepareInput {
	provider := strings.TrimSpace(input.Provider)
	authFingerprint := strings.TrimSpace(input.ProviderAuthFingerprint)
	if authFingerprint == "" {
		authFingerprint = providerAuthFingerprint(provider)
	}
	return runtimeprep.PrepareInput{
		WorkspaceID:               workspaceID,
		AgentSessionID:            strings.TrimSpace(input.AgentSessionID),
		ProviderSessionID:         strings.TrimSpace(input.ProviderSessionID),
		ProviderStateID:           providerStateIDFromRuntimeContext(input.RuntimeContext),
		ProviderAuthFingerprint:   authFingerprint,
		ImportedSession:           strings.TrimSpace(input.SessionOrigin) == agenthost.WorkspaceAgentSessionOriginImported,
		LegacyCodexHomePath:       strings.TrimSpace(input.LegacyCodexHomePath),
		AgentTargetID:             strings.TrimSpace(input.AgentTargetID),
		Provider:                  provider,
		Cwd:                       cwd,
		ModelEndpoint:             effectiveEndpoint,
		Title:                     value(input.Title),
		PermissionModeID:          value(input.PermissionModeID),
		PlanMode:                  clampComposerPlanModeForLaunch(provider, input.ProviderTargetRef, valueBool(input.PlanMode)),
		BrowserUse:                effectiveBrowserUse,
		ComputerUse:               effectiveComputerUse,
		CodexSaverMode:            valueBool(input.CodexSaverMode),
		ProviderTargetRef:         clonePayload(input.ProviderTargetRef),
		ExtensionSkillRoots:       s.resolveExtensionSkillRoots(ctx, input.ProviderTargetRef),
		ExtensionRuntimePrep:      s.resolveExtensionRuntimePrep(ctx, input.ProviderTargetRef),
		Model:                     clampComposerModelForLaunch(provider, input.ProviderTargetRef, value(input.Model)),
		ReasoningEffort:           normalizeReasoningEffortForLaunch(provider, input.ProviderTargetRef, value(input.ReasoningEffort)),
		ConversationDetailMode:    input.ConversationDetailMode,
		AgentName:                 input.AgentName,
		AgentDescription:          input.AgentDescription,
		AgentInstructions:         input.AgentInstructions,
		AgentCapabilitiesExplicit: input.AgentCapabilitiesExplicit,
		AgentSkills:               append([]string(nil), input.AgentSkills...),
		AgentTools:                append([]string(nil), input.AgentTools...),
		ExtraSkills:               sessionSkillBundlesToProviderSkillBundles(input.ExtraSkills),
		Metadata:                  input.Metadata,
		CommandCapabilityProjection: cloneCommandCapabilityProjection(
			input.CommandCapabilityProjection,
		),
		ExternalRolloutSourcePath: input.ExternalRolloutSourcePath,
	}
}
