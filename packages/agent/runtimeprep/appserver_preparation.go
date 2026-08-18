package runtimeprep

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const appServerProfileSessionPrefix = "appserver-profile-"

type appServerPreparedProfile struct {
	cleanupMu          sync.Mutex
	key                string
	digest             string
	syntheticSessionID string
	runtimeRoot        string
	cwd                string
	env                []string
	provider           string
	providerCleanup    func(context.Context) error
	skillsPrepared     bool
	references         int
	providerCleaned    bool
	rootCleaned        bool
}

type appServerPreparedSession struct {
	mu                       sync.Mutex
	workspaceID              string
	agentSessionID           string
	provider                 string
	profile                  *appServerPreparedProfile
	runtimeRoot              string
	providerCleanup          func(context.Context) error
	processTransferred       bool
	threadProviderCleaned    bool
	threadReleased           bool
	profileReferenceReleased bool
	ownsFinalProfileCleanup  bool
	processReleased          bool
}

type appServerProfileFingerprint struct {
	Scope                 AppServerProfileScope
	Provider              string
	AgentTargetID         string
	ProviderStateID       string
	CLICommand            string
	CodexSaverMode        bool
	ProviderTargetRef     map[string]any
	AgentSkills           []string
	AgentTools            []string
	ExtraSkills           []ProviderSkillBundle
	ConnectorRoutingHints []ConnectorRoutingHint
	Connector             *appServerConnectorFingerprint
	ExtensionSkillRoots   []string
	ModelEndpoint         *appServerModelEndpointFingerprint
}

type appServerConnectorFingerprint struct {
	CLIBinDir       string
	SkillRoots      []string
	RuntimeRevision uint64
}

type appServerModelEndpointFingerprint struct {
	PlanID              string
	Protocol            string
	BaseURL             string
	WireAPI             string
	Models              []ModelEndpointModel
	PlanUpdatedAtUnixMS int64
}

func (p *DefaultPreparer) appServerPreparationEnabled() bool {
	return p != nil && strings.TrimSpace(p.AppServerScope.ExecutionHostID) != "" &&
		strings.TrimSpace(p.AppServerScope.RuntimeGeneration) != "" &&
		strings.TrimSpace(p.AppServerScope.TransportScopeID) != ""
}

func (p *DefaultPreparer) acquireAppServerProfile(
	ctx context.Context,
	input PrepareInput,
	store RuntimeStore,
) (*appServerPreparedProfile, error) {
	key, err := p.appServerProfileKey(input)
	if err != nil {
		return nil, err
	}
	p.appServerMu.Lock()
	defer p.appServerMu.Unlock()
	if profile := p.appServerProfiles[key]; profile != nil {
		if profile.references <= 0 {
			if err := p.cleanupAppServerProfile(ctx, profile); err != nil {
				return nil, fmt.Errorf("retry retiring app-server process profile cleanup: %w", err)
			}
			if p.appServerProfiles[key] == profile {
				delete(p.appServerProfiles, key)
			}
		} else {
			if !input.SkipSkills && !profile.skillsPrepared {
				if err := p.upgradeAppServerProfileSkills(ctx, profile, input, store); err != nil {
					return nil, err
				}
			}
			profile.references++
			return profile, nil
		}
	}

	syntheticSessionID := appServerProfileSessionPrefix + key[:32]
	runtimeRoot, err := store.RuntimeRoot("appserver-profile", syntheticSessionID)
	if err != nil {
		return nil, err
	}
	if err := store.EnsureRuntimeRoot(runtimeRoot); err != nil {
		return nil, err
	}
	profileInput := appServerProcessPrepareInput(input, syntheticSessionID, p.StateDir)
	provider := p.provider(profileInput)
	result := ProviderPrepareResult{Cwd: profileInput.Cwd}
	if provider != nil {
		result, err = provider.Prepare(ctx, ProviderPrepareInput{
			PrepareInput: profileInput, RuntimeRoot: runtimeRoot, Store: store,
		})
		if err != nil {
			_ = store.CleanupRuntime(StoreCleanupInput{AgentSessionID: syntheticSessionID, RuntimeRoot: runtimeRoot})
			return nil, err
		}
	}
	processEnv := appServerStableProcessEnvironment(
		append(defaultRuntimeEnv(profileInput, p.StateDir), result.Env...),
		input.Provider,
	)
	if err := removeAppServerProfileInstructions(processEnv, input.Provider); err != nil {
		if result.Cleanup != nil {
			_ = result.Cleanup(ctx)
		}
		_ = store.CleanupRuntime(StoreCleanupInput{AgentSessionID: syntheticSessionID, RuntimeRoot: runtimeRoot})
		return nil, err
	}
	digest, err := appServerActualProfileDigest(runtimeRoot, result.Cwd, processEnv)
	if err != nil {
		if result.Cleanup != nil {
			_ = result.Cleanup(ctx)
		}
		_ = store.CleanupRuntime(StoreCleanupInput{AgentSessionID: syntheticSessionID, RuntimeRoot: runtimeRoot})
		return nil, err
	}
	profile := &appServerPreparedProfile{
		key: key, digest: digest, syntheticSessionID: syntheticSessionID,
		runtimeRoot: runtimeRoot, cwd: result.Cwd, env: processEnv,
		provider: strings.TrimSpace(input.Provider), providerCleanup: result.Cleanup,
		skillsPrepared: !input.SkipSkills, references: 1,
	}
	p.appServerProfiles[key] = profile
	return profile, nil
}

