package storesqlite

import "strings"

// HasUsableProviderTurnBinding rejects the legacy sentinel where older
// runtimes copied the canonical Turn id into the provider identity field.
// Provider identities are independently issued and must never be inferred
// from Tutti's canonical identity.
func HasUsableProviderTurnBinding(turn Turn) bool {
	turnID := strings.TrimSpace(turn.TurnID)
	providerTurnID := strings.TrimSpace(turn.RootProviderTurnID)
	return turnID != "" &&
		providerTurnID != "" &&
		providerTurnID != turnID
}
