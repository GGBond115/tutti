package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) BeginAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	secret []byte,
) (contracts.AuthorizationResult, error) {
	defer clear(secret)
	if err := validateAuthorizationMutation(mutation); err != nil {
		return contracts.AuthorizationResult{}, err
	}
	requestExecution := application.acquireAuthorizationRequest(ctx, mutation)
	defer application.releaseAuthorizationRequest(mutation.AccountID, mutation.ConnectorKey, requestExecution)
	unlock := application.lockAuthorization(mutation.AccountID, mutation.ConnectorKey)
	defer unlock()
	if err := requestExecution.context.Err(); err != nil {
		return contracts.AuthorizationResult{}, err
	}
	current, err := application.config.Repository.Connector(requestExecution.context, mutation.ConnectorKey)
	if err != nil {
		return contracts.AuthorizationResult{}, err
	}
	remote := current.Release.Manifest.Implementation.RemoteStreamableHTTP != nil
	accountID := strings.TrimSpace(mutation.AccountID)
	accountScoped := accountID != ""
	if remote && !accountScoped {
		return contracts.AuthorizationResult{}, invalidRequest("accountId is required for remote connector authorization")
	}
	idempotentReplay, err := application.isIdempotentConnectorOperation(
		requestExecution.context,
		mutation,
		contracts.OperationKindStartAuthorization,
	)
	if err != nil {
		return contracts.AuthorizationResult{}, err
	}
	if !idempotentReplay {
		if mutation.ReplacementPolicy == contracts.AuthorizationReplacementPolicyReplaceActive && !requestExecution.replacedLive {
			if err := application.verifyConnectorMutationRevision(requestExecution.context, mutation); err != nil {
				return contracts.AuthorizationResult{}, err
			}
		}
		unresolved, unresolvedErr := application.config.Repository.UnresolvedAuthorizationSessionOperations(
			requestExecution.context, contracts.OperationScope{AccountID: accountID},
		)
		if unresolvedErr != nil {
			return contracts.AuthorizationResult{}, unresolvedErr
		}
		for _, receipt := range unresolved {
			if receipt.ConnectorKey == mutation.ConnectorKey {
				if mutation.ReplacementPolicy == contracts.AuthorizationReplacementPolicyReplaceActive {
					if replaceErr := application.cancelAuthorizationAttempt(
						requestExecution.context, current, receipt, true,
					); replaceErr != nil {
						return contracts.AuthorizationResult{}, replaceErr
					}
					continue
				}
				return contracts.AuthorizationResult{}, contracts.NewDomainError(
					contracts.ErrorCodeOperationInProgress, "connector authorization is already pending", true, nil,
				)
			}
		}
		if mutation.ReplacementPolicy == contracts.AuthorizationReplacementPolicyReplaceActive {
			if err := application.resetPendingAuthorizationState(
				requestExecution.context,
				contracts.OperationScope{AccountID: accountID},
				mutation.ConnectorKey,
			); err != nil {
				return contracts.AuthorizationResult{}, err
			}
			current, err = application.config.Repository.Connector(requestExecution.context, mutation.ConnectorKey)
			if err != nil {
				return contracts.AuthorizationResult{}, err
			}
			currentRevision := current.Revision
			mutation.ExpectedConnectorRevision = &currentRevision
		}
	}
	if accountScoped && !idempotentReplay {
		projection, projectionErr := application.GetAuthorizationProjection(requestExecution.context, accountID, mutation.ConnectorKey)
		if projectionErr != nil && !errors.Is(projectionErr, contracts.ErrNotFound) {
			return contracts.AuthorizationResult{}, projectionErr
		}
		if projectionErr == nil && projection.State != contracts.AuthorizationStateDisconnected &&
			projection.State != contracts.AuthorizationStateExpired && projection.State != contracts.AuthorizationStateFailed {
			return contracts.AuthorizationResult{}, invalidTransition(
				"authorization", string(projection.State), string(contracts.AuthorizationStatePending),
			)
		}
	}
	accepted, err := application.acceptConnectorOperation(
		requestExecution.context,
		mutation,
		contracts.OperationKindStartAuthorization,
		func(connector contracts.Connector) (contracts.Connector, error) {
			if remote {
				return connector, nil
			}
			// Account-scoped authorization may reuse an already connected local
			// credential broker. Keep device truth intact while the provider binds
			// that credential to the current account projection.
			if accountScoped && connector.Authorization.State == contracts.AuthorizationStateConnected {
				return connector, nil
			}
			if !contracts.CanTransitionAuthorization(connector.Authorization.State, contracts.AuthorizationStatePending) {
				return contracts.Connector{}, invalidTransition(
					"authorization",
					string(connector.Authorization.State),
					string(contracts.AuthorizationStatePending),
				)
			}
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
			return connector, nil
		},
	)
	if err != nil {
		return contracts.AuthorizationResult{}, err
	}
	if accepted.Operation.State == contracts.OperationStateFailed {
		return contracts.AuthorizationResult{}, contracts.NewDomainError(
			contracts.ErrorCodeAuthorizationFailed,
			"connector authorization attempt previously failed",
			true,
			nil,
		)
	}

	session, err := application.beginAuthorizationSession(
		requestExecution.context, accepted.Operation, secret, mutation.ReplacementPolicy,
	)
	if err != nil {
		// Starting authorization has no durable provider receipt yet. Keep retry
		// under explicit user control instead of leaving a running operation for
		// the recovery worker to replay continuously.
		if accepted.Operation.State != contracts.OperationStateCompleted {
			terminalContext, cancelTerminal := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = application.failOperation(terminalContext, accepted.Operation.OperationID, contracts.ErrorCodeAuthorizationFailed)
			cancelTerminal()
		}
		return contracts.AuthorizationResult{}, err
	}
	operation, err := application.config.Repository.Operation(requestExecution.context, accepted.Operation.OperationID)
	if err != nil {
		return contracts.AuthorizationResult{}, err
	}
	connector, err := application.config.Repository.Connector(requestExecution.context, mutation.ConnectorKey)
	if err != nil {
		return contracts.AuthorizationResult{}, err
	}
	if accountScoped {
		projection, projectionErr := application.GetAuthorizationProjection(requestExecution.context, accountID, mutation.ConnectorKey)
		if errors.Is(projectionErr, contracts.ErrNotFound) {
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
		} else if projectionErr != nil {
			return contracts.AuthorizationResult{}, projectionErr
		} else {
			connector.Authorization = contracts.Authorization{State: projection.State, FailureCode: projection.FailureCode}
		}
		// The account projection remains the durable authorization truth, but a
		// redirect session can be pending before the control plane publishes its
		// first changed projection. Preserve that in-flight state in this command
		// result so callers keep following the same idempotent session instead of
		// treating the initial disconnected projection as terminal.
		if !session.IsResolved() && session.State == contracts.AuthorizationStatePending && connector.Authorization.State != contracts.AuthorizationStateConnected {
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
		}
	}
	return contracts.AuthorizationResult{
		Connector:              connector,
		Operation:              operation,
		AuthorizationURL:       session.AuthorizationURL,
		AuthorizationView:      authorizationViewForSession(connector.Release, session),
		AuthorizationExpiresAt: session.ExpiresAt,
		Revision:               connector.Revision,
	}, nil
}

