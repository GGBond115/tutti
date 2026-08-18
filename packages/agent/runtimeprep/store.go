package runtimeprep

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	managedBlockBegin = "<!-- BEGIN TUTTI-RUNTIME (auto-managed; do not edit) -->"
	managedBlockEnd   = "<!-- END TUTTI-RUNTIME -->"
)

type LocalStore struct {
	StateDir string
}

func (s LocalStore) RuntimeRoot(_ string, agentSessionID string) (string, error) {
	stateDir := filepath.Clean(strings.TrimSpace(s.StateDir))
	if stateDir == "." || stateDir == string(filepath.Separator) {
		return "", errors.New("agent sidecar state directory is not configured")
	}
	if strings.TrimSpace(agentSessionID) == "" {
		return "", errors.New("agent sidecar runtime root requires session")
	}
	runtimeRoot := filepath.Join(
		stateDir,
		"agent",
		"runs",
		safePathSegment(agentSessionID),
	)
	if err := s.validateRuntimeRoot(runtimeRoot); err != nil {
		return "", err
	}
	return runtimeRoot, nil
}

func (s LocalStore) ProviderStateRoot(providerStateID string) (string, error) {
	stateDir := filepath.Clean(strings.TrimSpace(s.StateDir))
	if stateDir == "." || stateDir == string(filepath.Separator) {
		return "", errors.New("agent sidecar state directory is not configured")
	}
	if strings.TrimSpace(providerStateID) == "" {
		return "", errors.New("provider state root requires provider state ID")
	}
	root := filepath.Join(stateDir, "agent", "provider-state", safePathSegment(providerStateID))
	if err := s.validateProviderStateRoot(root); err != nil {
		return "", err
	}
	return root, nil
}

func (s LocalStore) EnsureProviderStateRoot(providerStateRoot string) error {
	providerStateRoot = filepath.Clean(strings.TrimSpace(providerStateRoot))
	if providerStateRoot == "." {
		return errors.New("agent provider state root is not configured")
	}
	if err := s.validateProviderStateRoot(providerStateRoot); err != nil {
		return err
	}
	stateDir := filepath.Clean(strings.TrimSpace(s.StateDir))
	base := filepath.Join(stateDir, "agent", "provider-state")
	if err := ensureProviderStateDirectoryTree(base, providerStateRoot); err != nil {
		return fmt.Errorf("create agent provider state root: %w", err)
	}
	return nil
}

// ensureProviderStateDirectoryTree creates the managed chain one component at
// a time. MkdirAll follows a symlinked parent, so it cannot establish that a
// provider-state root remains below the configured state directory.
func ensureProviderStateDirectoryTree(base, target string) error {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	if err := ensurePathWithin(base, filepath.Join(target, "_")); err != nil {
		return err
	}
	relative, err := filepath.Rel(base, target)
	if err != nil {
		return err
	}
	current := filepath.Dir(filepath.Dir(base)) // configured StateDir
	stateInfo, stateErr := os.Lstat(current)
	if stateErr == nil {
		if stateInfo.Mode()&os.ModeSymlink != 0 || !stateInfo.IsDir() {
			return fmt.Errorf("configured agent state path is not a regular directory: %s", current)
		}
	} else if os.IsNotExist(stateErr) {
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
	} else {
		return stateErr
	}
	parts := []string{"agent", "provider-state"}
	if relative != "." {
		parts = append(parts, strings.Split(relative, string(filepath.Separator))...)
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("managed provider state path is not a regular directory: %s", current)
			}
			continue
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func (s LocalStore) EnsureRuntimeRoot(runtimeRoot string) error {
	runtimeRoot = filepath.Clean(strings.TrimSpace(runtimeRoot))
	if runtimeRoot == "." {
		return errors.New("agent sidecar runtime root is not configured")
	}
	if err := s.validateRuntimeRoot(runtimeRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create agent sidecar runtime root: %w", err)
	}
	return nil
}

func (LocalStore) WriteManagedBlock(path string, content string) (ManagedBlockWriteResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ManagedBlockWriteResult{}, fmt.Errorf("managed block path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ManagedBlockWriteResult{}, fmt.Errorf("create managed block directory: %w", err)
	}
	existingBytes, err := os.ReadFile(path)
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return ManagedBlockWriteResult{}, fmt.Errorf("read managed block file: %w", err)
		}
		created = true
	}
	existing := string(existingBytes)
	next := replaceManagedBlock(existing, content)
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return ManagedBlockWriteResult{}, fmt.Errorf("write managed block file: %w", err)
	}
	return ManagedBlockWriteResult{Path: path, Created: created}, nil
}

func (s LocalStore) SaveManifest(runtimeRoot string, manifest *Manifest) error {
	if manifest == nil {
		return nil
	}
	runtimeRoot = filepath.Clean(strings.TrimSpace(runtimeRoot))
	if err := s.validateRuntimeRoot(runtimeRoot); err != nil {
		return err
	}
	manifest.UpdatedAtUnixMS = time.Now().UTC().UnixMilli()
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create sidecar manifest directory: %w", err)
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sidecar manifest: %w", err)
	}
	path := filepath.Join(runtimeRoot, SidecarManifestFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(content, '\n'), 0o644); err != nil {
		return fmt.Errorf("write sidecar manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit sidecar manifest: %w", err)
	}
	return nil
}

