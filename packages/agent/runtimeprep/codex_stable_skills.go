package runtimeprep

import (
	"fmt"
	"os"
	"path/filepath"
)

func cleanupCodexSessionSkillsForStableRoots(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect codex session skill root for stable-root migration: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(root); err != nil {
			return fmt.Errorf("remove linked codex session skill root: %w", err)
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("recreate codex session skill root: %w", err)
		}
		return nil
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("codex session skill root is not a directory: %s", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read codex session skills for stable-root migration: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect codex session skill for stable-root migration: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale codex session skill link: %w", err)
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		markerInfo, err := os.Lstat(filepath.Join(path, ".tutti-managed-skill"))
		if err != nil || !markerInfo.Mode().IsRegular() {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove legacy codex managed skill: %w", err)
		}
	}
	return nil
}
