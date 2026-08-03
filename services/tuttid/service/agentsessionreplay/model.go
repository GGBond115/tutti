package agentsessionreplay

import (
	"errors"

	replay "github.com/tutti-os/tutti/packages/agent/session-replay"
)

var (
	ErrBusy              = replay.ErrBusy
	ErrNotFound          = replay.ErrRecordingNotFound
	ErrCassetteNotFound  = replay.ErrCassetteNotFound
	ErrInvalidState      = replay.ErrInvalidState
	ErrInvalidName       = replay.ErrInvalidName
	ErrInvalidImport     = errors.New("agent session cassette import is invalid")
	ErrUnsupportedTarget = errors.New("agent session recording target is unsupported")
)

type Status = replay.RecordingStatus
type ScenarioMode = replay.ScenarioMode
type ActivityEventKind = replay.ActivityEventKind
type Recording = replay.Recording
type Cassette = replay.Cassette
type ArtifactLayout = replay.ArtifactLayout
type MetadataStore = replay.MetadataStore
type ReplayStateStore = replay.ReplayStateStore
type ProcessRecorder = replay.ProcessRecorder
type ReplayComposerDefaults = replay.ReplayComposerDefaults
type ReplayPrerequisites = replay.ReplayPrerequisites

type ReplayWorkspaceCassette struct {
	Cassette Cassette
	Layout   ArtifactLayout
}

type ReplayWorkspaceRequest struct {
	Cassettes []ReplayWorkspaceCassette
}

type StartInput struct {
	WorkspaceID         string
	AgentTargetID       string
	AgentSessionID      string
	ReplayPrerequisites ReplayPrerequisites
}

type BindInput struct {
	RecordingID    string
	WorkspaceID    string
	AgentTargetID  string
	AgentSessionID string
}

type ImportInput struct {
	WorkspaceID       string
	SourceDirectories []string
}

type ImportFailure struct {
	Code            string
	SourceDirectory string
}

type ImportResult struct {
	Failures   []ImportFailure
	Recordings []Recording
}

type ActivityEvent struct {
	SchemaVersion   int                      `json:"schemaVersion"`
	Sequence        uint64                   `json:"sequence"`
	Kind            replay.ActivityEventKind `json:"kind"`
	Type            string                   `json:"type"`
	EventID         string                   `json:"eventId"`
	CorrelationID   string                   `json:"correlationId,omitempty"`
	CausedByEventID string                   `json:"causedByEventId,omitempty"`
	WorkspaceID     string                   `json:"workspaceId"`
	AgentSessionID  string                   `json:"agentSessionId,omitempty"`
	Payload         map[string]any           `json:"payload,omitempty"`
	OccurredAtMS    int64                    `json:"occurredAtUnixMs"`
}

const (
	StatusPreparing  = replay.RecordingStatusPreparing
	StatusReady      = replay.RecordingStatusReady
	StatusRecording  = replay.RecordingStatusRecording
	StatusFinalizing = replay.RecordingStatusFinalizing
	StatusComplete   = replay.RecordingStatusComplete
	StatusFailed     = replay.RecordingStatusFailed
	StatusCanceled   = replay.RecordingStatusCanceled
	StatusIncomplete = replay.RecordingStatusIncomplete

	ScenarioModeCreateSession   = replay.ScenarioModeCreateSession
	ScenarioModeContinueSession = replay.ScenarioModeContinueSession

	ActivityEventKindIntent         = replay.ActivityEventKindIntent
	ActivityEventKindEffect         = replay.ActivityEventKindEffect
	ActivityEventKindDirectStimulus = replay.ActivityEventKindDirectStimulus
)
