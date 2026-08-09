package liveprotocol

import "strings"

type protocolDialectDescriptor struct {
	Revision string
	Profile  string
}

// AcceptedProtocolRevisions returns exact dialect revisions that this build
// can safely speak. Historical revisions preserve their existing semantics;
// newer-only semantics may be downgraded by the matching profile.
func AcceptedProtocolRevisions() []string {
	revisions := make([]string, 0, len(generatedProtocolDialects))
	for _, dialect := range generatedProtocolDialects {
		revisions = append(revisions, dialect.Revision)
	}
	return revisions
}

func SupportsProtocolRevision(revision string) bool {
	dialect, ok := protocolDialectForRevision(revision)
	return ok && supportsProtocolDialectProfile(dialect.Profile)
}

func protocolDialectForRevision(revision string) (protocolDialectDescriptor, bool) {
	normalized := strings.TrimSpace(revision)
	if normalized != revision {
		return protocolDialectDescriptor{}, false
	}
	for _, dialect := range generatedProtocolDialects {
		if dialect.Revision == normalized {
			return dialect, true
		}
	}
	return protocolDialectDescriptor{}, false
}

func projectPublishInputForRevision(revision string, input PublishInput) (PublishInput, error) {
	dialect, ok := protocolDialectForRevision(revision)
	if !ok {
		return PublishInput{}, ErrProtocolMismatch
	}
	switch dialect.Profile {
	case protocolDialectProfileCurrent:
		return input, nil
	case protocolDialectProfilePreSessionRestored:
		if input.Discontinuity == nil ||
			strings.TrimSpace(input.Discontinuity.Reason) != "session_restored" {
			return input, nil
		}
		discontinuity := *input.Discontinuity
		discontinuity.Reason = "canonical_update"
		input.Discontinuity = &discontinuity
		return input, nil
	default:
		return PublishInput{}, ErrProtocolMismatch
	}
}

func validateFrameForProtocolDialect(frame Frame) error {
	if isTypedRejectionFrame(frame) {
		return nil
	}
	dialect, ok := protocolDialectForRevision(frame.ProtocolRevision)
	if !ok {
		return ErrProtocolMismatch
	}
	switch dialect.Profile {
	case protocolDialectProfileCurrent:
		return nil
	case protocolDialectProfilePreSessionRestored:
		for _, delivery := range frame.Deliveries {
			if delivery.Discontinuity != nil &&
				strings.TrimSpace(delivery.Discontinuity.Reason) == "session_restored" {
				return ErrProtocolMismatch
			}
		}
		return nil
	default:
		return ErrProtocolMismatch
	}
}

func supportsProtocolDialectProfile(profile string) bool {
	switch profile {
	case protocolDialectProfileCurrent, protocolDialectProfilePreSessionRestored:
		return true
	default:
		return false
	}
}