// upgradeAppServerProfileSkills completes the process-level part that a
// model-only probe intentionally omitted. The profile key stays unchanged so
// an already running compatible app-server remains the same physical process;
// the provider home is upgraded in place before the real Session starts.
func (p *DefaultPreparer) upgradeAppServerProfileSkills(
	ctx context.Context,
	profile *appServerPreparedProfile,
	input PrepareInput,
	store RuntimeStore,
) error {
	if profile == nil || profile.skillsPrepared {
		return nil
	}
	profileInput := appServerProcessPrepareInput(input, profile.syntheticSessionID, p.StateDir)
	profileInput.SkipSkills = false
	provider := p.provider(profileInput)
	if provider == nil {
		profile.skillsPrepared = true
		return nil
	}
	result, err := provider.Prepare(ctx, ProviderPrepareInput{
		PrepareInput: profileInput,
		RuntimeRoot:  profile.runtimeRoot,
		Store:        store,
	})
	if err != nil {
		return fmt.Errorf("upgrade app-server process profile skills: %w", err)
	}
	processEnv := appServerStableProcessEnvironment(
		append(defaultRuntimeEnv(profileInput, p.StateDir), result.Env...),
		profileInput.Provider,
	)
	if err := removeAppServerProfileInstructions(processEnv, profileInput.Provider); err != nil {
		if result.Cleanup != nil {
			_ = result.Cleanup(ctx)
		}
		return err
	}
	if result.Cleanup != nil {
		previousCleanup := profile.providerCleanup
		profile.providerCleanup = func(cleanupCtx context.Context) error {
			var cleanupErr error
			if previousCleanup != nil {
				cleanupErr = previousCleanup(cleanupCtx)
			}
			return errors.Join(cleanupErr, result.Cleanup(cleanupCtx))
		}
	}
	profile.skillsPrepared = true
	return nil
}

