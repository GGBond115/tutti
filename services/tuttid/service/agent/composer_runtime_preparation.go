package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
	tuttitypes "github.com/tutti-os/tutti/services/tuttid/types"
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
	if !composerProfileUsesAppServerCatalogPreparation(profile) {
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
	// Model discovery only needs the shared process profile and provider
	// configuration. Skills are Session/thread material and are prepared by
	// capability discovery or the actual Create path.
	prepared.SkipSkills = leaseKind == "model"
	return &prepared
}

func composerProfileUsesAppServerCatalogPreparation(profile composerProfile) bool {
	return profile.ModelCatalog == providerregistry.ModelCatalogKindCodexCLI ||
		profile.ModelCatalog == providerregistry.ModelCatalogKindTuttiCLI ||
		profile.CapabilityCatalogKind == providerregistry.CapabilityCatalogKindCodexAppServer ||
		profile.CapabilityCatalogKind == providerregistry.CapabilityCatalogKindAppServerSkills
}

func composerProfileUsesAppServerModelCatalog(profile composerProfile) bool {
	return profile.ModelCatalog == providerregistry.ModelCatalogKindCodexCLI ||
		profile.ModelCatalog == providerregistry.ModelCatalogKindTuttiCLI
}

func (s *Service) createModelCatalogPreparation(
	ctx context.Context,
	workspaceID string,
	provider string,
	cwd string,
	input CreateSessionInput,
) *runtimeprep.PrepareInput {
	if !composerProfileUsesAppServerModelCatalog(composerProfileFor(provider)) {
		return nil
	}
	input.Provider = provider
	input.Cwd = stringPointer(cwd)
	effectiveBrowserUse := s.clampComposerBrowserUseForLaunch(ctx, provider, input.ProviderTargetRef, input.BrowserUse)
	effectiveComputerUse := s.clampComposerComputerUseForLaunch(ctx, provider, input.ProviderTargetRef, input.ComputerUse)
	prepared := s.buildRuntimePrepareInput(
		ctx,
		workspaceID,
		cwd,
		input,
		nil,
		effectiveBrowserUse,
		effectiveComputerUse,
	)
	// Create-time model validation is still a model-only probe. The subsequent
	// actual Create preparation upgrades this shared profile with Session skills.
	prepared.SkipSkills = true
	return &prepared
}

func (s *Service) withCreateModelCatalogPreparation(
	ctx context.Context,
	workspaceID string,
	provider string,
	cwd string,
	input CreateSessionInput,
) context.Context {
	if preparation := s.createModelCatalogPreparation(ctx, workspaceID, provider, cwd, input); preparation != nil {
		return withRequestScopedAgentModelCatalogPreparation(ctx, preparation)
	}
	return ctx
}

func (s *Service) resolveComposerCatalogCwd(
	input ComposerOptionsInput,
	provider string,
	section ComposerOptionsSection,
) (string, error) {
	cwd := strings.TrimSpace(input.Cwd)
	profile := composerProfileFor(provider)
	needsCatalogCwd := composerProfileUsesAppServerCatalogPreparation(profile) &&
		(composerOptionsSectionIncludesCore(section) || composerOptionsSectionIncludesProviderCapabilities(section))
	if cwd != "" || !needsCatalogCwd {
		return cwd, nil
	}
	stateDir := strings.TrimSpace(s.WorktreeStateDir)
	if stateDir == "" {
		stateDir = tuttitypes.DefaultStateDir()
	}
	stateDir = filepath.Clean(stateDir)
	if stateDir == "." || stateDir == string(filepath.Separator) {
		return "", fmt.Errorf("agent catalog state directory is not configured")
	}
	catalogCwd := filepath.Join(stateDir, "agent", "catalog", agentprovider.Normalize(provider))
	if err := os.MkdirAll(catalogCwd, 0o700); err != nil {
		return "", fmt.Errorf("create agent catalog directory: %w", err)
	}
	return catalogCwd, nil
}

func composerCatalogAgentSessionID(workspaceID, targetID, provider, cwd, leaseKind string) string {
	return "composer-catalog:" + strings.Join([]string{
		strings.TrimSpace(workspaceID), strings.TrimSpace(targetID),
		strings.TrimSpace(provider), strings.TrimSpace(cwd), strings.TrimSpace(leaseKind),
	}, "\x00")
}
