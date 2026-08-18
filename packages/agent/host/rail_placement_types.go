package agenthost

type RailPlacementKind string

const (
	RailPlacementKindConversations RailPlacementKind = "conversations"
	RailPlacementKindProject       RailPlacementKind = "project"
)

// RailPlacement is the caller-selected conversation-rail identity for a newly
// created session. Host canonicalizes project paths and derives project
// SectionKey values from them; conversation placement uses the canonical
// conversations key. ProjectPath is the caller's logical project path, not a
// prepared runtime or owner-host path.
type RailPlacement struct {
	Version     int               `json:"version"`
	Kind        RailPlacementKind `json:"kind"`
	ProjectPath string            `json:"projectPath,omitempty"`
	SectionKey  string            `json:"sectionKey"`
}

// ResolveRuntimeSessionRailPlacementInput identifies the final prepared
// runtime context whose canonical rail placement must be known before a
// provider process starts.
type ResolveRuntimeSessionRailPlacementInput struct {
	WorkspaceID                string
	AgentSessionID             string
	Cwd                        string
	RuntimeContext             map[string]any
	RailPlacement              *RailPlacement
	RailPlacementAuthoritative bool
}
