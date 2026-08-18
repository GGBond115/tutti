package runtimeprep

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type runtimeStoreWithoutProviderState struct{ RuntimeStore }

func TestPrepareProviderStateFailsClosedWithoutProviderStateStore(t *testing.T) {
	store := runtimeStoreWithoutProviderState{RuntimeStore: LocalStore{StateDir: t.TempDir()}}
	input := PrepareInput{Provider: "codex", AgentTargetID: "target-1"}
	if err := prepareProviderState(store, &input); err == nil {
		t.Fatal("prepareProviderState() succeeded without ProviderStateStore")
	} else if !strings.Contains(err.Error(), "provider-state store") {
		t.Fatalf("prepareProviderState() error = %v, want provider-state store failure", err)
	}
}

func TestStableProviderStateIDExcludesRuntimeAndModelIdentity(t *testing.T) {
	base := PrepareInput{
		Provider: "codex", AgentTargetID: "target-1",
		ProviderTargetRef: map[string]any{
			"accountAuthority": "account-a", "model": "gpt-5", "cwd": "/one",
			"runtimeGeneration": "generation-1", "sessionId": "session-a",
		},
	}
	first, err := StableProviderStateID(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Model = "gpt-5-mini"
	base.Cwd = "/two"
	base.AgentSessionID = "session-b"
	base.ProviderTargetRef["model"] = "gpt-5-mini"
	base.ProviderTargetRef["cwd"] = "/two"
	base.ProviderTargetRef["runtimeGeneration"] = "generation-2"
	base.ProviderTargetRef["sessionId"] = "session-b"
	second, err := StableProviderStateID(base)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("provider state IDs differ across volatile fields: %q != %q", first, second)
	}
	base.ProviderTargetRef["accountAuthority"] = "account-b"
	third, err := StableProviderStateID(base)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("provider account authority did not change provider state ID")
	}
}

func TestStableProviderStateIDUsesOpaqueAuthFingerprintWithoutCredentialMaterial(t *testing.T) {
	base := PrepareInput{
		Provider: "codex", AgentTargetID: "target-1", ProviderAuthFingerprint: "fingerprint-a",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a", "apiKey": "secret-a"},
	}
	first, err := StableProviderStateID(base)
	if err != nil {
		t.Fatal(err)
	}
	base.ProviderTargetRef["apiKey"] = "secret-b"
	second, err := StableProviderStateID(base)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("raw credential changed provider state ID: %q != %q", first, second)
	}
	base.ProviderAuthFingerprint = "fingerprint-b"
	third, err := StableProviderStateID(base)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("auth authority fingerprint did not separate provider state")
	}
}

