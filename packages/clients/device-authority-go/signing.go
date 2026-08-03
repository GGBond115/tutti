package deviceauthority

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

const gatewayOwnerSessionPayloadVersion = "tsh.gateway.owner-session.v1"

func randomNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate device gateway nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func gatewayOwnerSessionSigningPayload(authorityID, runtimeID, keyID, nonce, timestamp string, ttlSeconds int, targets []string) (string, error) {
	authorityID = strings.TrimSpace(authorityID)
	runtimeID = strings.TrimSpace(runtimeID)
	keyID = strings.TrimSpace(keyID)
	nonce = strings.TrimSpace(nonce)
	timestamp = strings.TrimSpace(timestamp)
	if authorityID == "" || runtimeID == "" || keyID == "" || nonce == "" || timestamp == "" {
		return "", fmt.Errorf("device gateway owner session signing payload requires non-empty fields")
	}

	normalized := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		// Must match tsh-server normalizeRuntimeTarget before signature
		// verification. The request body intentionally retains the caller's
		// original values; only the canonical signing payload is normalized.
		target = strings.ToLower(strings.TrimSpace(target))
		if target == "" {
			return "", fmt.Errorf("device gateway owner session signing payload target is empty")
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		normalized = append(normalized, target)
	}
	sort.Strings(normalized)

	// This canonical form is verified by tsh-server. Field order, spelling,
	// whitespace, sorting, and newline separators are wire compatibility.
	return strings.Join([]string{
		gatewayOwnerSessionPayloadVersion,
		"authority_id=" + authorityID,
		"runtime_id=" + runtimeID,
		"key_id=" + keyID,
		"nonce=" + nonce,
		"timestamp=" + timestamp,
		fmt.Sprintf("ttl_seconds=%d", ttlSeconds),
		"supported_targets=" + strings.Join(normalized, ","),
	}, "\n"), nil
}
