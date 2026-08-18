package application

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"regexp"
	"strings"
)

var runtimeConnectionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,190}$`)

// AccountRuntimeBindingResolver derives runtime intent from device-scoped
// installation plus account-scoped authorization. It never caches grants.
type AccountRuntimeBindingResolver struct {
	Projections AuthorizationProjectionStore
	Credentials CredentialBrokerGrantIssuer
	Readiness   *AuthorizationReadinessGate
}

func (resolver AccountRuntimeBindingResolver) ResolveRuntimeIntent(
	ctx context.Context,
	request contracts.RuntimeBindingRequest,
) (contracts.RuntimeIntent, error) {
	request.Purpose = contracts.RuntimeBindingPurposePlan
	binding, err := resolver.ResolveRuntimeBinding(ctx, request)
	if len(binding.CredentialBrokerGrant) != 0 {
		clear(binding.CredentialBrokerGrant)
		return contracts.RuntimeIntent{}, invalidOperationReceipt("runtime intent resolver returned a credential grant")
	}
	return contracts.RuntimeIntent{
		ConnectionID: binding.ConnectionID, Enabled: binding.Enabled, AuthorizationState: binding.AuthorizationState,
	}, err
}

func (resolver AccountRuntimeBindingResolver) ResolveRuntimeBinding(
	ctx context.Context,
	request contracts.RuntimeBindingRequest,
) (contracts.RuntimeBinding, error) {
	connectorKey := strings.TrimSpace(request.Connector.Key)
	if connectorKey == "" {
		connectorKey = strings.TrimSpace(request.Release.ConnectorKey)
	}
	if connectorKey == "" {
		return contracts.RuntimeBinding{}, invalidRequest("connectorKey is required for runtime binding")
	}
	remote := request.Release.Manifest.Implementation.RemoteStreamableHTTP != nil
	if request.Release.Manifest.AuthorizationKind == "none" {
		if remote {
			accountID := strings.TrimSpace(request.Scope.AccountID)
			if accountID == "" {
				return contracts.RuntimeBinding{ConnectionID: contracts.AccountRuntimeConnectionID("signed-out", connectorKey), Enabled: false, AuthorizationState: contracts.AuthorizationStateNotRequired}, nil
			}
			return contracts.RuntimeBinding{ConnectionID: contracts.AccountRuntimeConnectionID(accountID, connectorKey), Enabled: true, AuthorizationState: contracts.AuthorizationStateNotRequired}, nil
		}
		return contracts.RuntimeBinding{ConnectionID: contracts.DeviceRuntimeConnectionID(connectorKey), Enabled: true, AuthorizationState: contracts.AuthorizationStateNotRequired}, nil
	}
	accountID := strings.TrimSpace(request.Scope.AccountID)
	if accountID == "" {
		return contracts.RuntimeBinding{ConnectionID: contracts.AccountRuntimeConnectionID("signed-out", connectorKey), Enabled: false, AuthorizationState: contracts.AuthorizationStateDisconnected}, nil
	}
	connectionID := contracts.AccountRuntimeConnectionID(accountID, connectorKey)
	if remote && resolver.Readiness != nil && !resolver.Readiness.Ready(accountID) {
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: false, AuthorizationState: contracts.AuthorizationStateDisconnected}, nil
	}
	if resolver.Projections == nil {
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: false, AuthorizationState: contracts.AuthorizationStateDisconnected}, nil
	}
	projection, err := resolver.Projections.AuthorizationProjection(ctx, accountID, connectorKey)
	if errors.Is(err, contracts.ErrNotFound) {
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: false, AuthorizationState: contracts.AuthorizationStateDisconnected}, nil
	}
	if err != nil {
		return contracts.RuntimeBinding{}, fmt.Errorf("load connector authorization projection: %w", err)
	}
	if projection.AccountID != accountID || projection.ConnectorKey != connectorKey {
		return contracts.RuntimeBinding{}, invalidOperationReceipt("authorization projection identity does not match runtime scope")
	}
	if remote && !projection.ServerSynchronized {
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: false, AuthorizationState: contracts.AuthorizationStateDisconnected}, nil
	}
	// Remote routes have a stable account+connector identity. The server's
	// connectionId is diagnostic authorization state and can change when a
	// default connection changes; it must not create an orphan local route.
	if projectedConnectionID := strings.TrimSpace(projection.ConnectionID); !remote && projectedConnectionID != "" {
		connectionID = projectedConnectionID
	}
	if !runtimeConnectionIDPattern.MatchString(connectionID) {
		return contracts.RuntimeBinding{}, invalidOperationReceipt("authorization projection connection id is invalid")
	}
	if projection.State != contracts.AuthorizationStateConnected {
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: false, AuthorizationState: projection.State}, nil
	}
	if request.Purpose == contracts.RuntimeBindingPurposePlan || request.Purpose == contracts.RuntimeBindingPurposeDeactivate {
		// Planning persists only non-secret intent. Reconcile resolves a fresh,
		// one-shot credential grant immediately before the host call.
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: true, AuthorizationState: projection.State}, nil
	}
	managed := request.Release.Manifest.Implementation.ManagedStdio
	if remote {
		// Remote MCP routes authenticate to tsh-server with the Tutti account
		// session. Provider credentials never cross the daemon boundary.
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: true, AuthorizationState: projection.State}, nil
	}
	if managed != nil && managed.CredentialBroker != nil {
		// Connector-owned credential brokers persist their own account binding
		// inside the managed VM user home. They do not consume a Server-issued
		// credential grant when the active CLI/MCP route is reconciled.
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: true, AuthorizationState: projection.State}, nil
	}
	if resolver.Credentials == nil {
		return contracts.RuntimeBinding{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "credential broker grant issuer is not registered", true, nil)
	}
	grant, err := resolver.Credentials.IssueCredentialBrokerGrant(ctx, accountID, connectorKey, connectionID)
	if err != nil {
		clear(grant)
		return contracts.RuntimeBinding{}, fmt.Errorf("issue connector credential broker grant: %w", err)
	}
	if len(grant) == 0 {
		return contracts.RuntimeBinding{}, invalidOperationReceipt("credential broker grant issuer returned an empty grant")
	}
	return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: true, AuthorizationState: projection.State, CredentialBrokerGrant: grant}, nil
}
