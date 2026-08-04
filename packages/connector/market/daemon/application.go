package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ApplicationConfig struct {
	Repository             Repository
	CatalogSource          CatalogSource
	ArtifactPreparer       ArtifactPreparer
	Host                   ImplementationHost
	Authorization          AuthorizationProvider
	Compatibility          CompatibilityEvaluator
	Scheduler              OperationScheduler
	ImplementationRegistry ImplementationRegistry
	WorkerID               string
	BootEpoch              string
	LeaseDuration          time.Duration
	Now                    func() time.Time
	NewID                  func() (string, error)
}

type Application struct {
	config ApplicationConfig
}

var _ Service = (*Application)(nil)

func NewApplication(config ApplicationConfig) (*Application, error) {
	if config.Repository == nil {
		return nil, errors.New("connector market repository is required")
	}
	if config.CatalogSource == nil {
		return nil, errors.New("connector market catalog source is required")
	}
	if config.ArtifactPreparer == nil {
		return nil, errors.New("connector market artifact preparer is required")
	}
	if config.Host == nil {
		return nil, errors.New("connector market implementation host is required")
	}
	if config.Authorization == nil {
		return nil, errors.New("connector market authorization provider is required")
	}
	if config.Compatibility == nil {
		return nil, errors.New("connector market compatibility evaluator is required")
	}
	if config.Scheduler == nil {
		return nil, errors.New("connector market operation scheduler is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = randomID
	}
	if strings.TrimSpace(config.WorkerID) == "" {
		workerID, err := config.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate connector market worker id: %w", err)
		}
		config.WorkerID = workerID
	}
	if strings.TrimSpace(config.BootEpoch) == "" {
		bootEpoch, err := config.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate connector market boot epoch: %w", err)
		}
		config.BootEpoch = bootEpoch
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 30 * time.Second
	}
	return &Application{config: config}, nil
}

func (application *Application) Snapshot(ctx context.Context, workspaceID string) (Snapshot, error) {
	return application.config.Repository.Snapshot(ctx, workspaceID)
}

func (application *Application) GetConnector(
	ctx context.Context,
	connectorKey string,
	workspaceID string,
) (Connector, error) {
	if strings.TrimSpace(connectorKey) == "" {
		return Connector{}, invalidRequest("connectorKey is required")
	}
	return application.config.Repository.Connector(ctx, connectorKey, workspaceID)
}

func (application *Application) GetOperation(ctx context.Context, operationID string) (Operation, error) {
	if strings.TrimSpace(operationID) == "" {
		return Operation{}, invalidRequest("operationID is required")
	}
	return application.config.Repository.Operation(ctx, operationID)
}

func (application *Application) RefreshCatalog(
	ctx context.Context,
	mutation Mutation,
) (MutationResult, error) {
	return application.acceptOperation(ctx, mutation, OperationKindRefreshCatalog, "")
}

func (application *Application) Install(
	ctx context.Context,
	mutation ConnectorMutation,
) (MutationResult, error) {
	if strings.TrimSpace(mutation.WorkspaceID) == "" {
		return MutationResult{}, invalidRequest("workspaceId is required for installation")
	}
	if err := application.requireFreshCatalog(ctx); err != nil {
		return MutationResult{}, err
	}
	var target InstallationState
	result, err := application.acceptConnectorOperation(
		ctx,
		mutation,
		OperationKindInstall,
		func(connector Connector) (Connector, error) {
			if connector.Compatibility.State != CompatibilityStateSupported {
				return Connector{}, NewDomainError(
					ErrorCodeIncompatible,
					"connector is not compatible with this host",
					false,
					nil,
				)
			}
			if connector.Installation.State == InstallationStateInstalled {
				target = InstallationStateUpdating
			} else {
				target = InstallationStateInstalling
			}
			if !CanTransitionInstallation(connector.Installation.State, target) {
				return Connector{}, invalidTransition("installation", string(connector.Installation.State), string(target))
			}
			connector.Installation.State = target
			connector.Installation.FailureCode = ""
			return connector, nil
		},
	)
	return result, err
}

