package daemon

import "time"

type Product string

const (
	ProductTSHDesktop   Product = "tsh-desktop"
	ProductTuttiDesktop Product = "tutti-desktop"
)

type Platform string

const (
	PlatformMacOS   Platform = "macos"
	PlatformWindows Platform = "windows"
	PlatformLinux   Platform = "linux"
)

type Architecture string

const (
	ArchitectureARM64 Architecture = "arm64"
	ArchitectureX64   Architecture = "x64"
)

type Identity struct {
	Product        Product      `json:"product"`
	Platform       Platform     `json:"platform"`
	Architecture   Architecture `json:"architecture"`
	CurrentVersion string       `json:"currentVersion"`
}

type PolicyResponse struct {
	Channel        string `json:"channel"`
	MinimumVersion string `json:"minimumVersion,omitempty"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason"`
	PolicyRevision string `json:"policyRevision"`
}

type PolicyFailure struct {
	Kind string `json:"kind"`
}

type PolicySnapshot struct {
	Status   string          `json:"status"`
	Response *PolicyResponse `json:"response,omitempty"`
	Failure  *PolicyFailure  `json:"failure,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

type FeatureAvailabilitySnapshot struct {
	Keys           []string   `json:"keys"`
	Source         string     `json:"source"`
	PolicyRevision *string    `json:"policyRevision"`
	FetchedAt      *time.Time `json:"fetchedAt"`
}

type Snapshot struct {
	Identity              Identity                    `json:"identity"`
	Policy                PolicySnapshot              `json:"policy"`
	FeatureAvailability   FeatureAvailabilitySnapshot `json:"featureAvailability"`
	LastAttemptAt         *time.Time                  `json:"lastAttemptAt"`
	NextForegroundCheckAt *time.Time                  `json:"nextForegroundCheckAt"`
}

type RefreshTrigger string

const (
	RefreshTriggerForeground RefreshTrigger = "foreground"
	RefreshTriggerRetry      RefreshTrigger = "retry"
)

type RefreshResult struct {
	Performed  bool     `json:"performed"`
	SkipReason string   `json:"skipReason,omitempty"`
	Snapshot   Snapshot `json:"snapshot"`
}