func TestCodexPrepareSeparatesAuthAuthoritiesAcrossDurableHomeAndProcessProfile(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1",
	}
	prepare := func(sessionID, fingerprint, authority string) PreparedRuntime {
		t.Helper()
		prepared, err := preparer.Prepare(t.Context(), PrepareInput{
			WorkspaceID: "workspace-1", AgentSessionID: sessionID, AgentTargetID: "target-1",
			Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
			ProviderAuthFingerprint: fingerprint,
			ProviderTargetRef:       map[string]any{"accountAuthority": authority},
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.AppServer == nil {
			t.Fatal("Prepare() AppServer = nil")
		}
		return prepared
	}
	first := prepare("session-auth-a", "auth-fingerprint-a", "account-a")
	second := prepare("session-auth-b", "auth-fingerprint-b", "account-b")
	if first.AppServer.ProviderStateID == second.AppServer.ProviderStateID {
		t.Fatalf("auth authorities reused provider state ID %q", first.AppServer.ProviderStateID)
	}
	firstHome := appServerEnvironmentValue(first.AppServer.ProcessEnv, "CODEX_HOME")
	secondHome := appServerEnvironmentValue(second.AppServer.ProcessEnv, "CODEX_HOME")
	if firstHome == "" || secondHome == "" || firstHome == secondHome {
		t.Fatalf("auth authorities reused process CODEX_HOME: %q and %q", firstHome, secondHome)
	}
	if first.AppServer.ProcessProfileDigest == second.AppServer.ProcessProfileDigest {
		t.Fatalf("auth authorities reused process profile digest %q", first.AppServer.ProcessProfileDigest)
	}
	if _, err := os.Stat(firstHome); err != nil {
		t.Fatalf("first durable Codex home missing: %v", err)
	}
	if _, err := os.Stat(secondHome); err != nil {
		t.Fatalf("second durable Codex home missing: %v", err)
	}
}

func TestLocalStoreEnsureProviderStateRootRejectsParentSymlink(t *testing.T) {
	stateDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stateDir, "agent")); err != nil {
		t.Skipf("filesystem does not permit symlinks: %v", err)
	}
	store := LocalStore{StateDir: stateDir}
	root := filepath.Join(stateDir, "agent", "provider-state", "provider-state-test")
	if err := store.EnsureProviderStateRoot(root); err == nil {
		t.Fatal("EnsureProviderStateRoot followed symlinked agent parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "provider-state")); !os.IsNotExist(err) {
		t.Fatalf("symlink target was mutated, stat error=%v", err)
	}

	stateDir = t.TempDir()
	if err := os.Mkdir(filepath.Join(stateDir, "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stateDir, "agent", "provider-state")); err != nil {
		t.Skipf("filesystem does not permit symlinks: %v", err)
	}
	root = filepath.Join(stateDir, "agent", "provider-state", "provider-state-test")
	if err := (LocalStore{StateDir: stateDir}).EnsureProviderStateRoot(root); err == nil {
		t.Fatal("EnsureProviderStateRoot followed symlinked provider-state parent")
	}
}

func TestCodexProviderStateMigratesOneLegacyRolloutAndSurvivesLeaseCleanup(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1",
	}
	legacyRoot, err := (LocalStore{StateDir: stateDir}).RuntimeRoot("workspace-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	legacyRollout := filepath.Join(legacyRoot, codexHomeDirectory, "sessions", "2026", "08", "history.jsonl")
	writeTestCodexRollout(t, legacyRollout, "provider-session-1")
	if err := os.WriteFile(filepath.Join(legacyRoot, codexHomeDirectory, "unrelated.json"), []byte("do-not-copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "provider-session-1", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.AppServer == nil || strings.TrimSpace(prepared.AppServer.ProviderStateID) == "" {
		t.Fatalf("prepared app-server state = %#v", prepared.AppServer)
	}
	stateStore := LocalStore{StateDir: stateDir}
	stateRoot, err := stateStore.ProviderStateRoot(prepared.AppServer.ProviderStateID)
	if err != nil {
		t.Fatal(err)
	}
	stateRollout := filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "history.jsonl")
	if content, err := os.ReadFile(stateRollout); err != nil || len(content) == 0 {
		t.Fatalf("migrated rollout read error=%v bytes=%d", err, len(content))
	}
	if _, err := os.Stat(filepath.Join(stateRoot, codexHomeDirectory, "unrelated.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy unrelated file was copied, err=%v", err)
	}
	lease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateRollout); err != nil {
		t.Fatalf("provider state did not survive lease cleanup: %v", err)
	}
}

func TestCodexProviderStateScansLegacySessionRunsWithoutPersistedHome(t *testing.T) {
	for _, test := range []struct {
		name       string
		sessionIDs []string
		wantErr    string
	}{
		{name: "one historical session is migrated", sessionIDs: []string{"legacy-session-auto"}},
		{name: "multiple historical sessions fail closed", sessionIDs: []string{"legacy-session-a", "legacy-session-b"}, wantErr: "multiple legacy Codex rollouts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			store := LocalStore{StateDir: stateDir}
			setTestHome(t, t.TempDir())
			providerSessionID := "provider-session-auto"
			for _, sessionID := range test.sessionIDs {
				legacyRoot, err := store.RuntimeRoot("workspace-1", sessionID)
				if err != nil {
					t.Fatal(err)
				}
				rollout := filepath.Join(legacyRoot, codexHomeDirectory, "sessions", "2026", "08", sessionID+".jsonl")
				writeTestCodexRollout(t, rollout, providerSessionID)
			}
			stateRoot, err := store.ProviderStateRoot("provider-state-auto-scan")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.EnsureProviderStateRoot(stateRoot); err != nil {
				t.Fatal(err)
			}
			_, err = (CodexPreparer{}).Prepare(t.Context(), ProviderPrepareInput{
				PrepareInput: PrepareInput{
					Provider:          "codex",
					ProviderSessionID: providerSessionID,
					ProviderStateRoot: stateRoot,
					Cwd:               t.TempDir(),
					ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
				},
				RuntimeRoot: filepath.Join(stateDir, "agent", "runs", "staging-runtime"),
				Store:       store,
			})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("legacy scan error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			migrated := filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "legacy-session-auto.jsonl")
			if _, err := os.Stat(migrated); err != nil {
				t.Fatalf("unique legacy rollout was not migrated: %v", err)
			}
		})
	}
}

