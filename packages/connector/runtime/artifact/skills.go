package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
	"gopkg.in/yaml.v3"
)

const (
	connectorSkillEntryName    = "SKILL.md"
	connectorSkillMaxDepth     = 8
	connectorSkillMaxCount     = contracts.ConnectorSkillMaxCount
	connectorSkillMaxEntrySize = 512 * 1024
)

// SkillProjection is produced once while a Connector route is activated.
// Root is empty when the release has no optional skills directory.
type SkillProjection struct {
	Root   string
	Skills []contracts.ConnectorSkillSummary
}

// InspectSkills validates the complete optional skills tree under an installed
// Connector release. Callers can safely retain the returned immutable
// projection instead of rescanning mutable filesystem state during discovery.
func InspectSkills(installedRoot string) (SkillProjection, error) {
	root := strings.TrimSpace(installedRoot)
	if root == "" || !filepath.IsAbs(root) {
		return SkillProjection{}, errors.New("connector installed root must be absolute")
	}
	skillRoot := filepath.Join(filepath.Clean(root), "skills")
	info, err := os.Lstat(skillRoot)
	if os.IsNotExist(err) {
		return SkillProjection{Skills: []contracts.ConnectorSkillSummary{}}, nil
	}
	if err != nil {
		return SkillProjection{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return SkillProjection{}, errors.New("connector skills root must be a directory, not a symlink")
	}

	skills := make([]contracts.ConnectorSkillSummary, 0)
	seen := make(map[string]struct{})
	skillCount := 0
	projectionBytes := 0
	err = filepath.WalkDir(skillRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == skillRoot {
			return nil
		}
		relative, err := filepath.Rel(skillRoot, path)
		if err != nil {
			return err
		}
		depth := strings.Count(filepath.ToSlash(relative), "/") + 1
		if depth > connectorSkillMaxDepth {
			return fmt.Errorf("connector Skill tree path %q exceeds depth %d", filepath.ToSlash(relative), connectorSkillMaxDepth)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("connector Skill tree contains symlink %q", filepath.ToSlash(relative))
		}
		if entry.IsDir() || entry.Name() != connectorSkillEntryName {
			return nil
		}
		skillCount++
		if skillCount > connectorSkillMaxCount {
			return fmt.Errorf("connector Skill count exceeds %d", connectorSkillMaxCount)
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("connector Skill entry %q must be a regular file", filepath.ToSlash(relative))
		}
		if entryInfo.Size() > connectorSkillMaxEntrySize {
			return fmt.Errorf("connector Skill entry %q exceeds %d bytes", filepath.ToSlash(relative), connectorSkillMaxEntrySize)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		metadata, err := parseSkill(string(content))
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.ToSlash(relative), err)
		}
		if err := ValidateSkillSummary(metadata); err != nil {
			return fmt.Errorf("%s: %w", filepath.ToSlash(relative), err)
		}
		projectionBytes += skillSummaryBytes(metadata)
		if projectionBytes > contracts.ConnectorSkillProjectionMaxBytes {
			return fmt.Errorf("connector Skill summary projection exceeds %d bytes", contracts.ConnectorSkillProjectionMaxBytes)
		}
		if _, duplicate := seen[metadata.Name]; duplicate {
			return fmt.Errorf("duplicate Connector Skill %q", metadata.Name)
		}
		seen[metadata.Name] = struct{}{}
		skills = append(skills, metadata)
		return nil
	})
	if err != nil {
		return SkillProjection{}, err
	}
	sort.Slice(skills, func(left, right int) bool { return skills[left].Name < skills[right].Name })
	return SkillProjection{Root: skillRoot, Skills: skills}, nil
}

// ValidateSkillSummaries applies the public projection bounds to metadata
// received across a process boundary. Artifact inspection calls the same
// validation before a route can be committed.
func ValidateSkillSummaries(summaries []contracts.ConnectorSkillSummary) error {
	return contracts.ValidateConnectorSkillSummaries(summaries)
}

func ValidateSkillSummary(summary contracts.ConnectorSkillSummary) error {
	return contracts.ValidateConnectorSkillSummary(summary)
}

func skillSummaryBytes(summary contracts.ConnectorSkillSummary) int {
	return len(summary.Name) + len(summary.Title) + len(summary.Description)
}

func parseSkill(content string) (contracts.ConnectorSkillSummary, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return contracts.ConnectorSkillSummary{}, errors.New("SKILL.md frontmatter is required")
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return contracts.ConnectorSkillSummary{}, errors.New("SKILL.md frontmatter is malformed")
	}
	frontmatter := content[4 : 4+end]
	body := content[4+end+4:]
	var header struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &header); err != nil {
		return contracts.ConnectorSkillSummary{}, errors.New("SKILL.md frontmatter is malformed")
	}
	metadata := contracts.ConnectorSkillSummary{Name: strings.TrimSpace(header.Name), Description: strings.TrimSpace(header.Description)}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			metadata.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	if metadata.Name == "" || metadata.Description == "" {
		return contracts.ConnectorSkillSummary{}, errors.New("SKILL.md name and description are required")
	}
	if metadata.Title == "" {
		metadata.Title = metadata.Name
	}
	return metadata, nil
}