// CancelAuthorization terminates the unresolved local authorization attempt.
// It does not disconnect an already-authorized account connection.
func (application *service) CancelAuthorization(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) error {
	connectorKey = strings.TrimSpace(connectorKey)
	scope.AccountID = strings.TrimSpace(scope.AccountID)
	if connectorKey == "" {
		return invalidRequest("connectorKey is required")
	}
	application.interruptAuthorizationRequest(scope.AccountID, connectorKey)
	unlock := application.lockAuthorization(scope.AccountID, connectorKey)
	defer unlock()
	connector, err := application.config.Repository.Connector(ctx, connectorKey)
	if err != nil {
		return err
	}
	operations, err := application.config.Repository.UnresolvedAuthorizationSessionOperations(ctx, scope)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.ConnectorKey != connectorKey {
			continue
		}
		if err := application.cancelAuthorizationAttempt(ctx, connector, operation, false); err != nil {
			return err
		}
	}
	return application.resetPendingAuthorizationState(ctx, scope, connectorKey)
}

func (application *service) lockAuthorization(accountID, connectorKey string) func() {
	key := authorizationLaneKey(accountID, connectorKey)
	application.authorizationMu.Lock()
	lane := application.authorizationLanes[key]
	if lane == nil {
		lane = &sync.Mutex{}
		application.authorizationLanes[key] = lane
	}
	application.authorizationMu.Unlock()
	lane.Lock()
	return lane.Unlock
}