func TestCodexProviderStatePrefersPersistedLegacyHomeOverProfileScan(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1",
	}
	store := LocalStore{StateDir: stateDir}
	legacyRoot, err := store.RuntimeRoot("appserver-profile", "appserver-profile-old")
	if err != nil {
		t.Fatal(err)
	}
	persistedHome := filepath.Join(legacyRoot, codexHomeDirectory)
	persistedRollout := filepath.Join(persistedHome, "sessions", "2026", "08", "rollout-provider-session-2.jsonl")
	writeTestCodexRollout(t, persistedRollout, "provider-session-2")
	otherHome := filepath.Join(stateDir, "agent", "runs", "appserver-profile-other", codexHomeDirectory)
	writeTestCodexRollout(t, filepath.Join(otherHome, "sessions", "2026", "08", "rollout-provider-session-2.jsonl"), "provider-session-2")
	prepared, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-2", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "provider-session-2", LegacyCodexHomePath: persistedHome,
		Cwd: t.TempDir(), CLICommand: "tutti", ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := store.ProviderStateRoot(prepared.AppServer.ProviderStateID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "rollout-provider-session-2.jsonl")); err != nil {
		t.Fatalf("persisted legacy rollout was not migrated: %v", err)
	}
}

func TestLegacyCodexMigrationRejectsSymlinkedPersistedHomeAncestor(t *testing.T) {
	stateDir := t.TempDir()
	runtimeRoot, err := (LocalStore{StateDir: stateDir}).RuntimeRoot("workspace-1", "session-symlink")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(stateDir, "agent", "legacy-home-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("filesystem does not permit symlinks: %v", err)
	}
	_, err = legacyCodexHomeCandidates(runtimeRoot, filepath.Join(link, codexHomeDirectory))
	if err == nil || !strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "regular directory") {
		t.Fatalf("legacy symlink ancestor error = %v, want fail closed", err)
	}
}

func TestLegacyCodexMigrationFailsClosedOnMultipleSourceCandidates(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1"}
	store := LocalStore{StateDir: stateDir}
	runtimeRoot, err := store.RuntimeRoot("workspace-1", "session-legacy-multiple")
	if err != nil {
		t.Fatal(err)
	}
	writeTestCodexRollout(t, filepath.Join(runtimeRoot, codexHomeDirectory, "sessions", "2026", "08", "runtime.jsonl"), "provider-legacy-multiple")
	writeTestCodexRollout(t, filepath.Join(stateDir, "agent", "codexHome", "sessions", "2026", "08", "home.jsonl"), "provider-legacy-multiple")
	input := PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-legacy-multiple", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "provider-legacy-multiple", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	}
	stateID, err := StableProviderStateID(input)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := store.ProviderStateRoot(stateID)
	if err != nil {
		t.Fatal(err)
	}
	keepPath := filepath.Join(stateRoot, codexHomeDirectory, "keep.jsonl")
	keepBytes := []byte("pre-existing-target-bytes\n")
	if err := os.MkdirAll(filepath.Dir(keepPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepPath, keepBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = preparer.Prepare(t.Context(), input)
	if err == nil || !strings.Contains(err.Error(), "multiple legacy Codex rollouts") {
		t.Fatalf("multiple legacy source error = %v, want fail closed", err)
	}
	if got, readErr := os.ReadFile(keepPath); readErr != nil {
		t.Fatalf("read pre-existing target after failed migration: %v", readErr)
	} else if string(got) != string(keepBytes) {
		t.Fatalf("pre-existing target changed after multiple-source failure: %q", got)
	}
	for _, destination := range []string{
		filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "runtime.jsonl"),
		filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "home.jsonl"),
	} {
		if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
			t.Fatalf("legacy source was copied before multiple-source failure: %s, stat error=%v", destination, statErr)
		}
	}
	firstDestination := filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "runtime.jsonl")
	entries, err := os.ReadDir(filepath.Dir(firstDestination))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tutti-codex-fork-") {
			t.Fatalf("temporary migration file survived multiple-source failure: %s", entry.Name())
		}
	}
}

