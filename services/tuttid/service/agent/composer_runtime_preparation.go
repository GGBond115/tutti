package agent

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// composerCatalogPrepareInput is the one preparation-input projection used by
// Composer catalog probes. It deliberately delegates field construction to the
// same builder used by Create/Session runtime preparation; catalog adapters do
// not manufacture a second approximate process profile.
func (s *Service) composerCatalogPrepareInput(
	ctx context.Context,
	input ComposerOptionsInput,
	provider string,
	settings ComposerSettings,
	launchInput CreateSessionInput,
	endpoint *runtimeprep.ModelEndpointConfig,
	leaseKind string,
) *runtimeprep.PrepareInput {
	profile := composerProfileFor(provider)
	if profile.ModelCatalog != providerregistry.ModelCatalogKindCodexCLI &&
		profile.ModelCatalog != providerregistry.ModelCatalogKindTuttiCLI &&
		profile.CapabilityCatalogKind != providerregistry.CapabilityCatalogKindCodexAppServer &&
		profile.CapabilityCatalogKind != providerregistry.CapabilityCatalogKindAppServerSkills {
		return nil
	}
	launchInput.AgentSessionID = composerCatalogAgentSessionID(input.WorkspaceID, input.AgentTargetID, provider, input.Cwd, leaseKind)
	launchInput.AgentTargetID = strings.TrimSpace(input.AgentTargetID)
	launchInput.Provider = provider
	launchInput.ProviderTargetRef = clonePayload(input.providerTargetRef)
	launchInput.Model = stringPointer(settings.Model)
	launchInput.ReasoningEffort = stringPointer(settings.ReasoningEffort)
	launchInput.PermissionModeID = stringPointer(settings.PermissionModeID)
	launchInput.PlanMode = boolPointer(settings.PlanMode)
	launchInput.BrowserUse = cloneBoolPointer(settings.BrowserUse)
	launchInput.ComputerUse = cloneBoolPointer(settings.ComputerUse)
	launchInput.CodexSaverMode = boolPointer(settings.CodexSaverMode)
	launchInput.ConversationDetailMode = settings.ConversationDetailMode
	launchInput.Cwd = stringPointer(input.Cwd)
	effectiveBrowserUse := s.clampComposerBrowserUseForLaunch(ctx, provider, launchInput.ProviderTargetRef, launchInput.BrowserUse)
	effectiveComputerUse := s.clampComposerComputerUseForLaunch(ctx, provider, launchInput.ProviderTargetRef, launchInput.ComputerUse)
	prepared := s.buildRuntimePrepareInput(
		ctx,
		input.WorkspaceID,
		input.Cwd,
		launchInput,
		endpoint,
		effectiveBrowserUse,
		effectiveComputerUse,
	)
	prepared.SkipSkills = false
	return &prepared
}

func composerCatalogAgentSessionID(workspaceID, targetID, provider, cwd, leaseKind string) string {
	return "composer-catalog:" + strings.Join([]string{
		strings.TrimSpace(workspaceID), strings.TrimSpace(targetID),
		strings.TrimSpace(provider), strings.TrimSpace(cwd), strings.TrimSpace(leaseKind),
	}, "\x00")
}
