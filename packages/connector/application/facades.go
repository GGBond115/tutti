package application

import (
	"context"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

// StateQueries is the account-aware read boundary shared by transports and
// Agent capability projection. It exposes no persistence or worker controls.
type StateQueries interface {
	Snapshot(context.Context) (contracts.Snapshot, error)
	SnapshotForScope(context.Context, contracts.OperationScope) (contracts.Snapshot, error)
}

type CatalogQueries interface {
	ListCatalogCategories(context.Context) ([]contracts.CatalogCategory, error)
	ListCatalogPageForScope(context.Context, contracts.OperationScope, contracts.CatalogPageQuery) (contracts.CatalogPage, error)
	GetConnectorForScope(context.Context, contracts.OperationScope, string) (contracts.Connector, error)
	SnapshotViewForScope(context.Context, contracts.OperationScope) (contracts.SnapshotView, error)
	ListCatalogPageViewForScope(context.Context, contracts.OperationScope, contracts.CatalogPageQuery) (contracts.CatalogPageView, error)
	GetConnectorViewForScope(context.Context, contracts.OperationScope, string) (contracts.ConnectorView, error)
	PresentConnectorForScope(context.Context, contracts.OperationScope, contracts.Connector) (contracts.ConnectorView, error)
}

type CatalogCommands interface {
	RefreshCatalog(context.Context, contracts.Mutation) contracts.CommandResult
}

type InstallationCommands interface {
	Install(context.Context, contracts.ConnectorMutation) contracts.CommandResult
	Uninstall(context.Context, contracts.ConnectorMutation) contracts.CommandResult
}

type AuthorizationCommands interface {
	BeginAuthorization(context.Context, contracts.ConnectorMutation, []byte) contracts.AuthorizationCommandResult
	CancelAuthorization(context.Context, contracts.CancelAuthorizationCommand) contracts.CommandResult
	DisconnectAuthorization(context.Context, contracts.ConnectorMutation) contracts.CommandResult
}

type OperationQueries interface {
	GetOperationForScope(context.Context, contracts.OperationScope, string) (contracts.Operation, error)
}

type AgentConnectorPolicyQueries interface {
	Evaluate(context.Context, contracts.AgentTarget) (contracts.AgentConnectorPolicySnapshot, error)
}

// Root is the public application composition object. Callers must inject one
// returned facet, not retain a concrete application implementation.
type Root interface {
	State() StateQueries
	Catalog() CatalogQueries
	CatalogCommands() CatalogCommands
	Installations() InstallationCommands
	Authorizations() AuthorizationCommands
	AgentPolicy() AgentConnectorPolicyQueries
	Operations() OperationQueries
}

type root struct{ service *service }

type commandFacades struct{ service *service }

func (value root) State() StateQueries                 { return value.service }
func (value root) Catalog() CatalogQueries             { return value.service }
func (value root) CatalogCommands() CatalogCommands    { return commandFacades{service: value.service} }
func (value root) Installations() InstallationCommands { return commandFacades{service: value.service} }
func (value root) Authorizations() AuthorizationCommands {
	return commandFacades{service: value.service}
}
func (value root) AgentPolicy() AgentConnectorPolicyQueries { return value.service }
func (value root) Operations() OperationQueries             { return value.service }

// DaemonPorts are private lifecycle/recovery capabilities intended only for
// connector/daemon. They are split so no worker receives the entire core.
type DaemonPorts struct {
	Recovery      RecoveryControl
	Operations    OperationRecoveryControl
	Catalog       CatalogMaintenance
	Installation  InstallationMaintenance
	Authorization AuthorizationMaintenance
	Runtime       RuntimeMaintenance
}

type RecoveryControl interface {
	ExecuteOperation(context.Context, string) error
	Recover(context.Context) error
	GetOperation(context.Context, string) (contracts.Operation, error)
	Snapshot(context.Context) (contracts.Snapshot, error)
}

type OperationRecoveryControl interface {
	RecoverableOperations(context.Context) ([]contracts.Operation, error)
}

type CatalogMaintenance interface {
	RefreshCatalog(context.Context, contracts.Mutation) (contracts.MutationResult, error)
}

type InstallationMaintenance interface {
	CalibrateInstalledConnectorsForScope(context.Context, contracts.OperationScope) error
}

type AuthorizationMaintenance interface {
	ReconcileAuthorizations(context.Context, contracts.OperationScope) ([]contracts.AuthorizationReconcileIntent, error)
	InstalledRemoteAuthorizedConnectorKeys(context.Context) ([]string, error)
	ProjectAuthorization(context.Context, contracts.OperationScope, contracts.AuthorizationProjection) error
	ResolveAuthorizationSession(context.Context, string, contracts.AuthorizationSessionResolution) error
}

type RuntimeMaintenance interface {
	FenceInstalledRuntimesForScope(context.Context, contracts.OperationScope) error
	ReconcileInstalledRuntimesForScope(context.Context, contracts.OperationScope) error
	ReconcileRuntimeDesired(context.Context, contracts.OperationScope, string) error
	ReconcileRuntimeAfterInvalidation(context.Context, contracts.OperationScope, string) error
	RuntimeConvergenceSnapshot(context.Context, contracts.OperationScope) ([]contracts.RuntimeConvergence, error)
	RuntimeRetryHealth(context.Context, contracts.OperationScope) ([]contracts.RuntimeRetryHealth, error)
	RuntimeBootEpoch() string
	ResetRuntimeFailureBudget(context.Context, contracts.OperationScope, string, uint64) error
	InvalidateRuntimeObservation(context.Context, contracts.OperationScope, string, uint64) error
	DueRuntimeConvergences(context.Context, contracts.OperationScope, int) ([]contracts.RuntimeConvergence, error)
	ConvergeRuntime(context.Context, contracts.OperationScope, string) error
}

type Composition struct {
	Root   Root
	Daemon DaemonPorts
}

func New(config Config) (Composition, error) {
	core, err := newService(config)
	if err != nil {
		return Composition{}, err
	}
	return Composition{
		Root: root{service: core},
		Daemon: DaemonPorts{
			Recovery: core, Operations: core, Catalog: core, Installation: core,
			Authorization: core, Runtime: core,
		},
	}, nil
}

// Narrow adapter interfaces retained for structural consumers.
type SnapshotReader interface {
	Snapshot(context.Context) (contracts.Snapshot, error)
}

type ScopedSnapshotReader interface {
	SnapshotForScope(context.Context, contracts.OperationScope) (contracts.Snapshot, error)
}
