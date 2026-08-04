package daemon

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	signatureAlgorithmEd25519       = "Ed25519"
	signatureAlgorithmEd25519SHA256 = "Ed25519-SHA256"
	releaseSignatureContext         = "tutti.connector.release.v1\x00"
	catalogSignatureContext         = "tutti.connector.catalog.v1\x00"
)

// SignedEnvelope transports the exact canonical bytes signed by the market.
// Intermediaries may cache it but must not decode and re-encode Payload.
type SignedEnvelope struct {
	KeyID     string `json:"keyId"`
	Algorithm string `json:"algorithm"`
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}

type ReleaseEnvelopePayload struct {
	SchemaVersion         string   `json:"schemaVersion"`
	ItemType              string   `json:"itemType"`
	ItemKey               string   `json:"itemKey"`
	Version               string   `json:"version"`
	PublisherSubject      string   `json:"publisherSubject"`
	SourceRepository      string   `json:"sourceRepository"`
	CommitSHA             string   `json:"commitSha"`
	WorkflowRef           string   `json:"workflowRef"`
	ProvenanceDigest      string   `json:"provenanceDigest"`
	ArtifactKey           string   `json:"artifactKey"`
	ArtifactStorageRealm  string   `json:"artifactStorageRealm"`
	ArtifactObjectVersion string   `json:"artifactObjectVersion"`
	ArtifactSHA256        string   `json:"artifactSha256"`
	ArtifactSizeBytes     int64    `json:"artifactSizeBytes"`
	ArtifactMediaType     string   `json:"artifactMediaType"`
	ManifestSHA256        string   `json:"manifestSha256"`
	TrustTier             string   `json:"trustTier"`
	Permissions           []string `json:"permissions"`
}

type CatalogReleaseStatus struct {
	ConnectorKey          string        `json:"connectorKey"`
	ReleaseDigest         string        `json:"releaseDigest"`
	Version               string        `json:"version"`
	Status                ReleaseStatus `json:"status"`
	PublishedMarkets      []string      `json:"publishedMarkets"`
	ManifestSHA256        string        `json:"manifestSha256"`
	ArtifactSHA256        string        `json:"artifactSha256"`
	ArtifactObjectVersion string        `json:"artifactObjectVersion"`
	EnvelopeSHA256        string        `json:"envelopeSha256"`
	SignatureKeyID        string        `json:"signatureKeyId"`
	Signature             string        `json:"signature"`
}

type CatalogIndex struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Releases      []CatalogReleaseStatus `json:"releases"`
}

type CatalogSnapshotPayload struct {
	Sequence     uint64       `json:"sequence"`
	IssuedAt     time.Time    `json:"issuedAt"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	NextUpdateAt time.Time    `json:"nextUpdateAt"`
	Catalog      CatalogIndex `json:"catalog"`
}

// CatalogTrustState is persisted with the catalog projection. EnvelopeDigest
// makes a repeated sequence idempotent only when it contains identical bytes.
type CatalogTrustState struct {
	Sequence       uint64    `json:"sequence"`
	EnvelopeDigest string    `json:"envelopeDigest"`
	IssuedAt       time.Time `json:"issuedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	NextUpdateAt   time.Time `json:"nextUpdateAt"`
	ObservedAt     time.Time `json:"observedAt"`
	WallHighWater  time.Time `json:"wallHighWater"`
}

func (state CatalogTrustState) Fresh(now time.Time, maxClockSkew time.Duration) bool {
	now = now.UTC()
	if state.Sequence == 0 || state.ExpiresAt.IsZero() || now.Before(state.WallHighWater.Add(-maxClockSkew)) {
		return false
	}
	return !now.After(state.ExpiresAt.Add(maxClockSkew))
}

type TrustVerifierConfig struct {
	Keys           map[string]ed25519.PublicKey
	Keyring        *TrustKeyring
	MaxSnapshotTTL time.Duration
	MaxClockSkew   time.Duration
	Now            func() time.Time
}

// TrustKeyring is an explicitly versioned set of simultaneously trusted
// market signing keys. A rotation is shipped as two successive versions:
// old+new during the overlap window, then new-only after publishers have
// moved. Version is operational metadata; signatures remain bound to KeyID.
type TrustKeyring struct {
	Version uint64
	Keys    map[string]ed25519.PublicKey
}

type TrustVerifier struct {
	keys           map[string]ed25519.PublicKey
	maxSnapshotTTL time.Duration
	maxClockSkew   time.Duration
	now            func() time.Time
}

