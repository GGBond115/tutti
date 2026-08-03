package daemon

import (
	"context"
	"testing"
	"time"
)

func TestDevelopmentInProcessSequenceIsOwnedByDaemon(t *testing.T) {
	resolution, err := ResolveDevelopment(map[string]string{
		developmentEnabledEnvironment:        "1",
		developmentFeatureKeysEnvironment:    "workspace.example,agent.preview",
		developmentPolicySequenceEnvironment: "upgradeRequired@1.1.0,minimumNotConfigured",
	}, testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Transport != "in-process" || resolution.Checker == nil {
		t.Fatalf("resolution = %#v", resolution)
	}
	first, err := resolution.Checker.Check(context.Background(), testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	parsedFirst, err := ParseRemoteResponse(first)
	if err != nil {
		t.Fatal(err)
	}
	if parsedFirst.Policy.Decision != "upgradeRequired" ||
		len(parsedFirst.Feature) != 2 ||
		parsedFirst.Feature[0] != "agent.preview" {
		t.Fatalf("first response = %#v", parsedFirst)
	}
	second, err := resolution.Checker.Check(context.Background(), testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	parsedSecond, err := ParseRemoteResponse(second)
	if err != nil {
		t.Fatal(err)
	}
	if parsedSecond.Policy.Reason != "minimumNotConfigured" {
		t.Fatalf("second response = %#v", parsedSecond)
	}
}

func TestDevelopmentLoopbackRejectsClientPolicySource(t *testing.T) {
	_, err := ResolveDevelopment(map[string]string{
		developmentEnabledEnvironment:       "1",
		developmentTransportEnvironment:     "loopback",
		developmentMockServerURLEnvironment: "http://127.0.0.1:43210",
		developmentPolicyEnvironment:        "upgradeRequired",
	}, testIdentity())
	if err == nil {
		t.Fatal("expected loopback policy ownership error")
	}
}

func TestDevelopmentForegroundIntervalIsDaemonOwned(t *testing.T) {
	resolution, err := ResolveDevelopment(map[string]string{
		developmentEnabledEnvironment:            "1",
		developmentPolicyEnvironment:             "minimumNotConfigured",
		developmentForegroundIntervalEnvironment: "3000",
	}, testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if resolution.ForegroundInterval != 3*time.Second {
		t.Fatalf("foreground interval = %s", resolution.ForegroundInterval)
	}
}

func TestDevelopmentRetryPolicyReleasedHasReachableSecondStep(t *testing.T) {
	resolution, err := ResolveDevelopment(map[string]string{
		developmentEnabledEnvironment:        "1",
		developmentMinimumVersionEnvironment: "1.1.0",
		developmentScenarioEnvironment:       "retry-policy-released",
	}, testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	first, err := resolution.Checker.Check(context.Background(), testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	firstParsed, err := ParseRemoteResponse(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolution.Checker.Check(context.Background(), testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	secondParsed, err := ParseRemoteResponse(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstParsed.Policy.Decision != "upgradeRequired" ||
		secondParsed.Policy.Reason != "minimumNotConfigured" {
		t.Fatalf("steps = %#v then %#v", firstParsed.Policy, secondParsed.Policy)
	}
}

func TestDevelopmentVersionComparisonDoesNotLoseNumericPrecision(t *testing.T) {
	if compared := compareDevelopmentVersions(
		"900719925474099312345.0.0",
		"900719925474099312344.999.999",
	); compared <= 0 {
		t.Fatalf("comparison = %d, want positive", compared)
	}
}