func (application *Application) Uninstall(
	ctx context.Context,
	mutation ConnectorMutation,
) (MutationResult, error) {
	return application.acceptConnectorOperation(
		ctx,
		mutation,
		OperationKindUninstall,
		func(connector Connector) (Connector, error) {
			if connector.Installation.InstalledReleaseDigest == "" {
				return Connector{}, invalidTransition(
					"installation",
					string(connector.Installation.State),
					string(InstallationStateUninstalling),
				)
			}
			if !CanTransitionInstallation(connector.Installation.State, InstallationStateUninstalling) {
				return Connector{}, invalidTransition(
					"installation",
					string(connector.Installation.State),
					string(InstallationStateUninstalling),
				)
			}
			connector.Installation.State = InstallationStateUninstalling
			connector.Installation.FailureCode = ""
			return connector, nil
		},
	)
}

func (application *Application) BeginAuthorization(
	ctx context.Context,
	mutation ConnectorMutation,
) (AuthorizationResult, error) {
	if strings.TrimSpace(mutation.WorkspaceID) == "" {
		return AuthorizationResult{}, invalidRequest("workspaceId is required for authorization")
	}
	if err := application.requireFreshCatalog(ctx); err != nil {
		return AuthorizationResult{}, err
	}
	accepted, err := application.acceptConnectorOperation(
		ctx,
		mutation,
		OperationKindStartAuthorization,
		func(connector Connector) (Connector, error) {
			if !CanTransitionAuthorization(connector.Authorization.State, AuthorizationStatePending) {
				return Connector{}, invalidTransition(
					"authorization",
					string(connector.Authorization.State),
					string(AuthorizationStatePending),
				)
			}
			connector.Authorization = Authorization{State: AuthorizationStatePending}
			return connector, nil
		},
	)
	if err != nil {
		return AuthorizationResult{}, err
	}
	if accepted.Operation.State == OperationStateFailed {
		return AuthorizationResult{}, NewDomainError(
			ErrorCodeAuthorizationFailed,
			"connector authorization attempt previously failed",
			true,
			nil,
		)
	}

	session, err := application.beginAuthorizationSession(ctx, accepted.Operation)
	if err != nil {
		if accepted.Operation.State != OperationStateCompleted {
			_ = application.failOperation(ctx, accepted.Operation.OperationID, ErrorCodeAuthorizationFailed)
		}
		return AuthorizationResult{}, err
	}
	operation, err := application.config.Repository.Operation(ctx, accepted.Operation.OperationID)
	if err != nil {
		return AuthorizationResult{}, err
	}
	connector, err := application.config.Repository.Connector(ctx, mutation.ConnectorKey, "")
	if err != nil {
		return AuthorizationResult{}, err
	}
	return AuthorizationResult{
		Connector:        connector,
		Operation:        operation,
		AuthorizationURL: session.AuthorizationURL,
		Revision:         connector.Revision,
	}, nil
}

func (application *Application) DisconnectAuthorization(
	ctx context.Context,
	mutation ConnectorMutation,
) (MutationResult, error) {
	return application.acceptConnectorOperation(
		ctx,
		mutation,
		OperationKindDisconnectAuthorization,
		func(connector Connector) (Connector, error) {
			if connector.Authorization.State == AuthorizationStateNotRequired {
				return Connector{}, invalidTransition(
					"authorization",
					string(connector.Authorization.State),
					string(AuthorizationStateDisconnected),
				)
			}
			return connector, nil
		},
	)
}