func (p *DefaultPreparer) appServerProfileKey(input PrepareInput) (string, error) {
	fingerprint := appServerProfileFingerprint{
		Scope: p.AppServerScope, Provider: strings.TrimSpace(input.Provider),
		AgentTargetID:   strings.TrimSpace(input.AgentTargetID),
		ProviderStateID: strings.TrimSpace(input.ProviderStateID), CLICommand: strings.TrimSpace(input.CLICommand),
		CodexSaverMode: input.CodexSaverMode, ProviderTargetRef: input.ProviderTargetRef,
		AgentSkills: input.AgentSkills, AgentTools: input.AgentTools,
		ExtraSkills: input.ExtraSkills, ConnectorRoutingHints: input.ConnectorRoutingHints,
		ExtensionSkillRoots: input.ExtensionSkillRoots,
	}
	if endpoint := input.ModelEndpoint; endpoint != nil {
		fingerprint.ModelEndpoint = &appServerModelEndpointFingerprint{
			PlanID: endpoint.PlanID, Protocol: endpoint.Protocol, BaseURL: endpoint.BaseURL,
			WireAPI: endpoint.WireAPI, Models: endpoint.Models,
			PlanUpdatedAtUnixMS: endpoint.PlanUpdatedAtUnixMS,
		}
	}
	if connector := input.Connector; connector != nil {
		fingerprint.Connector = &appServerConnectorFingerprint{
			CLIBinDir: connector.CLIBinDir, SkillRoots: append([]string(nil), connector.SkillRoots...),
			RuntimeRevision: connector.RuntimeRevision,
		}
	}
	raw, err := json.Marshal(fingerprint)
	if err != nil {
		return "", fmt.Errorf("encode app-server process profile identity: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func appServerProcessPrepareInput(input PrepareInput, syntheticSessionID, stateDir string) PrepareInput {
	input.WorkspaceID = "appserver-profile"
	input.AgentSessionID = syntheticSessionID
	input.Cwd = filepath.Clean(strings.TrimSpace(stateDir))
	input.ExternalRolloutSourcePath = ""
	input.MCPServers = nil
	input.AgentName = ""
	input.AgentDescription = ""
	input.AgentInstructions = ""
	input.ConversationDetailMode = ""
	input.Model = ""
	if input.ModelEndpoint != nil {
		endpoint := *input.ModelEndpoint
		endpoint.Model = ""
		input.ModelEndpoint = &endpoint
	}
	input.appServerProcessProfile = true
	if input.Connector != nil {
		connector := *input.Connector
		connector.MCPServers = nil
		input.Connector = &connector
	}
	return input
}

func appServerStableProcessEnvironment(env []string, provider string) []string {
	allowed := map[string]struct{}{"PATH": {}}
	if strings.TrimSpace(provider) == "tutti-agent" {
		allowed["TUTTI_AGENT_HOME"] = struct{}{}
		allowed["TUTTI_AGENT_EXTRA_SKILL_ROOTS_JSON"] = struct{}{}
		allowed["TUTTI_AGENT_STABLE_SYSTEM_SKILLS_ROOT"] = struct{}{}
	} else {
		allowed["CODEX_HOME"] = struct{}{}
	}
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, keep := allowed[strings.ToUpper(strings.TrimSpace(key))]; keep {
			result = append(result, entry)
		}
	}
	return result
}

func appServerThreadEnvironment(env []string) []string {
	// PATH belongs to the shared app-server process profile. Repeating the
	// machine/runtime-specific path in thread/start's shell overlay makes the
	// protocol payload non-portable and is redundant because shell children
	// inherit the process profile environment.
	return removeEnvironmentKeys(env, "CODEX_HOME", "TUTTI_AGENT_HOME", ModelPlanAPIKeyEnv, "PATH")
}

func appServerModelProviderCredentials(endpoint *ModelEndpointConfig) []AppServerModelProviderCredential {
	if !endpoint.supportsCodex() {
		return nil
	}
	return []AppServerModelProviderCredential{{
		ModelProviderID: ModelPlanProviderID,
		BearerToken:     endpoint.APIKey,
	}}
}

func removeEnvironmentKeys(env []string, keys ...string) []string {
	removed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		removed[strings.ToUpper(strings.TrimSpace(key))] = struct{}{}
	}
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, drop := removed[strings.ToUpper(strings.TrimSpace(key))]; drop {
				continue
			}
		}
		result = append(result, entry)
	}
	return result
}

