package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var (
	semverPattern        = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	stableVersionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
	rcVersionPattern     = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-rc\.(0|[1-9]\d*)$`)
	featureKeyPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*$`)
)

type parsedRemoteResponse struct {
	Policy            PolicyResponse
	Feature           []string
	FeaturePresent    bool
	FeatureValid      bool
	FeatureParseError error
}

func ParseRemoteResponse(raw []byte) (parsedRemoteResponse, error) {
	var fields map[string]json.RawMessage
	if err := decodeOneJSONValue(raw, &fields); err != nil {
		return parsedRemoteResponse{}, err
	}
	for field := range fields {
		switch field {
		case "channel", "minimumVersion", "decision", "reason", "policyRevision", "featureAvailability":
		default:
			return parsedRemoteResponse{}, fmt.Errorf(
				"desktop update admission response contains unknown field %q",
				field,
			)
		}
	}
	var envelope struct {
		Channel             json.RawMessage `json:"channel"`
		MinimumVersion      json.RawMessage `json:"minimumVersion"`
		Decision            json.RawMessage `json:"decision"`
		Reason              json.RawMessage `json:"reason"`
		PolicyRevision      json.RawMessage `json:"policyRevision"`
		FeatureAvailability json.RawMessage `json:"featureAvailability"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return parsedRemoteResponse{}, fmt.Errorf("decode desktop update admission response: %w", err)
	}

	policy := PolicyResponse{}
	if err := decodeRequiredString(envelope.Channel, "channel", &policy.Channel); err != nil {
		return parsedRemoteResponse{}, err
	}
	if err := decodeRequiredString(envelope.Decision, "decision", &policy.Decision); err != nil {
		return parsedRemoteResponse{}, err
	}
	if err := decodeRequiredString(envelope.Reason, "reason", &policy.Reason); err != nil {
		return parsedRemoteResponse{}, err
	}
	if err := decodeRequiredString(envelope.PolicyRevision, "policyRevision", &policy.PolicyRevision); err != nil {
		return parsedRemoteResponse{}, err
	}
	if len(envelope.MinimumVersion) > 0 {
		if err := json.Unmarshal(envelope.MinimumVersion, &policy.MinimumVersion); err != nil || strings.TrimSpace(policy.MinimumVersion) == "" {
			return parsedRemoteResponse{}, errors.New("minimumVersion must be a non-empty string when present")
		}
	}
	if err := validatePolicyResponse(policy); err != nil {
		return parsedRemoteResponse{}, err
	}

	result := parsedRemoteResponse{Policy: policy}
	if len(envelope.FeatureAvailability) == 0 {
		return result, nil
	}
	result.FeaturePresent = true
	keys, err := parseFeatureAvailability(envelope.FeatureAvailability)
	if err != nil {
		result.FeatureParseError = err
		return result, nil
	}
	result.Feature = keys
	result.FeatureValid = true
	return result, nil
}

func decodeOneJSONValue(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode desktop update admission response: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("desktop update admission response contains trailing JSON")
	}
	return nil
}

func decodeRequiredString(raw json.RawMessage, field string, target *string) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if err := json.Unmarshal(raw, target); err != nil || strings.TrimSpace(*target) == "" {
		return fmt.Errorf("%s must be a non-empty string", field)
	}
	return nil
}

func validatePolicyResponse(response PolicyResponse) error {
	hasMinimum := response.MinimumVersion != ""
	validMinimum := func(pattern *regexp.Regexp) bool {
		return hasMinimum && pattern.MatchString(response.MinimumVersion)
	}

	switch {
	case response.Channel == "unmanaged" &&
		response.Decision == "notApplicable" &&
		response.Reason == "unmanagedPrerelease" &&
		!hasMinimum:
		return nil
	case (response.Channel == "stable" || response.Channel == "rc") &&
		response.Decision == "notApplicable" &&
		response.Reason == "unsupportedRelease" &&
		!hasMinimum:
		return nil
	case (response.Channel == "stable" || response.Channel == "rc") &&
		response.Decision == "allowed" &&
		response.Reason == "minimumNotConfigured" &&
		!hasMinimum:
		return nil
	case response.Channel == "stable" &&
		response.Decision == "allowed" &&
		response.Reason == "meetsMinimum" &&
		validMinimum(stableVersionPattern):
		return nil
	case response.Channel == "rc" &&
		response.Decision == "allowed" &&
		response.Reason == "meetsMinimum" &&
		validMinimum(rcVersionPattern):
		return nil
	case response.Channel == "stable" &&
		response.Decision == "upgradeRequired" &&
		response.Reason == "belowMinimum" &&
		validMinimum(stableVersionPattern):
		return nil
	case response.Channel == "rc" &&
		response.Decision == "upgradeRequired" &&
		response.Reason == "belowMinimum" &&
		validMinimum(rcVersionPattern):
		return nil
	default:
		return errors.New("desktop update admission response has an invalid policy combination")
	}
}

func parseFeatureAvailability(raw json.RawMessage) ([]string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, errors.New("featureAvailability must be an object")
	}
	for field := range fields {
		if field != "keys" {
			return nil, fmt.Errorf(
				"featureAvailability contains unknown field %q",
				field,
			)
		}
	}
	var envelope struct {
		Keys json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errors.New("featureAvailability must be an object")
	}
	if len(envelope.Keys) == 0 {
		return nil, errors.New("featureAvailability.keys is required")
	}
	var values []string
	if err := json.Unmarshal(envelope.Keys, &values); err != nil {
		return nil, errors.New("featureAvailability.keys must be a string array")
	}
	if len(values) > 256 {
		return nil, errors.New("featureAvailability.keys exceeds 256 entries")
	}
	unique := make(map[string]struct{}, len(values))
	for _, key := range values {
		if len(key) > 128 || !featureKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("featureAvailability contains invalid key %q", key)
		}
		if _, exists := unique[key]; exists {
			return nil, fmt.Errorf("featureAvailability contains duplicate key %q", key)
		}
		unique[key] = struct{}{}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func validateIdentity(identity Identity, checksEnabled bool) error {
	switch identity.Product {
	case ProductTSHDesktop, ProductTuttiDesktop:
	default:
		return fmt.Errorf("unsupported desktop product %q", identity.Product)
	}
	switch identity.Platform {
	case PlatformMacOS, PlatformWindows, PlatformLinux:
	default:
		return fmt.Errorf("unsupported desktop platform %q", identity.Platform)
	}
	switch identity.Architecture {
	case ArchitectureARM64, ArchitectureX64:
	default:
		return fmt.Errorf("unsupported desktop architecture %q", identity.Architecture)
	}
	if strings.TrimSpace(identity.CurrentVersion) == "" {
		return errors.New("desktop current version is required")
	}
	if checksEnabled && !semverPattern.MatchString(identity.CurrentVersion) {
		return fmt.Errorf("desktop current version %q is not valid SemVer", identity.CurrentVersion)
	}
	return nil
}