func NewTrustVerifier(config TrustVerifierConfig) (*TrustVerifier, error) {
	configuredKeys := config.Keys
	if config.Keyring != nil {
		if len(config.Keys) != 0 {
			return nil, errors.New("connector market trust config cannot mix legacy keys and a versioned keyring")
		}
		if config.Keyring.Version == 0 {
			return nil, errors.New("connector market trust keyring version is required")
		}
		configuredKeys = config.Keyring.Keys
	}
	if len(configuredKeys) == 0 {
		return nil, errors.New("connector market production trust roots are required")
	}
	keys := make(map[string]ed25519.PublicKey, len(configuredKeys))
	for keyID, key := range configuredKeys {
		if strings.TrimSpace(keyID) == "" || len(key) != ed25519.PublicKeySize {
			return nil, errors.New("connector market trust root is invalid")
		}
		keys[keyID] = append(ed25519.PublicKey(nil), key...)
	}
	if config.MaxSnapshotTTL <= 0 {
		config.MaxSnapshotTTL = 15 * time.Minute
	}
	if config.MaxClockSkew <= 0 {
		config.MaxClockSkew = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &TrustVerifier{keys: keys, maxSnapshotTTL: config.MaxSnapshotTTL, maxClockSkew: config.MaxClockSkew, now: config.Now}, nil
}

func (verifier *TrustVerifier) VerifyCatalog(
	envelope SignedEnvelope,
	previous CatalogTrustState,
) (CatalogSnapshotPayload, CatalogTrustState, error) {
	now := verifier.now().UTC()
	if err := verifier.verifyEnvelope(catalogSignatureContext, envelope); err != nil {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, err
	}
	var payload CatalogSnapshotPayload
	if err := decodeCanonicalJSON(envelope.Payload, &payload); err != nil {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, fmt.Errorf("decode signed connector catalog: %w", err)
	}
	if payload.Catalog.SchemaVersion != "1" || payload.Sequence == 0 {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, errors.New("signed connector catalog identity is invalid")
	}
	if payload.IssuedAt.IsZero() || payload.ExpiresAt.IsZero() || payload.NextUpdateAt.IsZero() ||
		!payload.IssuedAt.Before(payload.NextUpdateAt) || payload.NextUpdateAt.After(payload.ExpiresAt) ||
		payload.ExpiresAt.Sub(payload.IssuedAt) > verifier.maxSnapshotTTL {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, errors.New("signed connector catalog freshness window is invalid")
	}
	if payload.IssuedAt.After(now.Add(verifier.maxClockSkew)) || now.After(payload.ExpiresAt.Add(verifier.maxClockSkew)) {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, errors.New("signed connector catalog is not currently valid")
	}
	digest := envelopeDigest(envelope)
	if previous.Sequence > payload.Sequence {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, errors.New("signed connector catalog sequence rollback rejected")
	}
	if previous.Sequence == payload.Sequence && previous.Sequence != 0 && previous.EnvelopeDigest != digest {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, errors.New("signed connector catalog sequence equivocation rejected")
	}
	if !previous.WallHighWater.IsZero() && now.Before(previous.WallHighWater.Add(-verifier.maxClockSkew)) {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, errors.New("local clock rollback requires a newer trusted catalog")
	}
	if err := validateCatalogReleaseOrder(payload.Catalog.Releases); err != nil {
		return CatalogSnapshotPayload{}, CatalogTrustState{}, err
	}
	state := CatalogTrustState{
		Sequence: payload.Sequence, EnvelopeDigest: digest,
		IssuedAt: payload.IssuedAt.UTC(), ExpiresAt: payload.ExpiresAt.UTC(), NextUpdateAt: payload.NextUpdateAt.UTC(),
		ObservedAt: now, WallHighWater: now,
	}
	if previous.WallHighWater.After(state.WallHighWater) {
		state.WallHighWater = previous.WallHighWater
	}
	return payload, state, nil
}

func (verifier *TrustVerifier) VerifyRelease(envelope SignedEnvelope) (ReleaseEnvelopePayload, string, error) {
	if err := verifier.verifyEnvelope(releaseSignatureContext, envelope); err != nil {
		return ReleaseEnvelopePayload{}, "", err
	}
	var payload ReleaseEnvelopePayload
	if err := decodeCanonicalJSON(envelope.Payload, &payload); err != nil {
		return ReleaseEnvelopePayload{}, "", fmt.Errorf("decode signed connector release: %w", err)
	}
	if payload.SchemaVersion != "1" || payload.ItemType != "connector" || !connectorKeyPattern.MatchString(payload.ItemKey) ||
		strings.TrimSpace(payload.Version) == "" || strings.TrimSpace(payload.PublisherSubject) == "" ||
		strings.TrimSpace(payload.SourceRepository) == "" || strings.TrimSpace(payload.CommitSHA) == "" ||
		strings.TrimSpace(payload.WorkflowRef) == "" || strings.TrimSpace(payload.TrustTier) == "" ||
		!artifactSHA256Pattern.MatchString(payload.ProvenanceDigest) || !artifactSHA256Pattern.MatchString(payload.ManifestSHA256) ||
		!artifactSHA256Pattern.MatchString(payload.ArtifactSHA256) || payload.ArtifactSizeBytes <= 0 ||
		strings.TrimSpace(payload.ArtifactKey) == "" || payload.ArtifactStorageRealm != ConnectorArtifactStorageRealmV1 ||
		strings.TrimSpace(payload.ArtifactObjectVersion) == "" || strings.TrimSpace(payload.ArtifactMediaType) == "" {
		return ReleaseEnvelopePayload{}, "", errors.New("signed connector release payload is invalid")
	}
	if err := validateUniqueIdentifiers("permission", payload.Permissions); err != nil {
		return ReleaseEnvelopePayload{}, "", err
	}
	payloadDigest := sha256.Sum256(envelope.Payload)
	return payload, hex.EncodeToString(payloadDigest[:]), nil
}

func (verifier *TrustVerifier) verifyEnvelope(context string, envelope SignedEnvelope) error {
	if (envelope.Algorithm != signatureAlgorithmEd25519 && envelope.Algorithm != signatureAlgorithmEd25519SHA256) ||
		len(envelope.Signature) != ed25519.SignatureSize || len(envelope.Payload) == 0 {
		return errors.New("connector market signature envelope is invalid")
	}
	key, ok := verifier.keys[envelope.KeyID]
	if !ok {
		return errors.New("connector market signing key is not trusted")
	}
	message := make([]byte, 0, len(context)+len(envelope.Payload))
	message = append(message, context...)
	message = append(message, envelope.Payload...)
	verifiedMessage := message
	if envelope.Algorithm == signatureAlgorithmEd25519SHA256 {
		digest := sha256.Sum256(message)
		verifiedMessage = digest[:]
	}
	if !ed25519.Verify(key, verifiedMessage, envelope.Signature) {
		return errors.New("connector market signature verification failed")
	}
	return nil
}

func decodeCanonicalJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("multiple JSON values are not allowed")
	}
	canonical, err := json.Marshal(destination)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return errors.New("payload is not canonical JSON")
	}
	return nil
}

