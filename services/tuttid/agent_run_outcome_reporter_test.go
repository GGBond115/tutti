package main

import (
	"context"
	"testing"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

type submitProvenanceCaptureReporter struct {
	intent     agentsessionstore.SubmitIntentInput
	provenance agentsessionstore.SubmitProvenanceInput
}

func (*submitProvenanceCaptureReporter) Report(context.Context, agentsessionstore.ReportActivityInput) error {
	return nil
}

func (r *submitProvenanceCaptureReporter) AdmitSubmitIntent(_ context.Context, input agentsessionstore.SubmitIntentInput) error {
	r.intent = input
	return nil
}

func (r *submitProvenanceCaptureReporter) UpdateSubmitProvenance(_ context.Context, input agentsessionstore.SubmitProvenanceInput) error {
	r.provenance = input
	return nil
}

func TestMessageLooksLikeAuthFailureMatchesRealClaude401(t *testing.T) {
	// The exact shape seen in the field logs: a failed runtime text message.
	payload := map[string]any{
		"source":  "runtime",
		"content": "Failed to authenticate. API Error: 401 Invalid authentication credentials",
		"text":    "Failed to authenticate. API Error: 401 Invalid authentication credentials",
	}
	if !messageLooksLikeAuthFailure("failed", payload) {
		t.Fatal("a failed Claude 401 message should be classified as an auth failure")
	}
}

func TestMessageLooksLikeAuthFailureMatchesRealGeminiVertexADCFailure(t *testing.T) {
	payload := map[string]any{
		"text": "Could not load the default credentials. Browse to Google Cloud authentication documentation for more information",
	}
	if !messageLooksLikeAuthFailure("failed", payload) {
		t.Fatal("a failed Gemini Vertex ADC message should be classified as an auth failure")
	}
}

func TestMessageLooksLikeAuthFailureUsesStructuredCode(t *testing.T) {
	if !messageLooksLikeAuthFailure("failed", map[string]any{"code": "auth_required"}) {
		t.Fatal("an explicit auth_required code should classify as auth failure")
	}
}

func TestMessageLooksLikeAuthFailureIgnoresNonFailedAndNonAuth(t *testing.T) {
	if messageLooksLikeAuthFailure("completed", map[string]any{"text": "401 auth"}) {
		t.Fatal("a non-failed message must not be an auth failure")
	}
	if messageLooksLikeAuthFailure("failed", map[string]any{"text": "rate limit exceeded"}) {
		t.Fatal("a non-auth failure must not match")
	}
}

func TestReportRunOutcomeAuthFailureWinsOverCompletion(t *testing.T) {
	input := agentsessionstore.ReportActivityInput{
		Source: canonical.EventSource{Provider: "claude-code"},
		MessageUpdates: []agentsessionstore.WorkspaceAgentMessageUpdate{
			{Status: "completed", Payload: map[string]any{"text": "hi"}},
			{Status: "failed", Payload: map[string]any{
				"text": "Failed to authenticate. API Error: 401 Invalid authentication credentials",
			}},
		},
	}
	if got := reportRunOutcome(input); got != runOutcomeAuthFailed {
		t.Fatalf("reportRunOutcome = %v, want authFailed", got)
	}
}

func TestReportRunOutcomeSuccessClears(t *testing.T) {
	input := agentsessionstore.ReportActivityInput{
		Source: canonical.EventSource{Provider: "codex"},
		MessageUpdates: []agentsessionstore.WorkspaceAgentMessageUpdate{
			{Status: "completed", Payload: map[string]any{"text": "done"}},
		},
	}
	if got := reportRunOutcome(input); got != runOutcomeSuccess {
		t.Fatalf("reportRunOutcome = %v, want success", got)
	}
}

func TestAgentRunOutcomeReporterPreservesSubmitIntentAdmission(t *testing.T) {
	inner := &submitProvenanceCaptureReporter{}
	reporter := agentRunOutcomeReporter{DurableActivityReporter: inner}
	input := agentsessionstore.SubmitIntentInput{WorkspaceID: "ws-1"}
	if err := reporter.AdmitSubmitIntent(context.Background(), input); err != nil {
		t.Fatalf("AdmitSubmitIntent() error = %v", err)
	}
	if inner.intent.WorkspaceID != "ws-1" {
		t.Fatalf("forwarded submit intent = %#v", inner.intent)
	}
}
