package runtimeprep

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// StableProviderStateID derives the durable provider-state identity. It is
// intentionally scoped to provider, target, and the target's account/authority
// binding; process, model, cwd, and Session values are not part of the key.
func StableProviderStateID(input PrepareInput) (string, error) {
	authority := stableProviderAuthority(input.ProviderTargetRef)
	payload := struct {
		Provider        string         `json:"provider"`
		TargetID        string         `json:"targetId"`
		AuthFingerprint string         `json:"authFingerprint,omitempty"`
		Authority       map[string]any `json:"authority,omitempty"`
	}{
		Provider:        strings.TrimSpace(input.Provider),
		TargetID:        strings.TrimSpace(input.AgentTargetID),
		AuthFingerprint: strings.TrimSpace(input.ProviderAuthFingerprint),
		Authority:       authority,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode provider state identity: %w", err)
	}
	digest := sha256.Sum256(raw)
	return "provider-state-" + hex.EncodeToString(digest[:]), nil
}

func stableProviderAuthority(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		if volatileProviderStateKey(key) || providerAuthMaterialKey(key) {
			continue
		}
		if nested, ok := item.(map[string]any); ok {
			item = stableProviderAuthority(nested)
		} else if list, ok := item.([]any); ok {
			item = stableProviderAuthorityList(list)
		}
		result[key] = item
	}
	return result
}

func providerAuthMaterialKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("_", "", "-", "", ".", "").Replace(key)
	for _, marker := range []string{"token", "apikey", "password", "secret", "credential", "privatekey"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func stableProviderAuthorityList(values []any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		if nested, ok := value.(map[string]any); ok {
			value = stableProviderAuthority(nested)
		}
		result = append(result, value)
	}
	return result
}

func volatileProviderStateKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("_", "", "-", "", ".", "").Replace(key)
	for _, suffix := range []string{
		"generation", "runtimegeneration", "model", "cwd", "session", "sessionid", "agentsessionid",
		"providersessionid", "transport", "transportscopeid", "workspace", "workspaceid",
	} {
		if key == suffix || strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func prepareProviderState(store RuntimeStore, input *PrepareInput) error {
	if input == nil || strings.TrimSpace(input.Provider) == "" {
		return nil
	}
	if input.Provider != "codex" && input.Provider != "tutti-agent" {
		return nil
	}
	stateStore, ok := store.(ProviderStateStore)
	if !ok {
		return fmt.Errorf("provider %q requires provider-state store", input.Provider)
	}
	id := strings.TrimSpace(input.ProviderStateID)
	var err error
	if id == "" {
		id, err = StableProviderStateID(*input)
		if err != nil {
			return err
		}
	}
	root, err := stateStore.ProviderStateRoot(id)
	if err != nil {
		return err
	}
	if err := stateStore.EnsureProviderStateRoot(root); err != nil {
		return err
	}
	input.ProviderStateID = id
	input.ProviderStateRoot = root
	return nil
}