func (s LocalStore) CleanupRuntime(input StoreCleanupInput) error {
	agentSessionID := strings.TrimSpace(input.AgentSessionID)
	if agentSessionID == "" {
		return errors.New("agent sidecar cleanup requires session")
	}
	runtimeRoot := strings.TrimSpace(input.RuntimeRoot)
	if runtimeRoot == "" {
		var err error
		runtimeRoot, err = s.RuntimeRoot("", agentSessionID)
		if err != nil {
			return err
		}
	}
	return s.cleanupRuntimeRoot(filepath.Clean(runtimeRoot))
}

func (s LocalStore) cleanupRuntimeRoot(runtimeRoot string) error {
	runtimeRoot = filepath.Clean(runtimeRoot)
	if err := s.validateRuntimeRoot(runtimeRoot); err != nil {
		return err
	}

	var firstErr error
	manifest, err := s.loadManifest(runtimeRoot)
	if err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	if manifest != nil {
		for _, file := range manifest.ManagedFiles {
			switch strings.TrimSpace(file.Kind) {
			case "provider-instructions":
				if err := removeManagedBlock(file.Path, file.Created); err != nil && firstErr == nil {
					firstErr = err
				}
			case "provider-skill":
				if file.Created && pathWithin(manifest.Cwd, file.Path) {
					if err := os.RemoveAll(file.Path); err != nil && firstErr == nil {
						firstErr = fmt.Errorf("remove provider skill: %w", err)
					}
				}
			default:
				continue
			}
		}
	}
	if err := os.RemoveAll(runtimeRoot); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("remove agent sidecar runtime root: %w", err)
	}
	return firstErr
}

func (LocalStore) loadManifest(runtimeRoot string) (*Manifest, error) {
	content, err := os.ReadFile(filepath.Join(runtimeRoot, SidecarManifestFileName))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("decode sidecar manifest: %w", err)
	}
	return &manifest, nil
}

func (s LocalStore) validateRuntimeRoot(runtimeRoot string) error {
	stateDir := filepath.Clean(strings.TrimSpace(s.StateDir))
	if stateDir == "." || stateDir == string(filepath.Separator) {
		return errors.New("agent sidecar state directory is not configured")
	}
	runsRoot := filepath.Join(stateDir, "agent", "runs")
	rel, err := filepath.Rel(runsRoot, runtimeRoot)
	if err != nil {
		return fmt.Errorf("validate agent sidecar runtime root: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("agent sidecar runtime root is outside managed runs directory")
	}
	return nil
}

func (s LocalStore) validateProviderStateRoot(providerStateRoot string) error {
	stateDir := filepath.Clean(strings.TrimSpace(s.StateDir))
	if stateDir == "." || stateDir == string(filepath.Separator) {
		return errors.New("agent sidecar state directory is not configured")
	}
	providerStateBase := filepath.Join(stateDir, "agent", "provider-state")
	rel, err := filepath.Rel(providerStateBase, providerStateRoot)
	if err != nil {
		return fmt.Errorf("validate agent provider state root: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("agent provider state root is outside managed provider-state directory")
	}
	return nil
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	var out strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			out.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			out.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			out.WriteRune(ch)
		case ch == '-' || ch == '_' || ch == '.':
			out.WriteRune(ch)
		default:
			out.WriteRune('_')
		}
	}
	return out.String()
}

func replaceManagedBlock(existing string, content string) string {
	block := managedBlockBegin + "\n" + strings.TrimSpace(content) + "\n" + managedBlockEnd + "\n"
	begin := strings.Index(existing, managedBlockBegin)
	end := strings.Index(existing, managedBlockEnd)
	if begin >= 0 && end >= begin {
		end += len(managedBlockEnd)
		next := existing[:begin] + block + existing[end:]
		return normalizeManagedBlockSpacing(next)
	}
	if strings.TrimSpace(existing) == "" {
		return block
	}
	separator := "\n\n"
	if strings.HasSuffix(existing, "\n") {
		separator = "\n"
	}
	return existing + separator + block
}

func normalizeManagedBlockSpacing(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.TrimRight(value, "\n") + "\n"
}

func removeManagedBlock(path string, deleteIfEmpty bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read managed block file: %w", err)
	}
	next, removed := stripManagedBlock(string(contentBytes))
	if !removed {
		return nil
	}
	if strings.TrimSpace(next) == "" && deleteIfEmpty {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove managed block file: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("write managed block rollback: %w", err)
	}
	return nil
}

func stripManagedBlock(existing string) (string, bool) {
	begin := strings.Index(existing, managedBlockBegin)
	end := strings.Index(existing, managedBlockEnd)
	if begin < 0 || end < begin {
		return existing, false
	}
	end += len(managedBlockEnd)
	next := existing[:begin] + existing[end:]
	return normalizeManagedBlockSpacing(next), true
}

func pathWithin(root string, path string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	path = filepath.Clean(strings.TrimSpace(path))
	if root == "." || path == "." {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
