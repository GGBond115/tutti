package runtimeprep

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTuttiAgentStableSkillsReuseRootAcrossSessions(t *testing.T) {
	stateDir := t.TempDir()
	storeRoot := filepath.Join(stateDir, "agent", "skill-bundles")
	preparer := TuttiAgentPreparer{
		ResolveAuthSource: func(context.Context, PrepareInput) (string, error) {
			return "", nil
		},
		StableSkillBundleRoot:       storeRoot,
		StableSystemSkillBundleRoot: filepath.Join(stateDir, "agent", "system-skill-bundles"),
	}
	wantRoot := ""
	for _, sessionID := range []string{"session-a", "session-b"} {
		runtimeRoot := filepath.Join(stateDir, "agent", "runs", sessionID)
		result, err := preparer.Prepare(t.Context(), ProviderPrepareInput{
			PrepareInput: testResolvedInput(t, PrepareInput{
				AgentSessionID: sessionID,
				AgentTargetID:  "local:tutti-agent",
				Provider:       "tutti-agent",
				CLICommand:     "tutti",
			}),
			RuntimeRoot: runtimeRoot,
			Store:       LocalStore{StateDir: stateDir},
		})
		if err != nil {
			t.Fatalf("Prepare(%s) error = %v", sessionID, err)
		}
		var roots []string
		if err := json.Unmarshal(
			[]byte(envValue(result.Env, "TUTTI_AGENT_EXTRA_SKILL_ROOTS_JSON")),
			&roots,
		); err != nil {
			t.Fatalf("Prepare(%s) extra roots = %#v: %v", sessionID, result.Env, err)
		}
		if len(roots) != 1 || !filepath.IsAbs(roots[0]) {
			t.Fatalf("Prepare(%s) roots = %#v, want one absolute root", sessionID, roots)
		}
		if root := envValue(result.Env, "TUTTI_AGENT_STABLE_SYSTEM_SKILLS_ROOT"); root != filepath.Join(stateDir, "agent", "system-skill-bundles") {
			t.Fatalf("Prepare(%s) stable system root = %q", sessionID, root)
		}
		if wantRoot == "" {
			wantRoot = roots[0]
		} else if roots[0] != wantRoot {
			t.Fatalf("stable roots differ: %q != %q", roots[0], wantRoot)
		}
		home := filepath.Join(runtimeRoot, "tutti-agent-home")
		if _, err := os.Stat(filepath.Join(home, "skills")); !os.IsNotExist(err) {
			t.Fatalf("Prepare(%s) created legacy skills root, err = %v", sessionID, err)
		}
		instructions, err := os.ReadFile(filepath.Join(home, "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(instructions), sessionID) {
			t.Fatalf("Prepare(%s) embedded session id in AGENTS.md", sessionID)
		}
	}
	if _, err := os.Stat(filepath.Join(wantRoot, tuttiSkillName, "SKILL.md")); err != nil {
		t.Fatalf("stable tutti-cli skill missing: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(wantRoot), "/agent/skill-bundles/v1/") {
		t.Fatalf("stable root = %q, want versioned content-addressed path", wantRoot)
	}
}

func TestCodexStableSkillsReuseRootAcrossSessionsAndReserveUserNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userSkill := filepath.Join(home, ".codex", "skills", "tutti-cli", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userSkill, []byte("---\nname: tutti-cli\n---\nuser-owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	preparer := CodexPreparer{
		StableSkillBundleRoot:       filepath.Join(stateDir, "agent", "skill-bundles"),
		StableSystemSkillBundleRoot: filepath.Join(stateDir, "agent", "system-skill-bundles"),
	}
	wantRoot := ""
	for _, sessionID := range []string{"session-a", "session-b"} {
		runtimeRoot := filepath.Join(stateDir, "agent", "runs", sessionID)
		result, err := preparer.Prepare(t.Context(), ProviderPrepareInput{
			PrepareInput: testResolvedInput(t, PrepareInput{
				AgentSessionID: sessionID,
				AgentTargetID:  "local:codex",
				Provider:       "codex",
				CLICommand:     "tutti",
			}),
			RuntimeRoot: runtimeRoot,
			Store:       LocalStore{StateDir: stateDir},
		})
		if err != nil {
			t.Fatalf("Prepare(%s) error = %v", sessionID, err)
		}
		var roots []string
		if err := json.Unmarshal(
			[]byte(envValue(result.Env, "TUTTI_AGENT_EXTRA_SKILL_ROOTS_JSON")),
			&roots,
		); err != nil {
			t.Fatalf("Prepare(%s) extra roots = %#v: %v", sessionID, result.Env, err)
		}
		if len(roots) != 1 || !filepath.IsAbs(roots[0]) {
			t.Fatalf("Prepare(%s) roots = %#v, want one absolute root", sessionID, roots)
		}
		if wantRoot == "" {
			wantRoot = roots[0]
		} else if roots[0] != wantRoot {
			t.Fatalf("stable roots differ: %q != %q", roots[0], wantRoot)
		}
		codexHome := filepath.Join(runtimeRoot, "codex-home")
		if _, err := os.Lstat(filepath.Join(codexHome, "skills", "tutti-cli")); err != nil {
			t.Fatalf("Prepare(%s) user skill missing: %v", sessionID, err)
		}
		if _, err := os.Stat(filepath.Join(codexHome, "skills", "tutti-cli-tutti")); !os.IsNotExist(err) {
			t.Fatalf("Prepare(%s) materialized managed skill in session home, err = %v", sessionID, err)
		}
		if root := envValue(result.Env, "TUTTI_AGENT_STABLE_SYSTEM_SKILLS_ROOT"); root != filepath.Join(stateDir, "agent", "system-skill-bundles") {
			t.Fatalf("Prepare(%s) stable system root = %q", sessionID, root)
		}
	}
	if _, err := os.Stat(filepath.Join(wantRoot, "tutti-cli-tutti", "SKILL.md")); err != nil {
		t.Fatalf("stable managed fallback skill missing: %v", err)
	}
}

func TestCodexStableSkillsMigrateLegacySessionHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := t.TempDir()
	runtimeRoot := filepath.Join(stateDir, "agent", "runs", "session-upgrade")
	input := ProviderPrepareInput{
		PrepareInput: testResolvedInput(t, PrepareInput{
			AgentSessionID: "session-upgrade",
			AgentTargetID:  "local:codex",
			Provider:       "codex",
			CLICommand:     "tutti",
		}),
		RuntimeRoot: runtimeRoot,
		Store:       LocalStore{StateDir: stateDir},
	}
	if _, err := (CodexPreparer{}).Prepare(t.Context(), input); err != nil {
		t.Fatalf("legacy Prepare() error = %v", err)
	}
	codexSkillRoot := filepath.Join(runtimeRoot, "codex-home", "skills")
	legacyManagedSkill := filepath.Join(codexSkillRoot, tuttiSkillName)
	if _, err := os.Stat(filepath.Join(legacyManagedSkill, ".tutti-managed-skill")); err != nil {
		t.Fatalf("legacy managed skill marker missing: %v", err)
	}
	staleTarget := filepath.Join(home, "removed-user-skill")
	staleLink := filepath.Join(codexSkillRoot, "stale-user-skill")
	if err := os.Symlink(staleTarget, staleLink); err != nil {
		t.Fatal(err)
	}

	stablePreparer := CodexPreparer{
		StableSkillBundleRoot:       filepath.Join(stateDir, "agent", "skill-bundles"),
		StableSystemSkillBundleRoot: filepath.Join(stateDir, "agent", "system-skill-bundles"),
	}
	result, err := stablePreparer.Prepare(t.Context(), input)
	if err != nil {
		t.Fatalf("stable Prepare() error = %v", err)
	}
	if _, err := os.Lstat(legacyManagedSkill); !os.IsNotExist(err) {
		t.Fatalf("legacy managed skill remains after migration, err = %v", err)
	}
	if _, err := os.Lstat(staleLink); !os.IsNotExist(err) {
		t.Fatalf("stale user skill link remains after migration, err = %v", err)
	}
	var roots []string
	if err := json.Unmarshal(
		[]byte(envValue(result.Env, "TUTTI_AGENT_EXTRA_SKILL_ROOTS_JSON")),
		&roots,
	); err != nil {
		t.Fatalf("stable extra roots = %#v: %v", result.Env, err)
	}
	if len(roots) != 1 {
		t.Fatalf("stable roots = %#v, want one root", roots)
	}
	if _, err := os.Stat(filepath.Join(roots[0], tuttiSkillName, "SKILL.md")); err != nil {
		t.Fatalf("stable managed skill did not reclaim canonical name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(roots[0], tuttiSkillName+"-tutti", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy managed skill incorrectly reserved fallback name, err = %v", err)
	}
}

func TestCodexStableSkillsMigrationDoesNotFollowSkillRootSymlink(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "codex-home", "skills")
	external := filepath.Join(t.TempDir(), "external-skills")
	externalManaged := filepath.Join(external, "external-managed")
	if err := os.MkdirAll(externalManaged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(externalManaged, ".tutti-managed-skill"),
		[]byte("external\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(externalManaged, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, root); err != nil {
		t.Fatal(err)
	}

	if err := cleanupCodexSessionSkillsForStableRoots(root); err != nil {
		t.Fatalf("cleanupCodexSessionSkillsForStableRoots() error = %v", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("recreated skill root mode = %v, want real directory", rootInfo.Mode())
	}
	content, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("external sentinel changed or removed: %v", err)
	}
	if string(content) != "preserve" {
		t.Fatalf("external sentinel = %q, want preserve", content)
	}
}

func TestStableSkillBundleDigestChangesWithContentNotMapOrder(t *testing.T) {
	specA := providerSkillSpec{
		baseName: "sample",
		skillID:  "example/sample",
		files: map[string]string{
			"SKILL.md":            "sample",
			"references/guide.md": "guide",
		},
	}
	specB := providerSkillSpec{
		baseName: "sample",
		skillID:  "example/sample",
		files: map[string]string{
			"references/guide.md": "guide",
			"SKILL.md":            "sample",
		},
	}
	first, _, err := canonicalizeStableProviderSkills("tutti-agent", []providerSkillSpec{specA})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := canonicalizeStableProviderSkills("tutti-agent", []providerSkillSpec{specB})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("canonical bundle depends on map order:\n%s\n%s", firstJSON, secondJSON)
	}
	specB.files["SKILL.md"] = "changed"
	changed, _, err := canonicalizeStableProviderSkills("tutti-agent", []providerSkillSpec{specB})
	if err != nil {
		t.Fatal(err)
	}
	changedJSON, _ := json.Marshal(changed)
	if string(firstJSON) == string(changedJSON) {
		t.Fatal("canonical bundle did not change with skill content")
	}
}

