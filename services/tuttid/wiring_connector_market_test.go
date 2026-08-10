package main

import (
	"testing"
)

func TestConnectorMarketDefaultUsesDesktopGateway(t *testing.T) {
	const expected = "https://api.tutti.sh/api/desktop"
	if connectorMarketDefaultBaseURL != expected {
		t.Fatalf("connector market base URL = %q, want %q", connectorMarketDefaultBaseURL, expected)
	}
}

func TestConnectorMCPDefaultUsesDesktopGateway(t *testing.T) {
	const expected = "https://api.tutti.sh/api/desktop"
	if connectorMCPDefaultBaseURL != expected {
		t.Fatalf("connector MCP base URL = %q, want %q", connectorMCPDefaultBaseURL, expected)
	}
}
