package main

import "testing"

func TestConnectorArtifactDefaultUsesPublicAssetsCloudFront(t *testing.T) {
	const expected = "https://d27a59zdy4534h.cloudfront.net/tutti/connector-market/"
	if connectorArtifactBaseURL != expected {
		t.Fatalf("connector artifact base URL = %q, want %q", connectorArtifactBaseURL, expected)
	}
}
