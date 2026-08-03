package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	developmentEnabledEnvironment            = "DESKTOP_UPDATE_ADMISSION_DEV"
	developmentFeatureKeysEnvironment        = "DESKTOP_UPDATE_ADMISSION_FEATURE_KEYS"
	developmentForegroundIntervalEnvironment = "DESKTOP_UPDATE_ADMISSION_FOREGROUND_INTERVAL_MS"
	developmentMinimumVersionEnvironment     = "DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION"
	developmentMockServerURLEnvironment      = "DESKTOP_UPDATE_ADMISSION_MOCK_SERVER_URL"
	developmentPolicyEnvironment             = "DESKTOP_UPDATE_ADMISSION_POLICY"
	developmentPolicySequenceEnvironment     = "DESKTOP_UPDATE_ADMISSION_POLICY_SEQUENCE"
	developmentScenarioEnvironment           = "DESKTOP_UPDATE_ADMISSION_SCENARIO"
	developmentTransportEnvironment          = "DESKTOP_UPDATE_ADMISSION_TRANSPORT"
)

type DevelopmentResolution struct {
	Enabled            bool
	Transport          string
	MockServerURL      string
	ForegroundInterval time.Duration
	Checker            Checker
}

type developmentPolicyStep struct {
	outcome        string
	minimumVersion string
}

type developmentChecker struct {
	featureKeys []string
	steps       []developmentPolicyStep
	mu          sync.Mutex
	indexes     map[Identity]int
}

func ResolveDevelopment(env map[string]string, identity Identity) (DevelopmentResolution, error) {
	enabled, err := parseDevelopmentBoolean(env[developmentEnabledEnvironment])
	if err != nil {
		return DevelopmentResolution{}, err
	}
	if !enabled {
		return DevelopmentResolution{}, nil
	}
	if !semverPattern.MatchString(identity.CurrentVersion) {
		return DevelopmentResolution{}, invalidDevelopmentError(
			fmt.Sprintf("current version %q must be valid SemVer", identity.CurrentVersion),
		)
	}
	interval := defaultForegroundInterval
	if raw := strings.TrimSpace(env[developmentForegroundIntervalEnvironment]); raw != "" {
		milliseconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || milliseconds < 100 {
			return DevelopmentResolution{}, invalidDevelopmentError(
				developmentForegroundIntervalEnvironment + " must be an integer greater than or equal to 100",
			)
		}
		interval = time.Duration(milliseconds) * time.Millisecond
	}
	transport := strings.TrimSpace(env[developmentTransportEnvironment])
	if transport == "" {
		transport = "in-process"
	}
	switch transport {
	case "loopback":
		for _, name := range []string{
			developmentFeatureKeysEnvironment,
			developmentMinimumVersionEnvironment,
			developmentPolicyEnvironment,
			developmentPolicySequenceEnvironment,
			developmentScenarioEnvironment,
		} {
			if strings.TrimSpace(env[name]) != "" {
				return DevelopmentResolution{}, invalidDevelopmentError(
					name + " belongs to the loopback mock server, not the client daemon",
				)
			}
		}
		mockServerURL, err := validateDevelopmentLoopbackURL(env[developmentMockServerURLEnvironment])
		if err != nil {
			return DevelopmentResolution{}, err
		}
		return DevelopmentResolution{
			Enabled:            true,
			Transport:          transport,
			MockServerURL:      mockServerURL,
			ForegroundInterval: interval,
		}, nil
	case "in-process":
		checker, err := resolveDevelopmentChecker(env, identity)
		if err != nil {
			return DevelopmentResolution{}, err
		}
		return DevelopmentResolution{
			Enabled:            true,
			Transport:          transport,
			ForegroundInterval: interval,
			Checker:            checker,
		}, nil
	default:
		return DevelopmentResolution{}, invalidDevelopmentError(
			developmentTransportEnvironment + " must be in-process or loopback",
		)
	}
}

func parseDevelopmentBoolean(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, invalidDevelopmentError(
			developmentEnabledEnvironment + " must be a boolean flag",
		)
	}
}

func validateDevelopmentLoopbackURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.Port() == "" {
		return "", invalidDevelopmentError(
			developmentMockServerURLEnvironment + " must be an http://127.0.0.1 origin",
		)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func resolveDevelopmentChecker(env map[string]string, identity Identity) (Checker, error) {
	featureKeys := []string{}
	if raw := strings.TrimSpace(env[developmentFeatureKeysEnvironment]); raw != "" {
		values := strings.Split(raw, ",")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
		}
		envelope, err := json.Marshal(map[string]any{"keys": values})
		if err != nil {
			return nil, err
		}
		keys, err := parseFeatureAvailability(envelope)
		if err != nil {
			return nil, invalidDevelopmentError(err.Error())
		}
		featureKeys = keys
	}
	steps, err := resolveDevelopmentPolicySteps(env)
	if err != nil {
		return nil, err
	}
	for _, step := range steps {
		if err := validateDevelopmentStep(identity.CurrentVersion, step); err != nil {
			return nil, err
		}
	}
	return &developmentChecker{
		featureKeys: featureKeys,
		steps:       steps,
		indexes:     make(map[Identity]int),
	}, nil
}

func resolveDevelopmentPolicySteps(env map[string]string) ([]developmentPolicyStep, error) {
	minimum := strings.TrimSpace(env[developmentMinimumVersionEnvironment])
	named := strings.TrimSpace(env[developmentScenarioEnvironment])
	policy := strings.TrimSpace(env[developmentPolicyEnvironment])
	sequence := strings.TrimSpace(env[developmentPolicySequenceEnvironment])
	if named != "" {
		if policy != "" || sequence != "" {
			return nil, invalidDevelopmentError(
				developmentScenarioEnvironment + " is mutually exclusive with configured policy outcomes",
			)
		}
		switch named {
		case "startup-force-success", "startup-updater-unavailable",
			"startup-target-below-minimum", "startup-download-error":
			return []developmentPolicyStep{{outcome: "upgradeRequired", minimumVersion: minimum}}, nil
		case "startup-policy-timeout":
			return []developmentPolicyStep{{outcome: "timeout"}}, nil
		case "retry-policy-released":
			return []developmentPolicyStep{
				{outcome: "upgradeRequired", minimumVersion: minimum},
				{outcome: "minimumNotConfigured"},
			}, nil
		case "foreground-upgrade-required":
			return []developmentPolicyStep{
				{outcome: "allowed", minimumVersion: "requestCurrentVersion"},
				{outcome: "upgradeRequired", minimumVersion: minimum},
			}, nil
		default:
			return nil, invalidDevelopmentError("unknown named scenario " + strconv.Quote(named))
		}
	}
	if policy != "" && sequence != "" {
		return nil, invalidDevelopmentError(
			developmentPolicyEnvironment + " and " + developmentPolicySequenceEnvironment + " are mutually exclusive",
		)
	}
	rawSteps := []string{}
	switch {
	case sequence != "":
		rawSteps = strings.Split(sequence, ",")
	case policy != "":
		rawSteps = []string{policy}
	default:
		return nil, invalidDevelopmentError(
			developmentPolicyEnvironment + " or " + developmentPolicySequenceEnvironment + " is required",
		)
	}
	steps := make([]developmentPolicyStep, 0, len(rawSteps))
	for _, raw := range rawSteps {
		parts := strings.Split(strings.TrimSpace(raw), "@")
		if len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, invalidDevelopmentError("invalid policy step " + strconv.Quote(raw))
		}
		outcome := strings.TrimSpace(parts[0])
		switch outcome {
		case "allowed", "upgradeRequired":
			version := minimum
			if len(parts) == 2 {
				version = strings.TrimSpace(parts[1])
			}
			steps = append(steps, developmentPolicyStep{outcome: outcome, minimumVersion: version})
		case "minimumNotConfigured", "unsupported", "unmanagedPrerelease", "error", "timeout":
			if len(parts) == 2 {
				return nil, invalidDevelopmentError("policy outcome " + outcome + " must not include a minimum version")
			}
			steps = append(steps, developmentPolicyStep{outcome: outcome})
		default:
			return nil, invalidDevelopmentError("unknown policy outcome " + strconv.Quote(outcome))
		}
	}
	return steps, nil
}

