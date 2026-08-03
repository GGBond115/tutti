package agentsessionreplay

import sessionreplay "github.com/tutti-os/tutti/packages/agent/session-replay"

// SemanticCassetteArtifact is the validated semantic input consumed by the
// replay application service. Filesystem layout remains a data-layer concern.
type SemanticCassetteArtifact struct {
	Manifest        sessionreplay.CassetteManifest
	InitialStateRaw []byte
	InitialState    *TuttiReplayState
	ExpectedState   TuttiReplayState
	CheckpointPlan  sessionreplay.CheckpointPlan
}
