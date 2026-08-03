package agentruntime

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

const (
	stableSystemSkillsSchemaVersion  = 1
	stableSystemSkillsMaxFiles       = 4096
	stableSystemSkillsMaxBytes       = 32 << 20
	tuttiAgentSystemSkillsMarkerFile = ".tutti-agent-system-skills.marker"
	codexSystemSkillsMarkerFile      = ".codex-system-skills.marker"
	// Retained for focused tutti-agent tests and fixtures.
	systemSkillsMarkerFile = tuttiAgentSystemSkillsMarkerFile
)

type appServerSystemSkillsLayout struct {
	homeEnv      string
	markerFile   string
	digestPrefix string
}

var (
	tuttiAgentSystemSkillsLayout = appServerSystemSkillsLayout{
		homeEnv:      "TUTTI_AGENT_HOME",
		markerFile:   tuttiAgentSystemSkillsMarkerFile,
		digestPrefix: "tutti-agent-system-skills-v1\x00",
	}
	codexSystemSkillsLayout = appServerSystemSkillsLayout{
		homeEnv:      "CODEX_HOME",
		markerFile:   codexSystemSkillsMarkerFile,
		digestPrefix: "codex-system-skills-v1\x00",
	}
)

type stableSystemSkillFile struct {
	relativePath string
	content      []byte
}

type stableSystemSkillsSnapshot struct {
	digest      string
	directories []string
	files       []stableSystemSkillFile
}

func (a *CodexAppServerAdapter) stabilizeSystemSkillPaths(
	session Session,
	storeRoot string,
	trace *codexAppServerStartupTrace,
) error {
	if strings.TrimSpace(storeRoot) == "" {
		return nil
	}
	layout, err := systemSkillsLayoutForStrategy(a.config.skillRootsStrategy)
	if err != nil {
		return err
	}
	home, found := lastEnvironmentValue(session.Env, layout.homeEnv)
	if !found {
		return fmt.Errorf("stabilize app-server system skills: %s is missing", layout.homeEnv)
	}
	home = filepath.Clean(strings.TrimSpace(home))
	if home == "." || !filepath.IsAbs(home) {
		return fmt.Errorf("stabilize app-server system skills: %s must be absolute", layout.homeEnv)
	}
	trace.Log("skills.system_paths.stabilize.begin", nil)
	target, digest, err := stabilizeAppServerSystemSkills(home, storeRoot, layout)
	if err != nil {
		return fmt.Errorf("stabilize app-server system skills: %w", err)
	}
	trace.Log("skills.system_paths.stabilize.succeeded", map[string]any{
		"fingerprint": digest[:12],
		"target_set":  strings.TrimSpace(target) != "",
	})
	return nil
}

func systemSkillsLayoutForStrategy(
	strategy providerregistry.AppServerSkillRootsStrategy,
) (appServerSystemSkillsLayout, error) {
	switch strategy {
	case providerregistry.AppServerSkillRootsStrategyTuttiStable:
		return tuttiAgentSystemSkillsLayout, nil
	case providerregistry.AppServerSkillRootsStrategyCodexStable:
		return codexSystemSkillsLayout, nil
	default:
		return appServerSystemSkillsLayout{}, fmt.Errorf(
			"app-server skill roots strategy %q has no stable system skill layout",
			strategy,
		)
	}
}

func stabilizeTuttiAgentSystemSkills(home string, storeRoot string) (string, string, error) {
	return stabilizeAppServerSystemSkills(home, storeRoot, tuttiAgentSystemSkillsLayout)
}

func stabilizeCodexSystemSkills(home string, storeRoot string) (string, string, error) {
	return stabilizeAppServerSystemSkills(home, storeRoot, codexSystemSkillsLayout)
}