func authorizationLaneKey(accountID, connectorKey string) string {
	return strings.TrimSpace(accountID) + "\x00" + strings.TrimSpace(connectorKey)
}

func (application *service) acquireAuthorizationRequest(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) *authorizationRequestExecution {
	key := authorizationLaneKey(mutation.AccountID, mutation.ConnectorKey)
	clientRequestID := strings.TrimSpace(mutation.ClientRequestID)
	application.authorizationMu.Lock()
	defer application.authorizationMu.Unlock()
	if current := application.authorizationRequests[key]; current != nil {
		if current.clientRequestID == clientRequestID {
			current.references++
			return current
		}
		if mutation.ReplacementPolicy != contracts.AuthorizationReplacementPolicyReplaceActive {
			requestContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
			return &authorizationRequestExecution{
				clientRequestID: clientRequestID, context: requestContext, cancel: cancel, references: 1,
			}
		}
		current.cancel()
		requestContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
		execution := &authorizationRequestExecution{
			clientRequestID: clientRequestID, context: requestContext, cancel: cancel, references: 1, replacedLive: true,
		}
		application.authorizationRequests[key] = execution
		return execution
	}
	requestContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	execution := &authorizationRequestExecution{
		clientRequestID: clientRequestID, context: requestContext, cancel: cancel, references: 1,
	}
	application.authorizationRequests[key] = execution
	return execution
}

func (application *service) releaseAuthorizationRequest(
	accountID, connectorKey string,
	execution *authorizationRequestExecution,
) {
	if execution == nil {
		return
	}
	key := authorizationLaneKey(accountID, connectorKey)
	application.authorizationMu.Lock()
	if application.authorizationRequests[key] == execution {
		execution.references--
		if execution.references == 0 {
			delete(application.authorizationRequests, key)
			execution.cancel()
		}
	} else {
		execution.cancel()
	}
	application.authorizationMu.Unlock()
}

func (application *service) interruptAuthorizationRequest(accountID, connectorKey string) {
	key := authorizationLaneKey(accountID, connectorKey)
	application.authorizationMu.Lock()
	if execution := application.authorizationRequests[key]; execution != nil {
		execution.cancel()
	}
	application.authorizationMu.Unlock()
}

func (application *service) cancelAuthorizationAttempt(
	ctx context.Context,
	connector contracts.Connector,
	operation contracts.Operation,
	requireProviderTermination bool,
) error {
	session := operation.Execution.AuthorizationSession
	if session == nil || session.IsResolved() {
		return nil
	}
	canceler, ok := application.config.Authorization.(AuthorizationAttemptCanceler)
	if !ok {
		if requireProviderTermination {
			return contracts.NewDomainError(
				contracts.ErrorCodeUnavailable,
				"connector authorization provider cannot safely replace an active attempt",
				false,
				nil,
			)
		}
		return application.config.Repository.ResolveAuthorizationSession(
			ctx, operation.OperationID, contracts.AuthorizationSessionResolutionSuperseded,
		)
	}
	if session.Resolution != contracts.AuthorizationSessionResolutionCanceling {
		if err := application.config.Repository.ResolveAuthorizationSession(
			ctx, operation.OperationID, contracts.AuthorizationSessionResolutionCanceling,
		); err != nil {
			return err
		}
	}
	release, err := frozenRelease(operation)
	if err != nil {
		return err
	}
	connector.Release = release
	if err := canceler.Cancel(ctx, contracts.AuthorizationCancelRequest{
		OperationID: operation.OperationID,
		Scope:       operation.Scope,
		Connector:   connector,
		Release:     release,
		Session:     *session,
	}); err != nil {
		return contracts.NewDomainError(
			contracts.ErrorCodeUnavailable,
			"connector authorization attempt could not be terminated",
			true,
			err,
		)
	}
	return application.config.Repository.ResolveAuthorizationSession(
		ctx, operation.OperationID, contracts.AuthorizationSessionResolutionSuperseded,
	)
}

