// Package process owns the process primitives used by Connector installation,
// MCP, CLI, readiness, and credential-broker execution. It deliberately has no
// dependency on Agent runtime or product composition.
package process

import (
	"context"
	"os"
)

// Spec describes one verified Connector process launch.
type Spec struct {
	ConnectorKey       string
	ConnectionID       string
	CWD                string
	Command            []string
	Env                []string
	ExecutableIdentity *ExecutableIdentity
	ArtifactTrees      []ArtifactTreeIdentity
	// SensitiveInheritedFiles are opt-in descriptors whose contents must never
	// be copied into argv or the process environment. The transport maps each
	// file to fd 3+n and publishes only that descriptor number.
	SensitiveInheritedFiles []SensitiveInheritedFile
}

// ArtifactTreeIdentity binds an immutable Connector artifact snapshot to the
// inventory digest verified from its signed artifact receipt.
type ArtifactTreeIdentity struct {
	Root   string
	SHA256 string
}

// SensitiveInheritedFile describes one host-owned secret-bearing descriptor.
// Ownership stays with the caller; the transport duplicates the descriptor for
// the child and never closes File.
type SensitiveInheritedFile struct {
	File             *os.File
	DescriptorEnvKey string
	Purpose          string
}

// ExecutableIdentity binds a launch to bytes verified by the owning managed
// runtime boundary.
type ExecutableIdentity struct {
	SHA256    string
	SizeBytes int64
}

// Frame is one bounded process output or terminal frame.
type Frame struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode *int
}

// Connection is the minimal bidirectional process stream used by Connector
// protocols.
type Connection interface {
	Send([]byte) error
	Recv() (Frame, error)
	Close() error
}

// ContextConnection lets a protocol stop waiting without terminating the
// process.
type ContextConnection interface {
	Connection
	RecvContext(context.Context) (Frame, error)
}

// GracefulConnection exposes the Connector process shutdown ladder.
type GracefulConnection interface {
	Connection
	CloseInput() error
	Terminate() error
	Kill() error
}

// Transport starts verified Connector processes.
type Transport interface {
	Start(context.Context, Spec) (Connection, error)
}