func validateCatalogReleaseOrder(entries []CatalogReleaseStatus) error {
	keys := make([]string, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		if !connectorKeyPattern.MatchString(entry.ConnectorKey) || !artifactSHA256Pattern.MatchString(entry.ReleaseDigest) {
			return errors.New("signed connector catalog contains an invalid release identity")
		}
		switch entry.Status {
		case ReleaseStatusAvailable, ReleaseStatusSuperseded, ReleaseStatusSecurityRevoked:
		default:
			return errors.New("signed connector catalog contains an invalid release status")
		}
		key := entry.ConnectorKey + "\x00" + entry.ReleaseDigest
		if entry.Version == "" || entry.EnvelopeSHA256 != entry.ReleaseDigest ||
			!artifactSHA256Pattern.MatchString(entry.ManifestSHA256) || !artifactSHA256Pattern.MatchString(entry.ArtifactSHA256) ||
			entry.ArtifactObjectVersion == "" || entry.SignatureKeyID == "" {
			return errors.New("signed connector catalog contains incomplete release evidence")
		}
		if _, exists := seen[key]; exists {
			return errors.New("signed connector catalog contains a duplicate release")
		}
		seen[key] = struct{}{}
		keys[index] = key
	}
	if !sort.StringsAreSorted(keys) {
		return errors.New("signed connector catalog releases must use canonical order")
	}
	return nil
}

func envelopeDigest(envelope SignedEnvelope) string {
	hash := sha256.New()
	hash.Write([]byte(envelope.KeyID))
	hash.Write([]byte{0})
	hash.Write([]byte(envelope.Algorithm))
	hash.Write([]byte{0})
	hash.Write(envelope.Payload)
	hash.Write([]byte{0})
	hash.Write(envelope.Signature)
	return hex.EncodeToString(hash.Sum(nil))
}