func (application *service) resetPendingAuthorizationState(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) error {
	if err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		stored, err := tx.Connector(connectorKey)
		if err != nil || stored.Authorization.State != contracts.AuthorizationStatePending {
			return err
		}
		revision := tx.AdvanceRevision()
		stored.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
		stored.Revision = revision
		if err := tx.SaveConnector(stored); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: connectorKey, Revision: revision})
	}); err != nil {
		return err
	}
	if scope.AccountID == "" || application.config.AuthorizationProjections == nil {
		return nil
	}
	projection, err := application.GetAuthorizationProjection(ctx, scope.AccountID, connectorKey)
	if errors.Is(err, contracts.ErrNotFound) || err == nil && projection.State != contracts.AuthorizationStatePending {
		return nil
	}
	if err != nil {
		return err
	}
	projection.State = contracts.AuthorizationStateDisconnected
	projection.ConnectionID = ""
	projection.FailureCode = ""
	projection.UpdatedAt = application.config.Now().UTC()
	return application.saveAuthorizationProjection(ctx, contracts.ConnectorMutation{
		ConnectorKey: connectorKey, AccountID: scope.AccountID,
	}, projection)
}

// ReconcileAuthorizations observes unresolved private start receipts for one
// explicit account. Remote Connector truth comes from the account projection;
// the device-scoped Connector authorization field is only used by local
// Connectors. It is safe to call repeatedly and after a daemon restart.
func (application *service) ReconcileAuthorizations(ctx context.Context, scope contracts.OperationScope) ([]contracts.AuthorizationReconcileIntent, error) {
	observer, canObserve := application.config.Authorization.(AuthorizationObserver)
	operations, err := application.config.Repository.UnresolvedAuthorizationSessionOperations(ctx, scope)
	if err != nil {
		return nil, err
	}
	intents := make([]contracts.AuthorizationReconcileIntent, 0, len(operations))
	var reconcileErr error
	for _, operation := range operations {
		if operation.Execution.AuthorizationSession == nil {
			continue
		}
		connector, connectorErr := application.config.Repository.Connector(ctx, operation.ConnectorKey)
		if connectorErr != nil {
			reconcileErr = errors.Join(reconcileErr, connectorErr)
			continue
		}
		release, releaseErr := frozenRelease(operation)
		if releaseErr != nil {
			reconcileErr = errors.Join(reconcileErr, releaseErr)
			continue
		}
		session := *operation.Execution.AuthorizationSession
		if session.Resolution == contracts.AuthorizationSessionResolutionCanceling {
			if cancelErr := application.cancelAuthorizationAttempt(ctx, connector, operation, true); cancelErr != nil {
				reconcileErr = errors.Join(reconcileErr, cancelErr)
			}
			continue
		}
		if !canObserve {
			continue
		}
		remote := release.Manifest.Implementation.RemoteStreamableHTTP != nil
		if remote {
			projection, projectionErr := application.GetAuthorizationProjection(ctx, scope.AccountID, connector.Key)
			if projectionErr == nil && projection.State == contracts.AuthorizationStateConnected {
				intents = append(intents, contracts.AuthorizationReconcileIntent{OperationID: operation.OperationID,
					ConnectorKey: connector.Key, Resolution: contracts.AuthorizationSessionResolutionAccountStateConverged})
				continue
			}
			if projectionErr != nil && !errors.Is(projectionErr, contracts.ErrNotFound) {
				reconcileErr = errors.Join(reconcileErr, projectionErr)
				continue
			}
		} else if connector.Authorization.State != contracts.AuthorizationStatePending {
			continue
		}
		observation := contracts.AuthorizationObservation{}
		expiresAt := session.ExpiresAt
		if expiresAt.IsZero() {
			expiresAt = operation.UpdatedAt
			if expiresAt.IsZero() {
				expiresAt = operation.CreatedAt
			}
			if !expiresAt.IsZero() {
				expiresAt = expiresAt.Add(authorizationSessionTTL)
			}
		}
		if !expiresAt.IsZero() && !expiresAt.After(application.config.Now().UTC()) {
			observation = contracts.AuthorizationObservation{
				State:       contracts.AuthorizationObservationFailed,
				FailureCode: "connector_authorization_timeout",
			}
		} else {
			var observeErr error
			observation, observeErr = observer.Observe(ctx, contracts.AuthorizationObserveRequest{
				Scope: operation.Scope, Connector: connector, Release: release, Session: session,
			})
			if observeErr != nil {
				reconcileErr = errors.Join(reconcileErr, observeErr)
				continue
			}
		}
		if observation.State == contracts.AuthorizationObservationPending {
			continue
		}
		if observation.State != contracts.AuthorizationObservationConnected && observation.State != contracts.AuthorizationObservationFailed {
			reconcileErr = errors.Join(reconcileErr, errors.New("authorization observer returned an invalid state"))
			continue
		}
		currentOperation, currentErr := application.config.Repository.Operation(ctx, operation.OperationID)
		if currentErr != nil {
			reconcileErr = errors.Join(reconcileErr, currentErr)
			continue
		}
		currentSession := currentOperation.Execution.AuthorizationSession
		if currentSession == nil || currentSession.SessionID != session.SessionID ||
			currentSession.Resolution == contracts.AuthorizationSessionResolutionCanceling || currentSession.IsResolved() {
			continue
		}
		resolution := authorizationSessionResolutionForObservation(observation)
		if !remote {
			if completeErr := application.completeAuthorizationObservation(ctx, connector.Key, observation); completeErr != nil {
				reconcileErr = errors.Join(reconcileErr, completeErr)
				continue
			}
		}
		projectionState := contracts.AuthorizationStateConnected
		if observation.State == contracts.AuthorizationObservationFailed {
			projectionState = contracts.AuthorizationStateFailed
		}
		if _, err := application.projectAuthorization(ctx, operation.Scope, connector.Key,
			observation.ConnectionID, projectionState, observation.FailureCode); err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
			continue
		}
		intents = append(intents, contracts.AuthorizationReconcileIntent{OperationID: operation.OperationID,
			ConnectorKey: connector.Key, Resolution: resolution})
	}
	return intents, reconcileErr
}