func removeAppServerProfileInstructions(env []string, provider string) error {
	homeKey := "CODEX_HOME"
	if strings.TrimSpace(provider) == "tutti-agent" {
		homeKey = "TUTTI_AGENT_HOME"
	}
	home := appServerEnvironmentValue(env, homeKey)
	if home == "" {
		return fmt.Errorf("app-server process profile is missing %s", homeKey)
	}
	if err := os.Remove(filepath.Join(home, "AGENTS.md")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Session instructions from app-server process profile: %w", err)
	}
	return nil
}

func appServerActualProfileDigest(root, cwd string, env []string) (string, error) {
	hash := sha256.New()
	normalizedEnv := append([]string(nil), env...)
	sort.Strings(normalizedEnv)
	_, _ = hash.Write([]byte(strings.TrimSpace(cwd) + "\x00" + strings.Join(normalizedEnv, "\x00")))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte("\x01" + filepath.ToSlash(rel)))
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = hash.Write([]byte("\x02" + target))
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = hash.Write(content)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("digest app-server process profile: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func appServerEnvironmentValue(env []string, name string) string {
	for index := len(env) - 1; index >= 0; index-- {
		key, value, ok := strings.Cut(env[index], "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(name)) {
			return value
		}
	}
	return ""
}

func (p *DefaultPreparer) rememberAppServerSession(value *appServerPreparedSession) {
	if value == nil {
		return
	}
	key := providerCleanupKey(value.workspaceID, value.agentSessionID)
	p.appServerMu.Lock()
	p.appServerSessions[key] = value
	p.appServerMu.Unlock()
}

