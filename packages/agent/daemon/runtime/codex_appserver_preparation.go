package agentruntime

import (
	"context"
	"errors"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

func (a *CodexAppServerAdapter) prepareInitializedClientLaunch(
	ctx context.Context,
	session Session,
) (appServerPreparedLaunch, error) {
	if a == nil || a.transport == nil {
		return appServerPreparedLaunch{}, errors.New(
			"app-server process transport is unavailable",
		)
	}
	command := append([]string(nil), a.config.command...)
	spawnEnv := append(codexACPEnv(session, a.host), session.Env...)
	var resolvedProcessEnv []string
	if a.commandResolver != nil {
		resolved, err := a.commandResolver(ctx, a.config.provider)
		if err != nil {
			return appServerPreparedLaunch{}, err
		}
		if len(resolved.Command) > 0 {
			command = append([]string(nil), resolved.Command...)
		}
		spawnEnv = append(spawnEnv, resolved.Env...)
		resolvedProcessEnv = append(resolvedProcessEnv, resolved.Env...)
	}
	spec := ProcessSpec{
		Provider:           a.config.provider,
		AgentSessionID:     session.AgentSessionID,
		RootAgentSessionID: session.RootAgentSessionID,
		RoomID:             session.RoomID,
		CWD:                a.sessionCWD(session),
		ProtocolCWD:        a.sessionCWD(session),
		Command:            command,
		Env:                spawnEnv,
	}
	if prepared := session.AppServer; prepared != nil && a.preparer == nil {
		profile := AppServerProcessProfile{
			ExecutionHostID: prepared.ExecutionHostID, RuntimeGeneration: prepared.RuntimeGeneration,
			TransportScopeID: prepared.TransportScopeID, ProcessProfileDigest: prepared.ProcessProfileDigest,
			Command: append([]string(nil), command...),
			Env:     append(append([]string{codexAgentRoutingEnv, codexRoutingPreload, "NO_BROWSER=1"}, prepared.ProcessEnv...), resolvedProcessEnv...),
			CWD:     prepared.ProcessCWD,
		}
		spec.Command = append([]string(nil), profile.Command...)
		spec.Env = append([]string(nil), profile.Env...)
		spec.CWD = profile.CWD
		if err := validateAppServerProcessProfile(profile); err != nil {
			return appServerPreparedLaunch{}, err
		}
		overlay := AppServerThreadOverlay{
			Env: append([]string(nil), prepared.ThreadEnv...), MCPServers: cloneMCPServerBindings(session.MCPServers),
			ModelProviderCredentials: cloneAppServerModelProviderCredentials(prepared.ModelProviderCredentials),
			BaseInstructions:         prepared.BaseInstructions, DeveloperInstructions: prepared.DeveloperInstructions,
		}
		if a.config.skillRootsStrategy == providerregistry.AppServerSkillRootsStrategyTuttiStable {
			overlay.Env = withoutEnvironmentKey(overlay.Env, tuttiAgentExtraSkillRootsEnv)
			overlay.Env = withoutEnvironmentKey(overlay.Env, tuttiAgentStableSystemSkillsEnv)
		}
		return appServerPreparedLaunch{
			spec: spec, profile: &profile, overlay: overlay,
		}, nil
	}
	if a.preparer == nil {
		return appServerPreparedLaunch{}, errors.New(
			"app-server launch requires explicit process profile preparation",
		)
	}
	launchSession := cloneProviderLaunchSession(session)
	if runtimeContext := providerLaunchRuntimeContext(ctx); runtimeContext != nil {
		launchSession.RuntimeContext = runtimeContext
	}
	result, err := a.preparer(ctx, ProviderLaunchPrepareInput{
		Provider: spec.Provider, Session: launchSession,
		Command: append([]string(nil), spec.Command...), Env: append([]string(nil), spec.Env...),
		CWD: spec.CWD,
	})
	if err != nil {
		return appServerPreparedLaunch{}, err
	}
	if result.AppServer == nil {
		return appServerPreparedLaunch{}, errors.Join(
			errors.New("app-server launch preparation must include an explicit process profile"),
			cleanupPreparedLaunch(result.Cleanup),
		)
	}
	if result.Cleanup != nil {
		return appServerPreparedLaunch{}, errors.New("app-server preparation must split process and thread cleanup leases")
	}
	profile := result.AppServer.ProcessProfile
	profile.Command = append([]string(nil), profile.Command...)
	profile.Env = append([]string(nil), profile.Env...)
	if err := validateAppServerProcessProfile(profile); err != nil {
		return appServerPreparedLaunch{}, err
	}
	spec.Command = append([]string(nil), profile.Command...)
	spec.Env = append([]string(nil), profile.Env...)
	spec.CWD = profile.CWD
	overlay := result.AppServer.ThreadOverlay
	overlay.Env = append([]string(nil), overlay.Env...)
	overlay.MCPServers = cloneMCPServerBindings(overlay.MCPServers)
	overlay.ModelProviderCredentials = cloneAppServerModelProviderCredentials(overlay.ModelProviderCredentials)
	if a.config.skillRootsStrategy == providerregistry.AppServerSkillRootsStrategyTuttiStable {
		spec.Env = withoutEnvironmentKey(spec.Env, tuttiAgentExtraSkillRootsEnv)
		spec.Env = withoutEnvironmentKey(spec.Env, tuttiAgentStableSystemSkillsEnv)
		overlay.Env = withoutEnvironmentKey(overlay.Env, tuttiAgentExtraSkillRootsEnv)
		overlay.Env = withoutEnvironmentKey(overlay.Env, tuttiAgentStableSystemSkillsEnv)
	}
	return appServerPreparedLaunch{
		spec:           spec,
		profile:        &profile,
		overlay:        overlay,
		processCleanup: providerLaunchCleanup(spec, result.AppServer.ProcessCleanup),
		threadCleanup:  providerLaunchCleanup(spec, result.AppServer.ThreadCleanup),
	}, nil
}

func validateAppServerProcessProfile(profile AppServerProcessProfile) error {
	if len(profile.Command) == 0 {
		return errors.New("app-server process profile requires an explicit command")
	}
	if strings.TrimSpace(profile.ExecutionHostID) == "" ||
		strings.TrimSpace(profile.RuntimeGeneration) == "" ||
		strings.TrimSpace(profile.TransportScopeID) == "" ||
		strings.TrimSpace(profile.ProcessProfileDigest) == "" {
		return errors.New("app-server process profile requires execution host, runtime generation, transport scope, and profile digest")
	}
	if strings.TrimSpace(profile.CWD) == "" {
		return errors.New("app-server process profile requires an explicit cwd")
	}
	return nil
}