func authorizationSessionResolutionForObservation(observation contracts.AuthorizationObservation) contracts.AuthorizationSessionResolution {
	if observation.State == contracts.AuthorizationObservationConnected {
		return contracts.AuthorizationSessionResolutionProviderConnected
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(observation.FailureCode)), "superseded") {
		return contracts.AuthorizationSessionResolutionSuperseded
	}
	return contracts.AuthorizationSessionResolutionProviderFailed
}

func (application *service) DisconnectAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) (contracts.MutationResult, error) {
	if err := validateConnectorMutation(mutation); err != nil {
		return contracts.MutationResult{}, err
	}
	current, err := application.config.Repository.Connector(ctx, mutation.ConnectorKey)
	if err != nil {
		return contracts.MutationResult{}, err
	}
	remote := current.Release.Manifest.Implementation.RemoteStreamableHTTP != nil
	if remote && strings.TrimSpace(mutation.AccountID) == "" {
		return contracts.MutationResult{}, invalidRequest("accountId is required for remote connector authorization")
	}
	return application.acceptConnectorOperation(
		ctx,
		mutation,
		contracts.OperationKindDisconnectAuthorization,
		func(connector contracts.Connector) (contracts.Connector, error) {
			if remote {
				return connector, nil
			}
			if connector.Authorization.State == contracts.AuthorizationStateNotRequired {
				return contracts.Connector{}, invalidTransition(
					"authorization",
					string(connector.Authorization.State),
					string(contracts.AuthorizationStateDisconnected),
				)
			}
			return connector, nil
		},
	)
}
