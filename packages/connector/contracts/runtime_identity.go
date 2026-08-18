package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DeviceRuntimeConnectionID is the canonical device-scoped route identity.
func DeviceRuntimeConnectionID(connectorKey string) string {
	return "device-" + strings.TrimSpace(connectorKey)
}

// AccountRuntimeConnectionID is the canonical non-reversible account-scoped
// route identity. All runtime owners must consume this function rather than
// re-derive connection identity.
func AccountRuntimeConnectionID(accountID, connectorKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(accountID) + "\x00" + strings.TrimSpace(connectorKey)))
	return "account-" + hex.EncodeToString(digest[:16])
}
