package runtimeprep

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// migrateLegacyCodexRollout imports only the one rollout identified by the
// durable provider session id. Legacy homes remain untouched so a retry can
// safely repeat the operation after a partial copy.
func migrateLegacyCodexRollout(ctx context.Context, input ProviderPrepareInput, targetHome string) error {
	providerSessionID := strings.TrimSpace(input.ProviderSessionID)
	if providerSessionID == "" {
		return nil
	}
	if _, _, _, err := findCodexRollout(ctx, targetHome, providerSessionID, true); err == nil {
		return nil
	} else if !errors.Is(err, errCodexRolloutNotFound) {
		return fmt.Errorf("inspect durable Codex rollout for provider session %q: %w", providerSessionID, err)
	}

	candidates, err := legacyCodexHomeCandidates(input.RuntimeRoot, input.LegacyCodexHomePath)
	if err != nil {
		return err
	}
	var sourcePath, relativePath string
	var sourceFingerprint codexRolloutFingerprint
	for _, candidate := range candidates {
		if filepath.Clean(candidate) == filepath.Clean(targetHome) {
			continue
		}
		path, rel, fingerprint, err := findCodexRollout(ctx, candidate, providerSessionID, false)
		if err != nil {
			if errors.Is(err, errCodexRolloutNotFound) {
				continue
			}
			return fmt.Errorf("inspect legacy Codex rollout in %s: %w", candidate, err)
		}
		if sourcePath != "" && filepath.Clean(sourcePath) != filepath.Clean(path) {
			return fmt.Errorf("multiple legacy Codex rollouts match provider session %q", providerSessionID)
		}
		sourcePath, relativePath, sourceFingerprint = path, rel, fingerprint
	}
	if sourcePath == "" {
		return nil
	}
	targetPath := filepath.Join(targetHome, filepath.FromSlash(filepath.ToSlash(relativePath)))
	if err := ensureDirectoryTreeWithoutSymlinks(targetHome, filepath.Dir(targetPath)); err != nil {
		return fmt.Errorf("prepare durable Codex rollout directory: %w", err)
	}
	if info, err := os.Lstat(targetPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("durable Codex rollout target is not a regular file: %s", targetPath)
		}
		matches, matchErr := codexRolloutMatches(targetPath, providerSessionID)
		if matchErr != nil {
			return fmt.Errorf("inspect durable Codex rollout target: %w", matchErr)
		}
		if matches {
			return nil
		}
		return fmt.Errorf("durable Codex rollout target conflicts with provider session %q", providerSessionID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect durable Codex rollout target: %w", err)
	}
	if err := copyRegularFileAtomically(sourcePath, targetPath, sourceFingerprint); err != nil {
		return fmt.Errorf("migrate Codex rollout for provider session %q: %w", providerSessionID, err)
	}
	return nil
}

func legacyCodexHomeCandidates(runtimeRoot, persistedHome string) ([]string, error) {
	runtimeRoot = filepath.Clean(strings.TrimSpace(runtimeRoot))
	if runtimeRoot == "." || runtimeRoot == string(filepath.Separator) {
		return nil, nil
	}
	stateDir := filepath.Dir(filepath.Dir(filepath.Dir(runtimeRoot)))
	if persistedHome = strings.TrimSpace(persistedHome); persistedHome != "" {
		persistedHome = filepath.Clean(persistedHome)
		if err := ensurePathWithin(stateDir, persistedHome); err != nil {
			return nil, fmt.Errorf("legacy Codex home is outside managed state: %w", err)
		}
		if err := validateManagedPathAncestorsWithoutSymlinks(stateDir, persistedHome); err != nil {
			return nil, fmt.Errorf("inspect persisted legacy Codex home path: %w", err)
		}
		if info, err := os.Stat(persistedHome); err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("legacy Codex home is not a directory: %s", persistedHome)
			}
			return []string{persistedHome}, nil
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect persisted legacy Codex home: %w", err)
		}
	}
	runsRoot := filepath.Join(stateDir, "agent", "runs")
	candidates := []string{
		filepath.Join(runtimeRoot, codexHomeDirectory),
		filepath.Join(stateDir, "agent", "codexHome"),
		filepath.Join(stateDir, "agent", "codex-home"),
	}
	entries, err := os.ReadDir(runsRoot)
	if err != nil {
		// Windows maps ENOTDIR to ERROR_PATH_NOT_FOUND, which also satisfies
		// errors.Is(err, fs.ErrNotExist). A regular file at runsRoot is a
		// malformed state root, not an absent directory, and must propagate.
		if !errors.Is(err, syscall.ENOTDIR) && errors.Is(err, fs.ErrNotExist) {
			return candidates, nil
		}
		return nil, fmt.Errorf("read legacy Codex run roots: %w", err)
	}
	for _, entry := range entries {
		// Legacy ordinary Sessions use their opaque Session id as the run
		// directory name. Provider-session matching below is the authority that
		// distinguishes a real migration candidate from unrelated run state;
		// do not narrow discovery to the synthetic app-server profile prefix.
		if !entry.IsDir() {
			continue
		}
		candidates = append(candidates, filepath.Join(runsRoot, entry.Name(), codexHomeDirectory))
	}
	for _, candidate := range candidates {
		if err := validateManagedPathAncestorsWithoutSymlinks(stateDir, candidate); err != nil {
			return nil, fmt.Errorf("inspect legacy Codex home path: %w", err)
		}
	}
	return candidates, nil
}

// validateManagedPathAncestorsWithoutSymlinks checks every existing component
// below a managed root without following symlinks. Missing leaves are allowed
// because migration candidates are intentionally sparse; an existing symlink
// ancestor is always a failed-closed source rather than a scan escape hatch.
func validateManagedPathAncestorsWithoutSymlinks(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if err := ensurePathWithin(root, filepath.Join(path, "_")); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	current := root
	segments := []string{"."}
	if relative != "." {
		segments = append(segments, strings.Split(relative, string(filepath.Separator))...)
	}
	for _, segment := range segments {
		if segment != "." {
			current = filepath.Join(current, segment)
		}
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed legacy Codex path is not a regular directory: %s", current)
		}
	}
	return nil
}
