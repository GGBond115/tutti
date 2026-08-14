package agenthost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	// AgentCWDEnvironmentVariable carries the exact logical working directory
	// of the running Agent session to nested Tutti CLI processes.
	AgentCWDEnvironmentVariable = "TUTTI_AGENT_CWD"
	// AgentRailPlacementEnvironmentVariable carries the Host-normalized,
	// immutable rail placement of the running Agent session as JSON.
	AgentRailPlacementEnvironmentVariable = "TUTTI_AGENT_RAIL_PLACEMENT"
)

// WithAgentRailPlacementEnvironment returns a copy of env with the canonical
// caller cwd and rail placement installed exactly once. Callers use this only
// after Host or a trusted binding has resolved the placement; it never
// classifies a cwd itself.
func WithAgentRailPlacementEnvironment(
	env []string,
	cwd string,
	placement *RailPlacement,
) ([]string, error) {
	normalized, err := normalizeRailPlacement(placement)
	if err != nil {
		return nil, err
	}
	if normalized == nil {
		return nil, ErrInvalidArgument
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode agent rail placement environment: %w", err)
	}
	result := replaceEnvironmentValue(env, AgentCWDEnvironmentVariable, strings.TrimSpace(cwd))
	result = replaceEnvironmentValue(result, AgentRailPlacementEnvironmentVariable, string(encoded))
	return result, nil
}

// ParseAgentRailPlacementEnvironment decodes and Host-normalizes the JSON
// value carried by AgentRailPlacementEnvironmentVariable. Unknown fields,
// trailing values, and unsupported placement versions fail closed.
func ParseAgentRailPlacementEnvironment(value string) (*RailPlacement, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(value)))
	decoder.DisallowUnknownFields()
	var placement RailPlacement
	if err := decoder.Decode(&placement); err != nil {
		return nil, fmt.Errorf("decode agent rail placement environment: %w", err)
	}
	if err := ensureJSONDocumentEnded(decoder); err != nil {
		return nil, fmt.Errorf("decode agent rail placement environment: %w", err)
	}
	return normalizeRailPlacement(&placement)
}

func ensureJSONDocumentEnded(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func replaceEnvironmentValue(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if environmentEntryMatchesKey(entry, key) {
			if !replaced {
				result = append(result, prefix+value)
				replaced = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !replaced {
		result = append(result, prefix+value)
	}
	return result
}

func environmentEntryMatchesKey(entry, key string) bool {
	separator := strings.IndexByte(entry, '=')
	return separator >= 0 && strings.EqualFold(entry[:separator], key)
}
