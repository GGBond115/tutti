package runtimeprep

import (
	"strconv"
	"strings"
)

var tuttiAgentUnsupportedFeatures = []string{
	"apps",
	"current_time_reminder",
	"image_generation",
	"imagegenext",
	"memories",
	"multi_agent",
	"multi_agent_v2",
	"plugins",
	"standalone_web_search",
	"tool_suggest",
}

// tuttiAgentConfigWithUnsupportedFeaturesDisabled keeps provider-owned hosted
// tools and namespace sources out of Tutti Agent sessions. These capabilities
// depend on Codex services that Tutti Agent auth does not provide. Apply the
// policy only to the run-scoped config copy; the user's global config remains
// untouched.
func tuttiAgentConfigWithUnsupportedFeaturesDisabled(content string) (string, bool) {
	next := tuttiAgentConfigWithoutTablePrefix(content, "mcp_servers")
	next = codexConfigWithTopLevelAssignment(next, "web_search", strconv.Quote("disabled"))
	for _, feature := range tuttiAgentUnsupportedFeatures {
		next = tuttiAgentConfigWithTableAssignment(next, "features", feature, "false")
	}
	for _, table := range []string{"orchestrator.mcp", "orchestrator.skills"} {
		next = tuttiAgentConfigWithTableAssignment(next, table, "enabled", "false")
	}
	return next, next != content
}

func tuttiAgentConfigWithTableAssignment(content string, table string, key string, encodedValue string) string {
	line := key + " = " + encodedValue
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for tableStart, existingLine := range lines {
		name, ok := tuttiAgentConfigTableName(existingLine)
		if !ok || name != table {
			continue
		}
		tableEnd := len(lines)
		for index := tableStart + 1; index < len(lines); index++ {
			if _, ok := tuttiAgentConfigTableName(lines[index]); ok {
				tableEnd = index
				break
			}
		}
		for index := tableStart + 1; index < tableEnd; index++ {
			trimmed := strings.TrimSpace(lines[index])
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || !codexConfigLineHasKey(trimmed, key) {
				continue
			}
			if trimmed == line {
				return content
			}
			nextLines := append([]string{}, lines...)
			nextLines[index] = line
			return strings.Join(nextLines, "\n")
		}
		nextLines := make([]string, 0, len(lines)+1)
		nextLines = append(nextLines, lines[:tableEnd]...)
		nextLines = append(nextLines, line)
		nextLines = append(nextLines, lines[tableEnd:]...)
		return strings.Join(nextLines, "\n")
	}
	block := "[" + table + "]\n" + line + "\n"
	if strings.TrimSpace(content) == "" {
		return block
	}
	return strings.TrimRight(content, "\r\n") + "\n\n" + block
}

func tuttiAgentConfigWithoutTablePrefix(content string, prefix string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	nextLines := make([]string, 0, len(lines))
	removing := false
	changed := false
	for _, line := range lines {
		if table, ok := tuttiAgentConfigTableName(line); ok {
			removing = table == prefix || strings.HasPrefix(table, prefix+".")
		}
		if removing {
			changed = true
			continue
		}
		nextLines = append(nextLines, line)
	}
	if !changed {
		return content
	}
	return strings.TrimRight(strings.Join(nextLines, "\n"), "\n") + "\n"
}

func tuttiAgentConfigTableName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "[[") {
		end := strings.Index(trimmed[2:], "]]")
		if end < 0 {
			return "", false
		}
		return strings.TrimSpace(trimmed[2 : end+2]), true
	}
	end := strings.IndexByte(trimmed[1:], ']')
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : end+1]), true
}
