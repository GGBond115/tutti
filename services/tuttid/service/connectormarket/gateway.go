package connectormarket

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type GatewayToolKind string

const (
	GatewayToolMCP GatewayToolKind = "mcp"
	GatewayToolCLI GatewayToolKind = "cli"
)

type GatewayRoute struct {
	WorkspaceID   string
	SessionID     string
	ConnectorKey  string
	ReleaseDigest string
	UpstreamName  string
	Kind          GatewayToolKind
	Generation    uint64
	InputSchema   json.RawMessage
	Invoke        func(context.Context, json.RawMessage) (json.RawMessage, error)
}

type GatewayCaller struct {
	WorkspaceID string
	SessionID   string
	Peer        PeerProof
}

// PeerProof is populated by the platform UDS/Named Pipe acceptor from kernel
// process metadata. It is never accepted from an MCP request payload.
type PeerProof struct {
	ProcessID        int
	ProcessStart     string
	ExecutableDigest string
	Nonce            string
}

type PeerAuthorizer interface {
	Authorize(context.Context, GatewayCaller) error
}

type InputSchemaValidator interface {
	Validate(schema json.RawMessage, input json.RawMessage) error
}

type GenerationRevoker interface {
	RevokeGeneration(context.Context, string, string, uint64) error
}

type ProcessTerminator interface {
	CancelGeneration(context.Context, string, string, uint64) error
	KillGeneration(context.Context, string, string, uint64) error
}

type GatewayConfig struct {
	PeerAuthorizer   PeerAuthorizer
	SchemaValidator  InputSchemaValidator
	CredentialBroker GenerationRevoker
	ResourceGrants   GenerationRevoker
	NetworkEgress    GenerationRevoker
	Processes        ProcessTerminator
	Now              func() time.Time
}

type Gateway struct {
	config  GatewayConfig
	mu      sync.RWMutex
	routes  map[string]GatewayRoute
	revoked map[string]uint64
}

