package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectSkillsValidatesAndProjectsRecursiveTree(t *testing.T) {
	root := t.TempDir()
	writeSkillTestFile(t, filepath.Join(root, "skills", "calendar", "SKILL.md"), "calendar", "Calendar")
	writeSkillTestFile(t, filepath.Join(root, "skills", "workflows", "standup", "SKILL.md"), "standup", "Standup")
	projection, err := InspectSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Root != filepath.Join(root, "skills") || len(projection.Skills) != 2 ||
		projection.Skills[0].Name != "calendar" || projection.Skills[1].Name != "standup" {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestInspectSkillsAllowsMissingOptionalTree(t *testing.T) {
	projection, err := InspectSkills(t.TempDir())
	if err != nil || projection.Root != "" || len(projection.Skills) != 0 {
		t.Fatalf("projection = %#v, err = %v", projection, err)
	}
}

func TestInspectSkillsRejectsDuplicateNames(t *testing.T) {
	root := t.TempDir()
	writeSkillTestFile(t, filepath.Join(root, "skills", "first", "SKILL.md"), "duplicate", "First")
	writeSkillTestFile(t, filepath.Join(root, "skills", "second", "SKILL.md"), "duplicate", "Second")
	_, err := InspectSkills(root)
	if err == nil || !strings.Contains(err.Error(), `duplicate Connector Skill "duplicate"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestInspectSkillsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside")
	writeSkillTestFile(t, filepath.Join(target, "SKILL.md"), "outside", "Outside")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectSkills(root); err == nil || !strings.Contains(err.Error(), "contains symlink") {
		t.Fatalf("error = %v", err)
	}
}

func TestInspectSkillsBoundsTheCompleteTree(t *testing.T) {
	root := t.TempDir()
	for index := 0; index <= connectorSkillMaxEntries; index++ {
		if err := os.MkdirAll(filepath.Join(root, "skills", "empty-"+fmt.Sprint(index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InspectSkills(root); err == nil || !strings.Contains(err.Error(), "tree entry count exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func writeSkillTestFile(t *testing.T, path, name, title string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Test " + name + ".\n---\n\n# " + title + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