func TestCodexRolloutCopyFailureLeavesTargetAndTempClean(t *testing.T) {
	targetDir := t.TempDir()
	source := filepath.Join(t.TempDir(), "source.jsonl")
	target := filepath.Join(targetDir, "target.jsonl")
	writeTestCodexRollout(t, source, "provider-copy")
	if err := os.WriteFile(target, []byte("old-target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, fingerprint, err := inspectCodexRollout(source, "provider-copy")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Size++
	if err := copyRegularFileAtomically(source, target, fingerprint); err == nil {
		t.Fatal("copyRegularFileAtomically() succeeded with changed fingerprint")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old-target\n" {
		t.Fatalf("target changed after failed copy: %q", content)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tutti-codex-fork-") {
			t.Fatalf("temporary copy survived failed migration: %s", entry.Name())
		}
	}
}

func TestCodexProviderStateMigrationRetriesAfterPartialTarget(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1",
	}
	store := LocalStore{StateDir: stateDir}
	legacyRoot, err := store.RuntimeRoot("workspace-1", "session-partial")
	if err != nil {
		t.Fatal(err)
	}
	legacyRollout := filepath.Join(legacyRoot, codexHomeDirectory, "sessions", "2026", "08", "rollout-provider-partial.jsonl")
	writeTestCodexRollout(t, legacyRollout, "provider-partial")
	input := PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-partial", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "provider-partial", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	}
	stateID, err := StableProviderStateID(input)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := store.ProviderStateRoot(stateID)
	if err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "rollout-provider-partial.jsonl")
	if err := os.MkdirAll(filepath.Dir(partial), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, []byte(`{"type":"session_meta","payload":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preparer.Prepare(t.Context(), input); err == nil || !strings.Contains(err.Error(), "durable Codex rollout") {
		t.Fatalf("partial target error = %v, want durable rollout inspection failure", err)
	}
	if err := os.Remove(partial); err != nil {
		t.Fatal(err)
	}
	if _, err := preparer.Prepare(t.Context(), input); err != nil {
		t.Fatalf("retry after partial target failed: %v", err)
	}
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("retry did not install rollout: %v", err)
	}
}

func TestCodexProviderStateMigrationFailsClosedOnMultipleDurableRollouts(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1"}
	input := PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-multiple", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "provider-multiple", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	}
	stateID, err := StableProviderStateID(input)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := (LocalStore{StateDir: stateDir}).ProviderStateRoot(stateID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.jsonl", "two.jsonl"} {
		writeTestCodexRollout(t, filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", name), "provider-multiple")
	}
	input.ProviderStateID = stateID
	if _, err := preparer.Prepare(t.Context(), input); err == nil || !strings.Contains(err.Error(), "multiple Codex rollouts") {
		t.Fatalf("multiple durable rollout error = %v, want fail closed", err)
	}
}

func TestLegacyCodexRunRootReadDirPropagatesNonNotExistError(t *testing.T) {
	stateDir := t.TempDir()
	runsRoot := filepath.Join(stateDir, "agent", "runs")
	if err := os.MkdirAll(filepath.Dir(runsRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runsRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(runsRoot, "session")
	if _, err := legacyCodexHomeCandidates(runtimeRoot, ""); err == nil {
		t.Fatal("legacy run root ReadDir error was swallowed")
	}
}

func TestCodexSaverMaterialIsProfileOwnedAndDoesNotPolluteDurableHome(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1"}
	base := PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-saver", AgentTargetID: "target-1",
		Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	}
	saver := base
	saver.CodexSaverMode = true
	preparedSaver, err := preparer.Prepare(t.Context(), saver)
	if err != nil {
		t.Fatal(err)
	}
	nonSaver := base
	nonSaver.AgentSessionID = "session-nonsaver"
	preparedNonSaver, err := preparer.Prepare(t.Context(), nonSaver)
	if err != nil {
		t.Fatal(err)
	}
	if preparedSaver.AppServer.ProviderStateID != preparedNonSaver.AppServer.ProviderStateID {
		t.Fatalf("sessions with one provider/account did not share state ID: saver=%q non-saver=%q", preparedSaver.AppServer.ProviderStateID, preparedNonSaver.AppServer.ProviderStateID)
	}
	if preparedSaver.AppServer.ProcessProfileDigest == preparedNonSaver.AppServer.ProcessProfileDigest {
		t.Fatal("saver and non-saver reused one process profile digest")
	}
	saverHome := appServerEnvironmentValue(preparedSaver.AppServer.ProcessEnv, "CODEX_HOME")
	nonSaverHome := appServerEnvironmentValue(preparedNonSaver.AppServer.ProcessEnv, "CODEX_HOME")
	if saverHome == "" || saverHome != nonSaverHome || !strings.Contains(saverHome, filepath.Join("agent", "provider-state")) {
		t.Fatalf("saver/non-saver process CODEX_HOME=%q/%q, want one canonical durable home isolated from profile roots", saverHome, nonSaverHome)
	}
	stateRoot, err := (LocalStore{StateDir: stateDir}).ProviderStateRoot(preparedSaver.AppServer.ProviderStateID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, codexHomeDirectory, "agents", "luna_worker.toml")); !os.IsNotExist(err) {
		t.Fatalf("saver role leaked into durable Home, stat error=%v", err)
	}
	config, err := os.ReadFile(filepath.Join(stateRoot, codexHomeDirectory, "config.toml"))
	if err == nil && strings.Contains(string(config), "[agents.default]") {
		t.Fatal("saver default role leaked into durable Home")
	}
	var saverProfile, nonSaverProfile *appServerPreparedProfile
	for _, profile := range preparer.appServerProfiles {
		switch profile.digest {
		case preparedSaver.AppServer.ProcessProfileDigest:
			saverProfile = profile
		case preparedNonSaver.AppServer.ProcessProfileDigest:
			nonSaverProfile = profile
		}
	}
	if saverProfile == nil || nonSaverProfile == nil {
		t.Fatalf("could not resolve saver/non-saver process profiles: saver=%#v non-saver=%#v", saverProfile, nonSaverProfile)
	}
	saverRole := filepath.Join(saverProfile.runtimeRoot, "codex-saver-profile", "agents", "luna_worker.toml")
	if _, err := os.Stat(saverRole); err != nil {
		t.Fatalf("saver role was not written to its exact profile root: %v", err)
	}
	nonSaverRole := filepath.Join(nonSaverProfile.runtimeRoot, "codex-saver-profile", "agents", "luna_worker.toml")
	if _, err := os.Stat(nonSaverRole); !os.IsNotExist(err) {
		t.Fatalf("saver role leaked into non-saver profile: %v", err)
	}
}

func TestCodexProviderStateResumesInNewProcess(t *testing.T) {
	const childKey = "TUTTI_CODEX_PROVIDER_STATE_NEW_PROCESS_CHILD"
	if os.Getenv(childKey) == "1" {
		stateDir := os.Getenv("TUTTI_CODEX_PROVIDER_STATE_NEW_PROCESS_DIR")
		preparer := newTestPreparer(stateDir)
		preparer.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-2", TransportScopeID: "transport-2"}
		prepared, err := preparer.Prepare(t.Context(), PrepareInput{
			WorkspaceID: "workspace-1", AgentSessionID: "session-child", AgentTargetID: "target-1",
			Provider: "codex", ProviderSessionID: "provider-new-process", Cwd: t.TempDir(), CLICommand: "tutti",
			ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
		})
		if err != nil {
			t.Fatalf("child process resume: %v", err)
		}
		marker := prepared.AppServer.ProviderStateID + "\n" + prepared.AppServer.RuntimeGeneration + "\n"
		if err := os.WriteFile(filepath.Join(stateDir, "new-process-marker"), []byte(marker), 0o600); err != nil {
			t.Fatalf("write child process marker: %v", err)
		}
		return
	}

	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1"}
	prepared, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-parent", AgentTargetID: "target-1",
		Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti", ImportedSession: true,
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := (LocalStore{StateDir: stateDir}).ProviderStateRoot(prepared.AppServer.ProviderStateID)
	if err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "new-process.jsonl")
	writeTestCodexRollout(t, rollout, "provider-new-process")

	child := exec.Command(os.Args[0], "-test.run", "^TestCodexProviderStateResumesInNewProcess$", "-test.v")
	child.Env = append(os.Environ(), childKey+"=1", "TUTTI_CODEX_PROVIDER_STATE_NEW_PROCESS_DIR="+stateDir)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("new process resume error=%v output=%s", err, output)
	}
	marker, err := os.ReadFile(filepath.Join(stateDir, "new-process-marker"))
	if err != nil {
		t.Fatalf("read new process marker: %v", err)
	}
	values := strings.Split(strings.TrimSpace(string(marker)), "\n")
	if len(values) != 2 || values[0] != prepared.AppServer.ProviderStateID || values[1] != "generation-2" {
		t.Fatalf("new process provider state marker=%q, want state %q generation-2", values, prepared.AppServer.ProviderStateID)
	}
	if _, err := os.Stat(rollout); err != nil {
		t.Fatalf("durable rollout missing after new process resume: %v", err)
	}
}

func TestCodexProviderStateSurvivesNewPreparerGenerationAndSharesHome(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	store := LocalStore{StateDir: stateDir}
	first := newTestPreparer(stateDir)
	first.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1"}
	base := PrepareInput{
		WorkspaceID: "workspace-1", AgentTargetID: "target-1", Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"}, ImportedSession: true,
	}
	base.AgentSessionID = "session-generation-1"
	firstPrepared, err := first.Prepare(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := store.ProviderStateRoot(firstPrepared.AppServer.ProviderStateID)
	if err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(stateRoot, codexHomeDirectory, "sessions", "2026", "08", "new-process.jsonl")
	writeTestCodexRollout(t, rollout, "provider-new-process")
	lease, err := first.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: base.AgentSessionID, Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}

	second := newTestPreparer(stateDir)
	second.AppServerScope = AppServerProfileScope{ExecutionHostID: "host-1", RuntimeGeneration: "generation-2", TransportScopeID: "transport-2"}
	base.AgentSessionID = "session-generation-2"
	base.ProviderSessionID = "provider-new-process"
	base.ImportedSession = false
	secondPrepared, err := second.Prepare(t.Context(), base)
	if err != nil {
		t.Fatalf("new generation resume error = %v", err)
	}
	if firstPrepared.AppServer.ProviderStateID != secondPrepared.AppServer.ProviderStateID {
		t.Fatalf("provider state changed across generation: first=%q second=%q", firstPrepared.AppServer.ProviderStateID, secondPrepared.AppServer.ProviderStateID)
	}
	if firstPrepared.AppServer.RuntimeGeneration == secondPrepared.AppServer.RuntimeGeneration {
		t.Fatal("prepared process profile did not observe generation change")
	}
	if _, err := os.Stat(rollout); err != nil {
		t.Fatalf("shared durable rollout missing after new generation: %v", err)
	}
}

func TestCodexOrdinaryProviderStateMissingRolloutFails(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1",
	}
	_, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "missing-provider-session", Cwd: t.TempDir(), CLICommand: "tutti",
		ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	})
	if err == nil || !strings.Contains(err.Error(), "ordinary Codex runtime cannot resume") {
		t.Fatalf("missing ordinary rollout error = %v", err)
	}
}

func TestCodexImportedProviderStateKeepsRecreatePreparationPath(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "host-1", RuntimeGeneration: "generation-1", TransportScopeID: "transport-1",
	}
	prepared, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "imported-session", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "imported-provider", ImportedSession: true,
		Cwd: t.TempDir(), CLICommand: "tutti", ProviderTargetRef: map[string]any{"accountAuthority": "account-a"},
	})
	if err != nil {
		t.Fatalf("imported preparation error = %v", err)
	}
	if prepared.AppServer == nil || prepared.AppServer.ProviderStateID == "" {
		t.Fatalf("imported app-server preparation = %#v", prepared.AppServer)
	}
}

func TestCodexIsolatedOrdinaryProviderStateMissingRolloutFails(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	_, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "isolated-session", AgentTargetID: "target-1",
		Provider: "codex", ProviderSessionID: "missing-isolated", Cwd: t.TempDir(), CLICommand: "tutti",
	})
	if err == nil || !strings.Contains(err.Error(), "ordinary Codex runtime cannot resume") {
		t.Fatalf("isolated missing rollout error = %v", err)
	}
}
