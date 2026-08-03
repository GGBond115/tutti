package runtimeprep

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const codexSaverModePolicy = `## Codex Saver Mode

Saver mode configures the default subagent as the Luna worker. Prefer delegating bounded, self-contained work when it would otherwise consume meaningful main-thread reasoning, context, tool calls, or waiting time. This includes substantial independent subtasks and simple but long-running mechanical workflows such as repeated validation, multi-repository checks, CI monitoring, and authorized commit, push, and check flows. Keep only work that is both quick and tightly coupled to the current reasoning in the main thread.

When a task has multiple independent units, spawn one Luna worker per unit without forking the main conversation history; use the no-history option exposed by the current tool. Run read-only or isolated-worktree units in parallel. If write scopes cannot be isolated, run them sequentially. Each delegation must be self-contained and state the relevant context and files, boundaries, allowed state-changing actions, acceptance criteria, retry limit, and expected evidence.

For external waits, let one worker own the bounded end-to-end workflow and prefer a blocking or event-driven wait command over repeated model-driven polling. After workers finish, verify their evidence against the acceptance criteria and redispatch failed units only within the stated scope and retry limit.`

const codexLunaWorkerRole = `name = "default"
description = "Luna worker for cost-efficient, bounded, self-contained implementation, research, verification, and tool-intensive workflows"
model = "gpt-5.6-luna"
model_reasoning_effort = "max"
developer_instructions = "Complete only the delegated task. Respect its stated scope, allowed state changes, retry limit, and expected output; report concrete evidence and do not expand into unrelated work. For long-running external operations, prefer blocking or event-driven waits over repeated polling."
`

func installCodexLunaWorkerRole(codexHome string) (string, error) {
	agentsDir := filepath.Join(codexHome, "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		return "", fmt.Errorf("create Codex agents directory: %w", err)
	}
	rolePath := filepath.Join(agentsDir, "luna_worker.toml")
	if err := os.WriteFile(rolePath, []byte(codexLunaWorkerRole), 0o600); err != nil {
		return "", fmt.Errorf("write Codex Luna worker role: %w", err)
	}
	return rolePath, nil
}

// Declare the session role explicitly so a copied user-defined default role
// cannot win over saver mode's session-scoped default.
func ensureCodexSaverDefaultRole(configPath string) error {
	contentBytes, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read codex config for saver role: %w", err)
	}
	next, changed := codexConfigWithSaverDefaultRole(string(contentBytes))
	if !changed {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(next), 0o600); err != nil {
		return fmt.Errorf("write codex saver role config: %w", err)
	}
	return nil
}

func codexConfigWithSaverDefaultRole(content string) (string, bool) {
	const section = "[agents.default]"
	managedLines := []string{
		`description = "Luna worker for cost-efficient, bounded, self-contained tasks"`,
		`config_file = "./agents/luna_worker.toml"`,
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	cleaned := make([]string, 0, len(lines))
	var currentSection []string
	skipDefaultSection := false
	multilineDelimiter := ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if multilineDelimiter != "" {
			if !skipDefaultSection {
				cleaned = append(cleaned, line)
			}
			if strings.Contains(line, multilineDelimiter) {
				multilineDelimiter = ""
			}
			continue
		}
		if sectionName, arrayTable, ok := codexTOMLSectionKey(line); ok {
			skipDefaultSection = !arrayTable && codexTOMLKeyEquals(sectionName, "agents", "default")
			currentSection = sectionName
			if !skipDefaultSection {
				cleaned = append(cleaned, line)
			}
			continue
		}
		if skipDefaultSection {
			if delimiter := codexTOMLUnclosedMultilineDelimiter(line); delimiter != "" {
				multilineDelimiter = delimiter
			}
			continue
		}
		if codexConfigIsDefaultAgentAssignment(line, currentSection) {
			index = codexConfigAssignmentEndLine(lines, index)
			continue
		}
		cleaned = append(cleaned, line)
		if delimiter := codexTOMLUnclosedMultilineDelimiter(line); delimiter != "" {
			multilineDelimiter = delimiter
		}
	}
	block := section + "\n" + strings.Join(managedLines, "\n")
	base := strings.TrimRight(strings.Join(cleaned, "\n"), "\r\n")
	if strings.TrimSpace(base) == "" {
		return block + "\n", normalized != block+"\n"
	}
	next := base + "\n\n" + block + "\n"
	return next, next != normalized
}

func codexConfigIsDefaultAgentAssignment(line string, currentSection []string) bool {
	lhs, _, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	key, ok := codexTOMLDottedKey(lhs)
	if !ok {
		return false
	}
	if len(currentSection) == 0 {
		return len(key) >= 2 && key[0] == "agents" && key[1] == "default"
	}
	if codexTOMLKeyEquals(currentSection, "agents") {
		return len(key) >= 1 && key[0] == "default"
	}
	return false
}

