package agentstatus

import (
	"testing"
	"time"
)

func TestCodexAccountUsageQuotaMapsProviderWindow(t *testing.T) {
	quota, ok := codexAccountUsageQuota(map[string]any{
		"usedPercent": float64(25), "windowDurationMins": float64(7 * 24 * 60),
		"resetsAt": float64(1_750_000_000),
	})
	if !ok || quota.QuotaType != "weekly" || quota.PercentRemaining != 75 ||
		quota.ResetsAtUnixMS == nil || *quota.ResetsAtUnixMS != 1_750_000_000_000 {
		t.Fatalf("quota = %#v, ok = %v", quota, ok)
	}
}

func TestClaudeAccountUsageQuotaMapsSDKWindow(t *testing.T) {
	reset := "2026-08-21T08:00:00Z"
	quota, ok := claudeAccountUsageQuota(map[string]any{
		"utilization": float64(12.5), "resets_at": reset,
	}, "model", "Opus")
	wantReset, _ := time.Parse(time.RFC3339, reset)
	if !ok || quota.QuotaType != "model" || quota.ModelName != "Opus" ||
		quota.PercentRemaining != 87.5 || quota.ResetsAtUnixMS == nil ||
		*quota.ResetsAtUnixMS != wantReset.UnixMilli() {
		t.Fatalf("quota = %#v, ok = %v", quota, ok)
	}
}

func TestAccountUsageQuotaRejectsInvalidUtilization(t *testing.T) {
	if _, ok := codexAccountUsageQuota(map[string]any{"usedPercent": float64(101)}); ok {
		t.Fatal("Codex quota accepted utilization above 100")
	}
	if _, ok := claudeAccountUsageQuota(map[string]any{"utilization": "25"}, "session", ""); ok {
		t.Fatal("Claude quota accepted a non-numeric utilization")
	}
}

func TestCodexAccountUsageNotApplicableForNonSubscriptionAuth(t *testing.T) {
	for _, authMethod := range []string{"apiKey", "amazonBedrock"} {
		if !codexAccountUsageNotApplicable(authMethod) {
			t.Fatalf("auth method %q should not have subscription quotas", authMethod)
		}
	}
	if codexAccountUsageNotApplicable("chatgpt") {
		t.Fatal("ChatGPT auth should retain subscription quota probing")
	}
}

func TestClaudeUnavailableAccountUsageDistinguishesAPIFromSubscription(t *testing.T) {
	billingMode, quotaState := claudeUnavailableAccountUsage("")
	if billingMode != "api" || quotaState != "not_applicable" {
		t.Fatalf("API usage = (%q, %q)", billingMode, quotaState)
	}
	billingMode, quotaState = claudeUnavailableAccountUsage("pro")
	if billingMode != "provider_account" || quotaState != "unavailable" {
		t.Fatalf("subscription usage = (%q, %q)", billingMode, quotaState)
	}
}