func stabilizeAppServerSystemSkills(
	home string,
	storeRoot string,
	layout appServerSystemSkillsLayout,
) (string, string, error) {
	home = filepath.Clean(strings.TrimSpace(home))
	storeRoot = filepath.Clean(strings.TrimSpace(storeRoot))
	if home == "." || !filepath.IsAbs(home) {
		return "", "", errors.New("app-server home must be absolute")
	}
	if storeRoot == "." || !filepath.IsAbs(storeRoot) {
		return "", "", errors.New("stable system skill store must be absolute")
	}
	systemRoot := filepath.Join(home, "skills", ".system")
	info, err := os.Lstat(systemRoot)
	if err != nil {
		return "", "", fmt.Errorf("inspect provider system skills: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, digest, err := validateStableSystemSkillSymlink(systemRoot, storeRoot, layout)
		return target, digest, err
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("provider system skill root is not a directory: %s", systemRoot)
	}

	snapshot, err := snapshotStableSystemSkills(systemRoot, layout)
	if err != nil {
		return "", "", err
	}
	versionRoot := filepath.Join(storeRoot, fmt.Sprintf("v%d", stableSystemSkillsSchemaVersion))
	bundleRoot := filepath.Join(versionRoot, snapshot.digest)
	target := filepath.Join(bundleRoot, ".system")
	if err := validateStableSystemSkillTarget(target, snapshot.digest, layout); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		if err := materializeStableSystemSkillTarget(versionRoot, bundleRoot, snapshot, layout); err != nil {
			return "", "", err
		}
	}
	if err := replaceSystemSkillRootWithSymlink(systemRoot, target); err != nil {
		return "", "", err
	}
	return target, snapshot.digest, nil
}

func snapshotStableSystemSkills(root string, layout appServerSystemSkillsLayout) (stableSystemSkillsSnapshot, error) {
	root = filepath.Clean(root)
	directories := make([]string, 0)
	files := make([]stableSystemSkillFile, 0)
	totalBytes := 0
	hasMarker := false
	hasSkill := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("provider system skills contain symlink: %s", relative)
		}
		if entry.IsDir() {
			if relative != "." {
				directories = append(directories, relative)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("provider system skills contain non-regular file: %s", relative)
		}
		if len(files) >= stableSystemSkillsMaxFiles {
			return errors.New("provider system skills exceed file limit")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		totalBytes += len(content)
		if totalBytes > stableSystemSkillsMaxBytes {
			return errors.New("provider system skills exceed byte limit")
		}
		hasMarker = hasMarker || relative == layout.markerFile
		hasSkill = hasSkill || filepath.Base(relative) == "SKILL.md"
		files = append(files, stableSystemSkillFile{relativePath: relative, content: content})
		return nil
	})
	if err != nil {
		return stableSystemSkillsSnapshot{}, fmt.Errorf("snapshot provider system skills: %w", err)
	}
	if !hasMarker || !hasSkill {
		return stableSystemSkillsSnapshot{}, errors.New("provider system skills are incomplete")
	}
	sort.Strings(directories)
	sort.Slice(files, func(left, right int) bool {
		return files[left].relativePath < files[right].relativePath
	})
	digest := sha256.New()
	_, _ = digest.Write([]byte(layout.digestPrefix))
	for _, directory := range directories {
		writeStableSystemSkillDigestPart(digest, 'd', directory, nil)
	}
	for _, file := range files {
		writeStableSystemSkillDigestPart(digest, 'f', file.relativePath, file.content)
	}
	return stableSystemSkillsSnapshot{
		digest:      hex.EncodeToString(digest.Sum(nil)),
		directories: directories,
		files:       files,
	}, nil
}

func writeStableSystemSkillDigestPart(
	digest interface{ Write([]byte) (int, error) },
	kind byte,
	path string,
	content []byte,
) {
	length := make([]byte, 8)
	_, _ = digest.Write([]byte{kind})
	binary.BigEndian.PutUint64(length, uint64(len(path)))
	_, _ = digest.Write(length)
	_, _ = digest.Write([]byte(path))
	binary.BigEndian.PutUint64(length, uint64(len(content)))
	_, _ = digest.Write(length)
	_, _ = digest.Write(content)
}