func validateDevelopmentStep(currentVersion string, step developmentPolicyStep) error {
	managedChannel := developmentManagedChannel(currentVersion)
	switch step.outcome {
	case "unmanagedPrerelease":
		if managedChannel != "unmanaged" {
			return invalidDevelopmentError("unmanagedPrerelease requires an unmanaged currentVersion")
		}
		return nil
	case "minimumNotConfigured", "unsupported":
		if managedChannel == "unmanaged" {
			return invalidDevelopmentError(step.outcome + " requires a managed currentVersion")
		}
		return nil
	case "allowed", "upgradeRequired":
		if managedChannel == "unmanaged" {
			return invalidDevelopmentError(step.outcome + " requires a managed currentVersion")
		}
		minimum := step.minimumVersion
		if minimum == "requestCurrentVersion" {
			minimum = currentVersion
		}
		if !semverPattern.MatchString(minimum) || developmentManagedChannel(minimum) != managedChannel {
			return invalidDevelopmentError(
				developmentMinimumVersionEnvironment + " must use the " + managedChannel + " channel",
			)
		}
		compared := compareDevelopmentVersions(currentVersion, minimum)
		if step.outcome == "allowed" && compared < 0 {
			return invalidDevelopmentError("allowed requires currentVersion to meet minimumVersion")
		}
		if step.outcome == "upgradeRequired" && compared >= 0 {
			return invalidDevelopmentError("upgradeRequired requires currentVersion below minimumVersion")
		}
	}
	return nil
}

func (checker *developmentChecker) Check(ctx context.Context, identity Identity) ([]byte, error) {
	checker.mu.Lock()
	index := checker.indexes[identity]
	if index >= len(checker.steps) {
		index = len(checker.steps) - 1
	}
	step := checker.steps[index]
	checker.indexes[identity]++
	checker.mu.Unlock()

	switch step.outcome {
	case "timeout":
		<-ctx.Done()
		return nil, ctx.Err()
	case "error":
		return nil, errors.New("development minimum-version policy check failed")
	}

	channel := developmentManagedChannel(identity.CurrentVersion)
	response := map[string]any{
		"channel":             channel,
		"featureAvailability": map[string]any{"keys": checker.featureKeys},
		"policyRevision":      fmt.Sprintf("development-policy-%d", index+1),
	}
	switch step.outcome {
	case "allowed":
		minimum := step.minimumVersion
		if minimum == "requestCurrentVersion" {
			minimum = identity.CurrentVersion
		}
		response["decision"] = "allowed"
		response["minimumVersion"] = minimum
		response["reason"] = "meetsMinimum"
	case "upgradeRequired":
		response["decision"] = "upgradeRequired"
		response["minimumVersion"] = step.minimumVersion
		response["reason"] = "belowMinimum"
	case "minimumNotConfigured":
		response["decision"] = "allowed"
		response["reason"] = "minimumNotConfigured"
	case "unsupported":
		response["decision"] = "notApplicable"
		response["reason"] = "unsupportedRelease"
	case "unmanagedPrerelease":
		response["channel"] = "unmanaged"
		response["decision"] = "notApplicable"
		response["reason"] = "unmanagedPrerelease"
	}
	return json.Marshal(response)
}

func developmentManagedChannel(version string) string {
	switch {
	case stableVersionPattern.MatchString(version):
		return "stable"
	case rcVersionPattern.MatchString(version):
		return "rc"
	default:
		return "unmanaged"
	}
}

func compareDevelopmentVersions(left string, right string) int {
	leftParts := parseDevelopmentVersionParts(left)
	rightParts := parseDevelopmentVersionParts(right)
	for index := 0; index < 3; index++ {
		if compared := compareDevelopmentNumericIdentifier(
			leftParts.core[index],
			rightParts.core[index],
		); compared != 0 {
			return compared
		}
	}
	if leftParts.stable != rightParts.stable {
		if leftParts.stable {
			return 1
		}
		return -1
	}
	return compareDevelopmentNumericIdentifier(leftParts.rc, rightParts.rc)
}

type developmentVersionParts struct {
	core   [3]string
	rc     string
	stable bool
}

func parseDevelopmentVersionParts(version string) developmentVersionParts {
	baseAndPrerelease := strings.SplitN(version, "-", 2)
	core := strings.Split(baseAndPrerelease[0], ".")
	parts := developmentVersionParts{stable: len(baseAndPrerelease) == 1}
	for index := 0; index < 3; index++ {
		parts.core[index] = core[index]
	}
	if len(baseAndPrerelease) == 2 {
		parts.rc = strings.TrimPrefix(baseAndPrerelease[1], "rc.")
	}
	return parts
}

func compareDevelopmentNumericIdentifier(left string, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func invalidDevelopmentError(message string) error {
	return errors.New("invalid desktop update development scenario: " + message)
}