func codexTOMLSectionKey(line string) ([]string, bool, bool) {
	line = strings.TrimSpace(codexTOMLWithoutComment(line))
	arrayTable := strings.HasPrefix(line, "[[")
	openLength, closeLength := 1, 1
	if arrayTable {
		openLength, closeLength = 2, 2
	}
	if !strings.HasPrefix(line, strings.Repeat("[", openLength)) ||
		!strings.HasSuffix(line, strings.Repeat("]", closeLength)) ||
		len(line) <= openLength+closeLength {
		return nil, false, false
	}
	body := line[openLength : len(line)-closeLength]
	key, ok := codexTOMLDottedKey(body)
	return key, arrayTable, ok
}

func codexTOMLDottedKey(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	segments := make([]string, 0, 2)
	for len(value) > 0 {
		value = strings.TrimLeft(value, " \t")
		if value == "" {
			return nil, false
		}
		var segment string
		switch value[0] {
		case '"':
			end := 1
			escaped := false
			for ; end < len(value); end++ {
				if escaped {
					escaped = false
					continue
				}
				if value[end] == '\\' {
					escaped = true
					continue
				}
				if value[end] == '"' {
					break
				}
			}
			if end >= len(value) {
				return nil, false
			}
			unquoted, err := strconv.Unquote(value[:end+1])
			if err != nil {
				return nil, false
			}
			segment = unquoted
			value = value[end+1:]
		case '\'':
			end := strings.IndexByte(value[1:], '\'')
			if end < 0 {
				return nil, false
			}
			end++
			segment = value[1:end]
			value = value[end+1:]
		default:
			end := strings.IndexByte(value, '.')
			if end < 0 {
				end = len(value)
			}
			segment = strings.TrimSpace(value[:end])
			value = value[end:]
		}
		if segment == "" {
			return nil, false
		}
		segments = append(segments, segment)
		value = strings.TrimLeft(value, " \t")
		if value == "" {
			break
		}
		if value[0] != '.' {
			return nil, false
		}
		value = value[1:]
	}
	return segments, true
}

func codexTOMLKeyEquals(key []string, expected ...string) bool {
	if len(key) != len(expected) {
		return false
	}
	for index := range key {
		if key[index] != expected[index] {
			return false
		}
	}
	return true
}

func codexTOMLWithoutComment(line string) string {
	escaped := false
	quote := byte(0)
	for index := 0; index < len(line); index++ {
		char := line[index]
		if quote == '"' {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '"', '\'':
			quote = char
		case '#':
			return line[:index]
		}
	}
	return line
}

func codexTOMLUnclosedMultilineDelimiter(line string) string {
	code := codexTOMLWithoutComment(line)
	for _, delimiter := range []string{`"""`, `'''`} {
		openIndex := strings.Index(code, delimiter)
		if openIndex < 0 {
			continue
		}
		if !strings.Contains(code[openIndex+len(delimiter):], delimiter) {
			return delimiter
		}
	}
	return ""
}

// Consume a complete multiline TOML array so stale marker entries do not remain
// after replacing project_root_markers with the session-scoped override.
func codexConfigAssignmentEndLine(lines []string, startIndex int) int {
	if startIndex < 0 || startIndex >= len(lines) {
		return startIndex
	}
	_, value, ok := strings.Cut(lines[startIndex], "=")
	if !ok {
		return startIndex
	}
	trimmedValue := strings.TrimSpace(value)
	for _, delimiter := range []string{`"""`, `'''`} {
		if !strings.HasPrefix(trimmedValue, delimiter) {
			continue
		}
		if strings.Contains(strings.TrimPrefix(trimmedValue, delimiter), delimiter) {
			return startIndex
		}
		for index := startIndex + 1; index < len(lines); index++ {
			if strings.Contains(lines[index], delimiter) {
				return index
			}
		}
		return startIndex
	}
	depth := tomlSquareBracketDelta(value) + tomlCurlyBracketDelta(value)
	if depth <= 0 {
		return startIndex
	}
	for index := startIndex + 1; index < len(lines); index++ {
		depth += tomlSquareBracketDelta(lines[index]) + tomlCurlyBracketDelta(lines[index])
		if depth <= 0 {
			return index
		}
	}
	return startIndex
}

func tomlCurlyBracketDelta(line string) int {
	return tomlBracketDelta(line, '{', '}')
}

func tomlSquareBracketDelta(line string) int {
	return tomlBracketDelta(line, '[', ']')
}

func tomlBracketDelta(line string, open rune, close rune) int {
	depth := 0
	escaped := false
	quote := rune(0)
	for _, char := range line {
		switch quote {
		case '"':
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				quote = 0
			}
			continue
		case '\'':
			if char == '\'' {
				quote = 0
			}
			continue
		}
		switch char {
		case '#':
			return depth
		case '"', '\'':
			quote = char
		case open:
			depth++
		case close:
			depth--
		}
	}
	return depth
}
