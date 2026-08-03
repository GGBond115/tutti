package daemon

import "testing"

func TestParseRemoteResponseRejectsUnknownPolicyFields(t *testing.T) {
	_, err := ParseRemoteResponse([]byte(
		`{"channel":"stable","decision":"allowed","reason":"minimumNotConfigured","policyRevision":"v1","policySource":"appconfig"}`,
	))
	if err == nil {
		t.Fatal("expected unknown policy field error")
	}
}

func TestParseRemoteResponseRetainsPolicyWhenFeatureEnvelopeIsInvalid(t *testing.T) {
	parsed, err := ParseRemoteResponse([]byte(
		`{"channel":"stable","decision":"allowed","reason":"minimumNotConfigured","policyRevision":"v1","featureAvailability":{"keys":["workspace.example"],"extra":true}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.FeaturePresent || parsed.FeatureValid || parsed.FeatureParseError == nil {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.Policy.Decision != "allowed" {
		t.Fatalf("policy = %#v", parsed.Policy)
	}
}

func TestParseRemoteResponseRejectsDuplicateFeatureKeys(t *testing.T) {
	parsed, err := ParseRemoteResponse([]byte(
		`{"channel":"stable","decision":"allowed","reason":"minimumNotConfigured","policyRevision":"v1","featureAvailability":{"keys":["workspace.example","workspace.example"]}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.FeatureValid || parsed.FeatureParseError == nil {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestParseRemoteResponseRejectsTrailingJSON(t *testing.T) {
	_, err := ParseRemoteResponse([]byte(
		`{"channel":"stable","decision":"allowed","reason":"minimumNotConfigured","policyRevision":"v1"} {}`,
	))
	if err == nil {
		t.Fatal("expected trailing JSON error")
	}
}