func (application *Application) SetWorkspaceEnabled(
	ctx context.Context,
	command SetWorkspaceEnabledCommand,
) (WorkspaceBindingResult, error) {
	if command.Enabled {
		if err := application.requireFreshCatalog(ctx); err != nil {
			return WorkspaceBindingResult{}, err
		}
	}
	if err := validateConnectorMutation(command.ConnectorMutation); err != nil {
		return WorkspaceBindingResult{}, err
	}
	if strings.TrimSpace(command.WorkspaceID) == "" {
		return WorkspaceBindingResult{}, invalidRequest("workspaceId is required")
	}
	var result WorkspaceBindingResult
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		existing, err := tx.OperationByClientRequestID(command.ClientRequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := verifyIdempotentOperation(*existing, OperationKindSetWorkspaceEnabled, command.ConnectorKey, command.WorkspaceID, &command.Enabled); err != nil {
				return err
			}
			connector, err := tx.Connector(command.ConnectorKey)
			if err != nil {
				return err
			}
			result = WorkspaceBindingResult{Connector: connector, Operation: *existing, Revision: tx.Revision()}
			return nil
		}
		if err := verifyRevision(tx, command.ExpectedRevision); err != nil {
			return err
		}
		if err := rejectActiveOperation(tx, command.ConnectorKey); err != nil {
			return err
		}
		now := application.config.Now().UTC()
		revision := tx.AdvanceRevision()
		operationID, err := application.config.NewID()
		if err != nil {
			return NewDomainError(ErrorCodeUnavailable, "connector operation id could not be generated", true, err)
		}
		connector, err := tx.Connector(command.ConnectorKey)
		if err != nil {
			return err
		}
		enabled := command.Enabled
		operation := Operation{
			OperationID:      operationID,
			ClientRequestID:  command.ClientRequestID,
			ConnectorKey:     command.ConnectorKey,
			Kind:             OperationKindSetWorkspaceEnabled,
			State:            OperationStateAccepted,
			Stage:            OperationStageAccepted,
			Target:           operationTarget(OperationKindSetWorkspaceEnabled, connector),
			WorkspaceID:      command.WorkspaceID,
			WorkspaceEnabled: &enabled,
			HostGeneration:   HostGeneration{BootEpoch: application.config.BootEpoch, Generation: revision},
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(ChangedEvent{
			ConnectorKey: connector.Key,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = WorkspaceBindingResult{Connector: connector, Operation: operation, Revision: revision}
		return nil
	})
	if err != nil {
		return WorkspaceBindingResult{}, err
	}
	if result.Operation.State == OperationStateAccepted || result.Operation.State == OperationStateRunning {
		if err := application.config.Scheduler.Schedule(ctx, result.Operation.OperationID); err != nil {
			return WorkspaceBindingResult{}, NewDomainError(ErrorCodeUnavailable, "connector workspace reconcile could not be scheduled", true, err)
		}
	}
	return result, nil
}

func (application *Application) ExecuteOperation(ctx context.Context, operationID string) error {
	now := application.config.Now().UTC()
	operation, claimed, err := application.config.Repository.ClaimOperation(
		ctx,
		operationID,
		application.config.WorkerID,
		now,
		now.Add(application.config.LeaseDuration),
	)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	defer func() {
		_ = application.config.Repository.ReleaseOperationLease(
			context.WithoutCancel(ctx),
			operationID,
			application.config.WorkerID,
		)
	}()
	if operation.State == OperationStateCompleted || operation.State == OperationStateFailed {
		return nil
	}
	operation, err = application.markOperationRunning(ctx, operation.OperationID)
	if err != nil {
		return err
	}

	var executeErr error
	switch operation.Kind {
	case OperationKindRefreshCatalog:
		executeErr = application.executeRefresh(ctx, operation)
	case OperationKindInstall:
		executeErr = application.executeInstall(ctx, operation)
	case OperationKindUninstall:
		executeErr = application.executeUninstall(ctx, operation)
	case OperationKindDisconnectAuthorization:
		executeErr = application.executeDisconnectAuthorization(ctx, operation)
	case OperationKindStartAuthorization:
		_, executeErr = application.beginAuthorizationSession(ctx, operation)
	case OperationKindSetWorkspaceEnabled:
		executeErr = application.executeWorkspaceReconcile(ctx, operation)
	default:
		executeErr = invalidRequest(fmt.Sprintf("operation kind %q is not executable", operation.Kind))
	}
	if executeErr != nil {
		code := ErrorCodeInstallFailed
		if operation.Kind == OperationKindRefreshCatalog {
			code = ErrorCodeUpstreamUnavailable
		}
		if operation.Kind == OperationKindStartAuthorization ||
			operation.Kind == OperationKindDisconnectAuthorization {
			code = ErrorCodeAuthorizationFailed
		}
		_ = application.failOperation(ctx, operation.OperationID, code)
		return executeErr
	}
	return nil
}

func (application *Application) requireFreshCatalog(ctx context.Context) error {
	state, err := application.config.Repository.CatalogTrustState(ctx)
	if err != nil {
		return NewDomainError(ErrorCodeUnavailable, "connector catalog trust state is unavailable", true, err)
	}
	if !state.Fresh(application.config.Now().UTC(), 30*time.Second) {
		return NewDomainError(ErrorCodeUpstreamUnavailable, "connector catalog trust is stale", true, nil)
	}
	return nil
}

func (application *Application) Recover(ctx context.Context) error {
	operations, err := application.config.Repository.RecoverableOperations(ctx)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.LeaseExpiresAt != nil && operation.LeaseExpiresAt.After(application.config.Now().UTC()) &&
			operation.LeaseOwner != "" && operation.LeaseOwner != application.config.WorkerID {
			delay := operation.LeaseExpiresAt.Sub(application.config.Now().UTC())
			operationID := operation.OperationID
			go func() {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					_ = application.config.Scheduler.Schedule(ctx, operationID)
				}
			}()
			continue
		}
		if err := application.config.Scheduler.Schedule(ctx, operation.OperationID); err != nil {
			return NewDomainError(ErrorCodeUnavailable, "connector operation recovery could not be scheduled", true, err)
		}
	}
	return nil
}