func NewGateway(config GatewayConfig) (*Gateway, error) {
	if config.PeerAuthorizer == nil || config.SchemaValidator == nil || config.CredentialBroker == nil ||
		config.ResourceGrants == nil || config.NetworkEgress == nil || config.Processes == nil {
		return nil, errors.New("connector gateway security ports are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Gateway{config: config, routes: make(map[string]GatewayRoute), revoked: make(map[string]uint64)}, nil
}

func (gateway *Gateway) Register(routes []GatewayRoute) ([]string, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	registered := make([]string, 0, len(routes))
	pending := make(map[string]GatewayRoute, len(routes))
	for _, route := range routes {
		if err := validateGatewayRoute(route); err != nil {
			return nil, err
		}
		key := gatewayGenerationKey(route.WorkspaceID, route.ConnectorKey)
		if route.Generation <= gateway.revoked[key] {
			return nil, errors.New("connector gateway route uses a revoked generation")
		}
		toolID := StableGatewayToolID(route.ConnectorKey, route.UpstreamName, route.ReleaseDigest, route.Kind)
		internalKey := gatewayRouteKey(route.WorkspaceID, route.SessionID, toolID)
		if existing, exists := gateway.routes[internalKey]; exists && !sameGatewayRoute(existing, route) {
			return nil, errors.New("connector gateway tool id collision")
		}
		if existing, exists := pending[internalKey]; exists && !sameGatewayRoute(existing, route) {
			return nil, errors.New("connector gateway registration contains a collision")
		}
		pending[internalKey] = route
		registered = append(registered, toolID)
	}
	for key, route := range pending {
		gateway.routes[key] = route
	}
	return registered, nil
}

func (gateway *Gateway) Invoke(ctx context.Context, caller GatewayCaller, toolID string, input json.RawMessage) (json.RawMessage, error) {
	if err := gateway.config.PeerAuthorizer.Authorize(ctx, caller); err != nil {
		return nil, fmt.Errorf("authorize connector gateway peer: %w", err)
	}
	gateway.mu.RLock()
	route, ok := gateway.routes[gatewayRouteKey(caller.WorkspaceID, caller.SessionID, toolID)]
	revokedGeneration := uint64(0)
	if ok {
		revokedGeneration = gateway.revoked[gatewayGenerationKey(caller.WorkspaceID, route.ConnectorKey)]
	}
	gateway.mu.RUnlock()
	if !ok || route.WorkspaceID != caller.WorkspaceID || route.SessionID != caller.SessionID || route.Generation <= revokedGeneration {
		return nil, errors.New("connector gateway route is unavailable")
	}
	if err := gateway.config.SchemaValidator.Validate(route.InputSchema, input); err != nil {
		return nil, fmt.Errorf("validate connector tool input: %w", err)
	}
	if route.Invoke == nil {
		return nil, errors.New("connector gateway route has no invoker")
	}
	return route.Invoke(ctx, input)
}

// SecurityRevoke applies the non-reversible ordering required for a security
// revocation: fence/remove routes and capabilities, then cancel and bounded
// kill. Callers must surface in-flight work as outcome_unknown and never retry.
func (gateway *Gateway) SecurityRevoke(ctx context.Context, workspaceID, connectorKey string, generation uint64, deadline time.Time) error {
	gateway.mu.Lock()
	key := gatewayGenerationKey(workspaceID, connectorKey)
	if generation <= gateway.revoked[key] {
		gateway.mu.Unlock()
		return nil
	}
	gateway.revoked[key] = generation
	for routeKey, route := range gateway.routes {
		if route.WorkspaceID == workspaceID && route.ConnectorKey == connectorKey && route.Generation <= generation {
			delete(gateway.routes, routeKey)
		}
	}
	gateway.mu.Unlock()

	var revokeErrors []error
	for _, revoker := range []GenerationRevoker{gateway.config.CredentialBroker, gateway.config.ResourceGrants, gateway.config.NetworkEgress} {
		if err := revoker.RevokeGeneration(ctx, workspaceID, connectorKey, generation); err != nil {
			revokeErrors = append(revokeErrors, err)
		}
	}
	if err := gateway.config.Processes.CancelGeneration(ctx, workspaceID, connectorKey, generation); err != nil {
		revokeErrors = append(revokeErrors, err)
	}
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		revokeErrors = append(revokeErrors, ctx.Err())
		if err := gateway.config.Processes.KillGeneration(context.WithoutCancel(ctx), workspaceID, connectorKey, generation); err != nil {
			revokeErrors = append(revokeErrors, err)
		}
	case <-timer.C:
		if err := gateway.config.Processes.KillGeneration(context.WithoutCancel(ctx), workspaceID, connectorKey, generation); err != nil {
			revokeErrors = append(revokeErrors, err)
		}
	}
	return errors.Join(revokeErrors...)
}

func StableGatewayToolID(connectorKey, upstreamName, releaseDigest string, kind GatewayToolKind) string {
	hash := sha256.Sum256([]byte(connectorKey + "\x00" + upstreamName + "\x00" + releaseDigest + "\x00" + string(kind)))
	return "cx_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash[:]))
}

func validateGatewayRoute(route GatewayRoute) error {
	if strings.TrimSpace(route.WorkspaceID) == "" || strings.TrimSpace(route.SessionID) == "" ||
		strings.TrimSpace(route.ConnectorKey) == "" || strings.TrimSpace(route.UpstreamName) == "" ||
		len(route.ReleaseDigest) != 64 || route.Generation == 0 || len(route.InputSchema) == 0 {
		return errors.New("connector gateway route identity is invalid")
	}
	if route.Kind != GatewayToolMCP && route.Kind != GatewayToolCLI {
		return errors.New("connector gateway route kind is invalid")
	}
	return nil
}

func sameGatewayRoute(left, right GatewayRoute) bool {
	return left.WorkspaceID == right.WorkspaceID && left.SessionID == right.SessionID &&
		left.ConnectorKey == right.ConnectorKey && left.ReleaseDigest == right.ReleaseDigest &&
		left.UpstreamName == right.UpstreamName && left.Kind == right.Kind && left.Generation == right.Generation &&
		string(left.InputSchema) == string(right.InputSchema)
}

func gatewayGenerationKey(workspaceID, connectorKey string) string {
	return workspaceID + "\x00" + connectorKey
}

func gatewayRouteKey(workspaceID, sessionID, toolID string) string {
	return workspaceID + "\x00" + sessionID + "\x00" + toolID
}