func materializeStableSystemSkillTarget(
	versionRoot string,
	bundleRoot string,
	snapshot stableSystemSkillsSnapshot,
	layout appServerSystemSkillsLayout,
) error {
	if err := os.MkdirAll(versionRoot, 0o755); err != nil {
		return fmt.Errorf("create stable system skill store: %w", err)
	}
	temporaryRoot, err := os.MkdirTemp(versionRoot, ".tmp-"+snapshot.digest+"-")
	if err != nil {
		return fmt.Errorf("create stable system skill staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryRoot) }()
	temporarySystemRoot := filepath.Join(temporaryRoot, ".system")
	if err := os.MkdirAll(temporarySystemRoot, 0o755); err != nil {
		return err
	}
	for _, directory := range snapshot.directories {
		if err := os.MkdirAll(filepath.Join(temporarySystemRoot, filepath.FromSlash(directory)), 0o755); err != nil {
			return err
		}
	}
	for _, file := range snapshot.files {
		target := filepath.Join(temporarySystemRoot, filepath.FromSlash(file.relativePath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, file.content, 0o644); err != nil {
			return err
		}
	}
	if err := validateStableSystemSkillTarget(temporarySystemRoot, snapshot.digest, layout); err != nil {
		return fmt.Errorf("validate staged system skills: %w", err)
	}
	if err := os.Rename(temporaryRoot, bundleRoot); err != nil {
		if validationErr := validateStableSystemSkillTarget(
			filepath.Join(bundleRoot, ".system"),
			snapshot.digest,
			layout,
		); validationErr == nil {
			return nil
		}
		return fmt.Errorf("commit stable system skills: %w", err)
	}
	return nil
}

func validateStableSystemSkillTarget(
	target string,
	wantDigest string,
	layout appServerSystemSkillsLayout,
) error {
	snapshot, err := snapshotStableSystemSkills(target, layout)
	if err != nil {
		return err
	}
	if snapshot.digest != wantDigest {
		return errors.New("stable system skill bundle does not match digest")
	}
	return nil
}

func validateStableSystemSkillSymlink(
	systemRoot string,
	storeRoot string,
	layout appServerSystemSkillsLayout,
) (string, string, error) {
	target, err := filepath.EvalSymlinks(systemRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve stable system skill symlink: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", "", err
	}
	versionRoot := filepath.Join(storeRoot, fmt.Sprintf("v%d", stableSystemSkillsSchemaVersion))
	versionRoot, err = filepath.EvalSymlinks(versionRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve stable system skill store: %w", err)
	}
	relative, err := filepath.Rel(versionRoot, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", errors.New("provider system skill symlink escapes stable store")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || len(parts[0]) != sha256.Size*2 || parts[1] != ".system" {
		return "", "", errors.New("provider system skill symlink has invalid stable target")
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return "", "", errors.New("provider system skill symlink has invalid digest")
	}
	if err := validateStableSystemSkillTarget(target, parts[0], layout); err != nil {
		return "", "", err
	}
	return target, parts[0], nil
}

func replaceSystemSkillRootWithSymlink(systemRoot string, target string) error {
	return replaceSystemSkillRootWithSymlinkUsingRename(systemRoot, target, os.Rename)
}

func replaceSystemSkillRootWithSymlinkUsingRename(
	systemRoot string,
	target string,
	rename func(string, string) error,
) error {
	parent := filepath.Dir(systemRoot)
	staging, err := os.MkdirTemp(parent, ".system-stabilize-")
	if err != nil {
		return fmt.Errorf("create system skill replacement staging directory: %w", err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	replacement := filepath.Join(staging, "replacement")
	backup := filepath.Join(staging, "original")
	if err := os.Symlink(target, replacement); err != nil {
		return fmt.Errorf("create stable system skill symlink: %w", err)
	}
	if err := rename(systemRoot, backup); err != nil {
		return fmt.Errorf("stage provider system skills for replacement: %w", err)
	}
	if err := rename(replacement, systemRoot); err != nil {
		restoreErr := rename(backup, systemRoot)
		if restoreErr != nil {
			removeStaging = false
			return errors.Join(
				fmt.Errorf("activate stable system skill symlink: %w", err),
				fmt.Errorf("restore original system skills; backup preserved at %s: %w", backup, restoreErr),
			)
		}
		return fmt.Errorf("activate stable system skill symlink: %w", err)
	}
	return nil
}
