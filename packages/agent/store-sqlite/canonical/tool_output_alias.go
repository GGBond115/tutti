package canonical

import "strings"

var terminalCommandTokenReplacer = strings.NewReplacer(
	"_", "",
	"-", "",
	" ", "",
	".", "",
)

// CompactTerminalCommandOutputAliases removes only display text that is
// exactly reconstructible from a terminal command's canonical stdout or
// stderr. Running commands retain text because live tool-output deltas use it;
// non-command tools retain text as their provider-neutral display contract.
// The payload is mutated in place and the return value reports whether it
// changed.
func CompactTerminalCommandOutputAliases(
	status string,
	payload map[string]any,
) bool {
	if !isTerminalToolStatus(status) || len(payload) == 0 {
		return false
	}

	changed := false
	input, _ := payload["input"].(map[string]any)
	if isTerminalCommandPayload(payload, input) {
		changed = compactTerminalCommandBodyAlias(payload["output"]) || changed
		changed = compactTerminalCommandBodyAlias(payload["error"]) || changed
	}

	steps, _ := payload["steps"].([]any)
	for _, item := range steps {
		step, _ := item.(map[string]any)
		if len(step) == 0 {
			continue
		}
		stepStatus := firstToolString(step["status"], status)
		if !isTerminalToolStatus(stepStatus) {
			continue
		}
		stepInput, _ := step["toolInput"].(map[string]any)
		if !isTerminalCommandPayload(step, stepInput) {
			continue
		}
		changed = compactTerminalCommandBodyAlias(step["toolResult"]) || changed
		changed = compactTerminalCommandBodyAlias(step["toolError"]) || changed
	}
	return changed
}

func isTerminalCommandPayload(payload, input map[string]any) bool {
	metadata, _ := payload["metadata"].(map[string]any)
	for _, value := range []any{
		payload["toolName"],
		payload["name"],
		payload["activityKind"],
		metadata["toolName"],
		metadata["tool"],
		metadata["kind"],
	} {
		token := strings.ToLower(toolString(value))
		token = terminalCommandTokenReplacer.Replace(token)
		switch token {
		case "bash", "exec", "execcommand", "runcommand", "runshellcommand", "shell", "shellcommand", "terminal", "commandexecution", "executecommand":
			return true
		}
	}
	if input != nil && firstToolString(input["command"], input["cmd"]) != "" {
		return true
	}
	return firstToolString(payload["command"]) != ""
}

func compactTerminalCommandBodyAlias(value any) bool {
	body, _ := value.(map[string]any)
	if body == nil {
		return false
	}
	text, ok := body["text"].(string)
	if !ok {
		return false
	}
	for _, key := range []string{"stdout", "stderr"} {
		stream, ok := body[key].(string)
		if ok && text == strings.TrimSpace(stream) {
			delete(body, "text")
			return true
		}
	}
	return false
}

func isTerminalToolStatus(status string) bool {
	return isCompletedToolStatus(status) || isFailedToolStatus(status)
}
