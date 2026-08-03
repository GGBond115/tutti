package types

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCORSPermitsAgentCommandProvenanceHeaders(t *testing.T) {
	handler := WithCORS(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodOptions, "/v1/workspaces/ws-1/agent-sessions/session-1/input", nil)
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "authorization, content-type, x-tutti-agent-command-id, x-tutti-agent-command-origin")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got, want := response.Header().Get("Access-Control-Allow-Headers"), "Content-Type, Authorization, X-Tutti-Agent-Command-Origin"; got != want {
		t.Fatalf("Access-Control-Allow-Headers = %q, want %q", got, want)
	}
}

func TestWithBearerTokenAuthRejectsEmptyExpectedToken(t *testing.T) {
	called := false
	handler := WithBearerTokenAuthFunc("", nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	request.Header.Set("Authorization", "Bearer anything")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if called {
		t.Fatal("handler was called with an empty expected token")
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestWithBearerTokenAuthAllowsMatchingToken(t *testing.T) {
	called := false
	handler := WithBearerTokenAuthFunc("desktop-session-token", nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	request.Header.Set("Authorization", "Bearer desktop-session-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called {
		t.Fatal("handler was not called with a matching token")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestWithBearerTokenAuthAllowsWebSocketQueryToken(t *testing.T) {
	called := false
	handler := WithBearerTokenAuthFunc("desktop-session-token", nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/workspaces/ws-1/terminals/term-1/ws?access_token=desktop-session-token", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called {
		t.Fatal("handler was not called with a matching websocket query token")
	}
	if response.Code != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSwitchingProtocols)
	}
}

func TestWithBearerTokenAuthRejectsQueryTokenWithoutWebSocketUpgrade(t *testing.T) {
	called := false
	handler := WithBearerTokenAuthFunc("desktop-session-token", nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/health?access_token=desktop-session-token", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if called {
		t.Fatal("handler was called with a query token on a non-websocket request")
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
