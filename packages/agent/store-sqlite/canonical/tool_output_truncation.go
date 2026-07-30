package canonical

import (
	"strings"
	"unicode/utf8"
)

const (
	// ToolOutputTextMaxBytes is the per-field persisted and live projection bound.
	ToolOutputTextMaxBytes = 1 << 20
	// ToolOutputTruncationMarker identifies a tool output prefix with omitted bytes.
	ToolOutputTruncationMarker    = "[Output truncated]"
	toolOutputTruncationSeparator = "\n"
)

var truncatedToolOutputTextKeys = [...]string{"text", "stdout", "stderr"}

// TruncateToolOutputText bounds one canonical tool output field while
// preserving a valid UTF-8 prefix and an explicit truncation marker.
func TruncateToolOutputText(value string) string {
	if len(value) <= ToolOutputTextMaxBytes {
		return value
	}

	value = strings.ToValidUTF8(value, string(utf8.RuneError))
	if len(value) <= ToolOutputTextMaxBytes {
		return value
	}

	prefixLimit := ToolOutputTextMaxBytes -
		len(toolOutputTruncationSeparator) -
		len(ToolOutputTruncationMarker)
	for prefixLimit > 0 && !utf8.RuneStart(value[prefixLimit]) {
		prefixLimit--
	}
	prefix := value[:prefixLimit]
	separator := toolOutputTruncationSeparator
	if prefix == "" || strings.HasSuffix(prefix, "\n") {
		separator = ""
	}
	return prefix + separator + ToolOutputTruncationMarker
}

// TruncateToolOutputBody clones one canonical output/error body and applies
// the same text-field bound to its nested canonical tool steps.
func TruncateToolOutputBody(body map[string]any) map[string]any {
	result := cloneToolMap(body)
	truncateToolOutputBodyInPlace(result)
	return result
}

func truncateToolOutputBodyInPlace(body map[string]any) {
	if len(body) == 0 {
		return
	}
	for _, key := range truncatedToolOutputTextKeys {
		if value, ok := body[key].(string); ok {
			body[key] = TruncateToolOutputText(value)
		}
	}
	steps, _ := body["steps"].([]any)
	for _, value := range steps {
		step, _ := value.(map[string]any)
		truncateToolOutputStepInPlace(step)
	}
}

func truncateToolOutputStepInPlace(step map[string]any) {
	if len(step) == 0 {
		return
	}
	for _, key := range []string{
		"toolResult",
		"tool_result",
		"toolError",
		"tool_error",
		"output",
		"error",
	} {
		if body, ok := step[key].(map[string]any); ok {
			truncateToolOutputBodyInPlace(body)
		}
	}
	if payload, ok := step["payload"].(map[string]any); ok {
		for _, key := range []string{"output", "error"} {
			if body, ok := payload[key].(map[string]any); ok {
				truncateToolOutputBodyInPlace(body)
			}
		}
	}
}