func (p *DefaultPreparer) AcquireAppServerLaunchLease(
	_ context.Context,
	input AppServerLaunchLeaseInput,
) (AppServerLaunchLease, error) {
	key := providerCleanupKey(strings.TrimSpace(input.WorkspaceID), strings.TrimSpace(input.AgentSessionID))
	p.appServerMu.Lock()
	session := p.appServerSessions[key]
	p.appServerMu.Unlock()
	if session == nil || session.provider != strings.TrimSpace(input.Provider) {
		return AppServerLaunchLease{}, errors.New("app-server launch has no prepared Session lease")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.threadReleased || session.processReleased {
		return AppServerLaunchLease{}, errors.New("app-server launch Session lease was already released")
	}
	if session.processTransferred {
		return AppServerLaunchLease{}, errors.New("app-server process lease was already transferred")
	}
	session.processTransferred = true
	return AppServerLaunchLease{
		ProcessCleanup: func(ctx context.Context) error { return p.releaseAppServerProcessLease(ctx, session) },
		ThreadCleanup:  func(ctx context.Context) error { return p.releaseAppServerThreadLease(ctx, session) },
	}, nil
}

func (p *DefaultPreparer) releasePreparedAppServerSession(ctx context.Context, workspaceID, agentSessionID string, forceProcess bool) (bool, error) {
	key := providerCleanupKey(strings.TrimSpace(workspaceID), strings.TrimSpace(agentSessionID))
	p.appServerMu.Lock()
	session := p.appServerSessions[key]
	p.appServerMu.Unlock()
	if session == nil {
		return false, nil
	}
	threadErr := p.releaseAppServerThreadLease(ctx, session)
	session.mu.Lock()
	transferred := session.processTransferred
	session.mu.Unlock()
	var processErr error
	if forceProcess || !transferred {
		processErr = p.releaseAppServerProcessLease(ctx, session)
	}
	return true, errors.Join(threadErr, processErr)
}

func (p *DefaultPreparer) releaseAppServerThreadLease(ctx context.Context, session *appServerPreparedSession) error {
	if session == nil {
		return nil
	}
	session.mu.Lock()
	if session.threadReleased {
		session.mu.Unlock()
		return nil
	}
	if !session.threadProviderCleaned && session.providerCleanup != nil {
		if err := session.providerCleanup(ctx); err != nil {
			session.mu.Unlock()
			return err
		}
		session.threadProviderCleaned = true
	}
	if err := p.runtimeStore().CleanupRuntime(StoreCleanupInput{
		WorkspaceID: session.workspaceID, AgentSessionID: session.agentSessionID, RuntimeRoot: session.runtimeRoot,
	}); err != nil {
		session.mu.Unlock()
		return err
	}
	session.threadReleased = true
	session.mu.Unlock()
	p.forgetReleasedAppServerSession(session)
	return nil
}

func (p *DefaultPreparer) releaseAppServerProcessLease(ctx context.Context, session *appServerPreparedSession) error {
	if session == nil {
		return nil
	}
	session.mu.Lock()
	if session.processReleased {
		session.mu.Unlock()
		return nil
	}
	if !session.profileReferenceReleased {
		p.appServerMu.Lock()
		if session.profile != nil && session.profile.references > 0 {
			session.profile.references--
			session.ownsFinalProfileCleanup = session.profile.references == 0
		}
		p.appServerMu.Unlock()
		session.profileReferenceReleased = true
	}
	if session.ownsFinalProfileCleanup {
		if err := p.cleanupAppServerProfile(ctx, session.profile); err != nil {
			session.mu.Unlock()
			return err
		}
		p.appServerMu.Lock()
		if session.profile != nil && session.profile.references == 0 &&
			p.appServerProfiles[session.profile.key] == session.profile {
			delete(p.appServerProfiles, session.profile.key)
		}
		p.appServerMu.Unlock()
	}
	session.processReleased = true
	session.mu.Unlock()
	p.forgetReleasedAppServerSession(session)
	return nil
}

func (p *DefaultPreparer) cleanupAppServerProfile(ctx context.Context, profile *appServerPreparedProfile) error {
	if profile == nil {
		return nil
	}
	profile.cleanupMu.Lock()
	defer profile.cleanupMu.Unlock()
	if !profile.providerCleaned {
		if profile.providerCleanup != nil {
			if err := profile.providerCleanup(ctx); err != nil {
				return err
			}
		}
		profile.providerCleaned = true
	}
	if profile.rootCleaned {
		return nil
	}
	if err := p.runtimeStore().CleanupRuntime(StoreCleanupInput{
		WorkspaceID: "appserver-profile", AgentSessionID: profile.syntheticSessionID, RuntimeRoot: profile.runtimeRoot,
	}); err != nil {
		return err
	}
	profile.rootCleaned = true
	return nil
}

func (p *DefaultPreparer) releaseAppServerProfileReference(ctx context.Context, key string) error {
	p.appServerMu.Lock()
	profile := p.appServerProfiles[key]
	if profile == nil {
		p.appServerMu.Unlock()
		return nil
	}
	profile.references--
	if profile.references > 0 {
		p.appServerMu.Unlock()
		return nil
	}
	p.appServerMu.Unlock()
	if err := p.cleanupAppServerProfile(ctx, profile); err != nil {
		return err
	}
	p.appServerMu.Lock()
	if profile.references == 0 && p.appServerProfiles[key] == profile {
		delete(p.appServerProfiles, key)
	}
	p.appServerMu.Unlock()
	return nil
}

func (p *DefaultPreparer) forgetReleasedAppServerSession(session *appServerPreparedSession) {
	if session == nil {
		return
	}
	session.mu.Lock()
	released := session.threadReleased && session.processReleased
	session.mu.Unlock()
	if !released {
		return
	}
	p.appServerMu.Lock()
	key := providerCleanupKey(session.workspaceID, session.agentSessionID)
	if p.appServerSessions[key] == session {
		delete(p.appServerSessions, key)
	}
	p.appServerMu.Unlock()
}