// ReconcileDurableBindings rebuilds the daemon-owned runtime projection from
// committed workspace intent after every daemon restart. A successful install
// is only an artifact fact; enabled bindings are the authoritative source for
// MCP routes and CLI capabilities.
func (application *Application) ReconcileDurableBindings(ctx context.Context) error {
	if application == nil {
		return NewDomainError(ErrorCodeUnavailable, "connector application is unavailable", false, nil)
	}
	snapshot, err := application.config.Repository.Snapshot(ctx, "")
	if err != nil {
		return err
	}
	if err := application.requireFreshCatalog(ctx); err != nil {
		return err
	}
	installedReleases := make(map[string]Release)
	effectiveIntent := make(map[string]Operation)
	for _, operation := range snapshot.Operations {
		if operation.Kind == OperationKindInstall && operation.State == OperationStateCompleted &&
			operation.Target != nil && operation.Target.Release != nil {
			installedReleases[operation.ConnectorKey+"\x00"+operation.Target.ReleaseDigest] = *operation.Target.Release
		}
		if operation.Kind != OperationKindSetWorkspaceEnabled || operation.WorkspaceEnabled == nil ||
			(operation.State != OperationStateAccepted && operation.State != OperationStateRunning) {
			continue
		}
		key := operation.ConnectorKey + "\x00" + operation.WorkspaceID
		previous, exists := effectiveIntent[key]
		if !exists || operation.CreatedAt.After(previous.CreatedAt) {
			effectiveIntent[key] = operation
		}
	}
	for _, connector := range snapshot.Connectors {
		if connector.Installation.State != InstallationStateInstalled {
			continue
		}
		installedRelease, ok := installedReleases[connector.Key+"\x00"+connector.Installation.InstalledReleaseDigest]
		if !ok && connector.Release.ReleaseDigest == connector.Installation.InstalledReleaseDigest {
			installedRelease = connector.Release
			ok = true
		}
		if !ok || installedRelease.Status != ReleaseStatusAvailable {
			return NewDomainError(ErrorCodeUnavailable, "installed connector release evidence is unavailable", false, nil)
		}
		installedConnector := connector
		installedConnector.Release = installedRelease
		bindings, err := application.config.Repository.WorkspaceBindings(ctx, connector.Key)
		if err != nil {
			return err
		}
		generation := connector.Revision
		if generation == 0 {
			generation = 1
		}
		for _, binding := range bindings {
			desired := binding.Enabled
			if pending, exists := effectiveIntent[connector.Key+"\x00"+binding.WorkspaceID]; exists {
				desired = *pending.WorkspaceEnabled
			}
			if !desired {
				if err := application.config.Host.Revoke(ctx, SecurityRevocationRequest{
					WorkspaceID: binding.WorkspaceID, ConnectorKey: connector.Key,
					ReleaseDigest: connector.Installation.InstalledReleaseDigest,
					Generation:    HostGeneration{BootEpoch: application.config.BootEpoch, Generation: ^uint64(0)},
					Deadline:      application.config.Now().UTC().Add(5 * time.Second),
				}); err != nil {
					return NewDomainError(ErrorCodeUnavailable, "connector pending disable intent could not be fenced", false, err)
				}
				continue
			}
			operationID := "reconcile/" + application.config.BootEpoch + "/" + connector.Key + "/" + binding.WorkspaceID
			receipt, err := application.config.Host.Reconcile(ctx, WorkspaceReconcileRequest{
				OperationID: operationID, WorkspaceID: binding.WorkspaceID,
				Connector: installedConnector, Enabled: true,
				Generation: HostGeneration{BootEpoch: application.config.BootEpoch, Generation: generation},
			})
			if err != nil {
				return NewDomainError(ErrorCodeUnavailable, "connector durable workspace intent could not be reconciled", true, err)
			}
			if err := validateWorkspaceRuntimeReceipt(receipt, operationID, binding.WorkspaceID, connector.Key,
				installedRelease.ReleaseDigest, HostGeneration{BootEpoch: application.config.BootEpoch, Generation: generation}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (application *Application) acceptConnectorOperation(
	ctx context.Context,
	mutation ConnectorMutation,
	kind OperationKind,
	transition func(Connector) (Connector, error),
) (MutationResult, error) {
	if err := validateConnectorMutation(mutation); err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		existing, err := tx.OperationByClientRequestID(mutation.ClientRequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := verifyIdempotentOperation(*existing, kind, mutation.ConnectorKey, mutation.WorkspaceID, nil); err != nil {
				return err
			}
			connector, err := tx.Connector(mutation.ConnectorKey)
			if err != nil {
				return err
			}
			result = MutationResult{Connector: &connector, Operation: *existing, Revision: tx.Revision()}
			return nil
		}
		if err := verifyRevision(tx, mutation.ExpectedRevision); err != nil {
			return err
		}
		if err := rejectActiveOperation(tx, mutation.ConnectorKey); err != nil {
			return err
		}
		connector, err := tx.Connector(mutation.ConnectorKey)
		if err != nil {
			return err
		}
		connector, err = transition(connector)
		if err != nil {
			return err
		}
		now := application.config.Now().UTC()
		revision := tx.AdvanceRevision()
		operationID, err := application.config.NewID()
		if err != nil {
			return NewDomainError(ErrorCodeUnavailable, "connector operation id could not be generated", true, err)
		}
		connector.Revision = revision
		operation := Operation{
			OperationID:     operationID,
			ClientRequestID: mutation.ClientRequestID,
			ConnectorKey:    mutation.ConnectorKey,
			Kind:            kind,
			State:           OperationStateAccepted,
			Stage:           OperationStageAccepted,
			Target:          operationTarget(kind, connector),
			WorkspaceID:     mutation.WorkspaceID,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(ChangedEvent{
			ConnectorKey: connector.Key,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = MutationResult{Connector: &connector, Operation: operation, Revision: revision}
		return nil
	})
	if err != nil {
		return MutationResult{}, err
	}
	if kind != OperationKindStartAuthorization &&
		(result.Operation.State == OperationStateAccepted || result.Operation.State == OperationStateRunning) {
		if err := application.config.Scheduler.Schedule(ctx, result.Operation.OperationID); err != nil {
			return MutationResult{}, NewDomainError(ErrorCodeUnavailable, "connector operation could not be scheduled", true, err)
		}
	}
	return result, nil
}

func (application *Application) acceptOperation(
	ctx context.Context,
	mutation Mutation,
	kind OperationKind,
	connectorKey string,
) (MutationResult, error) {
	if err := validateMutation(mutation); err != nil {
		return MutationResult{}, err
	}
	var result MutationResult
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		existing, err := tx.OperationByClientRequestID(mutation.ClientRequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := verifyIdempotentOperation(*existing, kind, connectorKey, "", nil); err != nil {
				return err
			}
			result = MutationResult{Operation: *existing, Revision: tx.Revision()}
			return nil
		}
		if err := verifyRevision(tx, mutation.ExpectedRevision); err != nil {
			return err
		}
		if err := rejectActiveOperation(tx, connectorKey); err != nil {
			return err
		}
		now := application.config.Now().UTC()
		revision := tx.AdvanceRevision()
		operationID, err := application.config.NewID()
		if err != nil {
			return NewDomainError(ErrorCodeUnavailable, "connector operation id could not be generated", true, err)
		}
		operation := Operation{
			OperationID:     operationID,
			ClientRequestID: mutation.ClientRequestID,
			ConnectorKey:    connectorKey,
			Kind:            kind,
			State:           OperationStateAccepted,
			Stage:           OperationStageAccepted,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if kind == OperationKindRefreshCatalog {
			if err := tx.SetCatalogState(CatalogStateRefreshing); err != nil {
				return err
			}
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(ChangedEvent{
			ConnectorKey: connectorKey,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = MutationResult{Operation: operation, Revision: revision}
		return nil
	})
	if err != nil {
		return MutationResult{}, err
	}
	if result.Operation.State == OperationStateAccepted || result.Operation.State == OperationStateRunning {
		if err := application.config.Scheduler.Schedule(ctx, result.Operation.OperationID); err != nil {
			return MutationResult{}, NewDomainError(ErrorCodeUnavailable, "connector operation could not be scheduled", true, err)
		}
	}
	return result, nil
}

func validateMutation(mutation Mutation) error {
	if strings.TrimSpace(mutation.ClientRequestID) == "" {
		return invalidRequest("clientRequestId is required")
	}
	return nil
}

func validateConnectorMutation(mutation ConnectorMutation) error {
	if err := validateMutation(mutation.Mutation); err != nil {
		return err
	}
	if strings.TrimSpace(mutation.ConnectorKey) == "" {
		return invalidRequest("connectorKey is required")
	}
	return nil
}

func verifyRevision(tx Transaction, expected uint64) error {
	if tx.Revision() == expected {
		return nil
	}
	return NewDomainError(
		ErrorCodeRevisionConflict,
		fmt.Sprintf("expected revision %d but current revision is %d", expected, tx.Revision()),
		true,
		nil,
	)
}

func verifyIdempotentOperation(operation Operation, kind OperationKind, connectorKey, workspaceID string, enabled *bool) error {
	if operation.Kind != kind || operation.ConnectorKey != connectorKey || operation.WorkspaceID != workspaceID {
		return invalidRequest("clientRequestId was already used for a different connector-market command")
	}
	if enabled != nil && (operation.WorkspaceEnabled == nil || *operation.WorkspaceEnabled != *enabled) {
		return invalidRequest("clientRequestId was already used for a different connector-market command")
	}
	return nil
}

func rejectActiveOperation(tx Transaction, connectorKey string) error {
	active, err := tx.ActiveOperation(connectorKey)
	if err != nil {
		return err
	}
	if active == nil {
		return nil
	}
	return NewDomainError(
		ErrorCodeOperationInProgress,
		fmt.Sprintf("operation %s is already in progress", active.OperationID),
		true,
		nil,
	)
}

func invalidRequest(message string) error {
	return NewDomainError(ErrorCodeInvalidRequest, message, false, nil)
}

func invalidTransition(kind, from, to string) error {
	return NewDomainError(
		ErrorCodeOperationInProgress,
		fmt.Sprintf("%s cannot transition from %s to %s", kind, from, to),
		true,
		nil,
	)
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func operationTarget(kind OperationKind, connector Connector) *OperationTarget {
	if kind == OperationKindInstall || kind == OperationKindStartAuthorization ||
		kind == OperationKindDisconnectAuthorization || kind == OperationKindSetWorkspaceEnabled {
		release := connector.Release
		return &OperationTarget{
			ConnectorKey:   release.ConnectorKey,
			Version:        release.Version,
			ReleaseID:      release.ReleaseID,
			ReleaseDigest:  release.ReleaseDigest,
			ArtifactSHA256: release.Artifact.SHA256,
			Release:        &release,
		}
	}
	if kind == OperationKindUninstall {
		return &OperationTarget{
			ConnectorKey:  connector.Key,
			Version:       connector.Installation.InstalledVersion,
			ReleaseID:     connector.Installation.InstalledReleaseID,
			ReleaseDigest: connector.Installation.InstalledReleaseDigest,
		}
	}
	return nil
}
