package agentruntime

import "strings"

func acpTerminalExitCodeCompleted(update map[string]any, body map[string]any, exitCode int) bool {
	if exitCode == 0 {
		return true
	}
	return exitCode == 1 && acpIsGitDiffNoIndexCommand(update, body)
}

func acpIsGitDiffNoIndexCommand(update map[string]any, body map[string]any) bool {
	input := acpMapFromValue(acpToolCallRawInput(update), "input")
	found := false
	for _, candidate := range []any{
		input["parsed_cmd"],
		input["parsedCmd"],
		input["command"],
		body["parsed_cmd"],
		body["parsedCmd"],
		body["command"],
		update["parsed_cmd"],
		update["parsedCmd"],
		update["command"],
	} {
		if candidate == nil {
			continue
		}
		found = true
		if !acpCommandValueIsGitDiffNoIndex(candidate) {
			return false
		}
	}
	return found
}

func acpCommandValueIsGitDiffNoIndex(value any) bool {
	switch typed := value.(type) {
	case string:
		if strings.ContainsAny(typed, ";&|\r\n<>`") {
			return false
		}
		return acpCommandTokensAreGitDiffNoIndex(strings.Fields(typed))
	case []string:
		return acpCommandTokensAreGitDiffNoIndex(typed)
	case []any:
		tokens := make([]string, 0, len(typed))
		allStrings := true
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				allStrings = false
				break
			}
			tokens = append(tokens, text)
		}
		if allStrings && acpCommandTokensAreGitDiffNoIndex(tokens) {
			return true
		}
		if len(typed) == 1 {
			return acpCommandValueIsGitDiffNoIndex(typed[0])
		}
	}
	return false
}

func acpCommandTokensAreGitDiffNoIndex(tokens []string) bool {
	if len(tokens) < 3 {
		return false
	}
	executable := strings.ToLower(strings.Trim(strings.TrimSpace(tokens[0]), "\"'"))
	executable = strings.ReplaceAll(executable, "\\", "/")
	if slash := strings.LastIndex(executable, "/"); slash >= 0 {
		executable = executable[slash+1:]
	}
	if executable != "git" && executable != "git.exe" {
		return false
	}
	if strings.ToLower(strings.Trim(strings.TrimSpace(tokens[1]), "\"'")) != "diff" {
		return false
	}
	for _, token := range tokens[2:] {
		if strings.ToLower(strings.Trim(strings.TrimSpace(token), "\"'")) == "--no-index" {
			return true
		}
	}
	return false
}