func TestStableSkillBundleConcurrentMaterialization(t *testing.T) {
	input := testResolvedInput(t, PrepareInput{
		AgentSessionID: "session-a",
		AgentTargetID:  "local:tutti-agent",
		Provider:       "tutti-agent",
		CLICommand:     "tutti",
	})
	storeRoot := filepath.Join(t.TempDir(), "skill-bundles")
	const workers = 8
	roots := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			root, err := materializeStableProviderSkills(storeRoot, input)
			if err != nil {
				errors <- err
				return
			}
			roots <- root
		}()
	}
	group.Wait()
	close(roots)
	close(errors)
	for err := range errors {
		t.Fatalf("materializeStableProviderSkills() error = %v", err)
	}
	want := ""
	for root := range roots {
		if want == "" {
			want = root
		} else if root != want {
			t.Fatalf("concurrent roots differ: %q != %q", root, want)
		}
	}
	if _, err := os.Stat(filepath.Join(want, tuttiSkillName, "SKILL.md")); err != nil {
		t.Fatalf("concurrent bundle is incomplete: %v", err)
	}
}

func TestStableSkillBundleRejectsEscapingSkillName(t *testing.T) {
	_, _, err := canonicalizeStableProviderSkills("tutti-agent", []providerSkillSpec{{
		baseName: "../escape",
		skillID:  "example/escape",
		files:    map[string]string{"SKILL.md": "escape"},
	}})
	if err == nil {
		t.Fatal("canonicalizeStableProviderSkills() error = nil, want escaping name rejection")
	}
}
