package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (host *Host) runAuthorizationReconcileWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileContext, cancel := context.WithTimeout(ctx, 15*time.Second)
			host.bootstrapMu.Lock()
			bootstrapped, scope := host.bootstrapped, host.bootstrapScope
			if !bootstrapped || strings.TrimSpace(scope.AccountID) == "" {
				host.bootstrapMu.Unlock()
				cancel()
				continue
			}
			err := host.reconcileAuthorizationsLocked(reconcileContext, scope)
			host.bootstrapMu.Unlock()
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("connector authorization reconciliation failed", "error", err)
			}
		}
	}
}

func (host *Host) reconcileAuthorizationsLocked(ctx context.Context, scope contracts.OperationScope) error {
	intents, err := host.authorizationMaintenance.ReconcileAuthorizations(ctx, scope)
	if err != nil || len(intents) == 0 {
		return err
	}
	// ReconcileAuthorizations persists projection intent. Create or join one
	// scoped runtime operation for every affected Connector before releasing the
	// lifecycle fence, so logout or account switching cannot be followed by a
	// late old-account route publication.
	connectorKeys := make(map[string]struct{}, len(intents))
	for _, intent := range intents {
		connectorKeys[intent.ConnectorKey] = struct{}{}
	}
	for connectorKey := range connectorKeys {
		if reconcileErr := host.reconcileRuntimeForScopeLocked(ctx, scope, connectorKey); reconcileErr != nil {
			err = errors.Join(err, reconcileErr)
		}
	}
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if resolveErr := host.authorizationMaintenance.ResolveAuthorizationSession(ctx, intent.OperationID, intent.Resolution); resolveErr != nil {
			err = errors.Join(err, resolveErr)
		}
	}
	return err
}

// Bootstrap restores durable local runtime intent without depending on the
// remote catalog. Account-authorized remote routes additionally require a
// fresh server snapshot before the lifecycle gate opens.
// ActivateScope fences the previous authority and restores durable runtime
// intent for the explicitly active account.
func (host *Host) ActivateScope(ctx context.Context, scope contracts.OperationScope) error {
	if host == nil || host.recoveryControl == nil {
		return errors.New("connector market host is unavailable")
	}
	if err := host.requireRunning(); err != nil {
		return err
	}
	if err := host.acquireScopeTransition(ctx); err != nil {
		return err
	}
	defer host.releaseScopeTransition()
	drained := host.commandAdmission.close()
	select {
	case <-drained:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := host.requireRunning(); err != nil {
		return err
	}
	host.bootstrapMu.Lock()
	previousScope := host.bootstrapScope
	sameStableScope := host.bootstrapped && previousScope == scope && !host.activationGate.requiresRecovery()
	host.bootstrapMu.Unlock()
	if !sameStableScope {
		if err := errors.Join(
			host.applyCapabilityPublication(ctx, previousScope, false),
			host.activationGate.FailClosed(ctx, time.Now().Add(10*time.Second)),
		); err != nil {
			return err
		}
	}
	if err := host.bootstrapForScope(ctx, scope); err != nil {
		return err
	}
	host.lifecycleMu.Lock()
	if host.lifecycleState != LifecycleStateRunning {
		host.lifecycleMu.Unlock()
		return errHostNotRunning
	}
	host.commandAdmission.open()
	host.lifecycleMu.Unlock()
	return nil
}

func (host *Host) bootstrapForScope(ctx context.Context, scope contracts.OperationScope) error {
	host.bootstrapMu.Lock()
	defer host.bootstrapMu.Unlock()
	sameScope := host.bootstrapScope == scope
	if host.bootstrapped && sameScope && !host.activationGate.requiresRecovery() {
		return host.reconcilePendingRuntimesLocked(ctx, scope)
	}
	host.bootstrapScope = scope
	host.runtimeRecoveryPending = make(map[string]struct{})
	if host.authorizationReadiness != nil && strings.TrimSpace(scope.AccountID) != "" {
		host.authorizationReadiness.SetReady(scope.AccountID, false)
	}
	host.notifyAuthorizationScopeChanged()
	host.bootstrapped = false
	host.activationGate.setOpen(scope, false)
	if err := host.applyCapabilityPublication(ctx, scope, false); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		host.activationGate.setOpen(scope, false)
		_ = host.applyCapabilityPublication(context.Background(), scope, false)
		fenceContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.implementationCommands.FailClosed(fenceContext, time.Now().Add(10*time.Second)); err != nil {
			slog.Error("connector market bootstrap rollback runtime fence failed", "error", err)
		}
		if err := host.runtimeMaintenance.FenceInstalledRuntimesForScope(fenceContext, scope); err != nil {
			slog.Error("connector market bootstrap rollback fence failed", "error", err)
		}
	}()
	// Fence any route left by an interrupted previous bootstrap before recovery
	// can replay host-touching operations. Reconcile calls remain staged behind
	// activationGate until durable local recovery has completed.
	if err := host.implementationCommands.FailClosed(ctx, time.Now().Add(10*time.Second)); err != nil {
		return err
	}
	if err := host.runtimeMaintenance.FenceInstalledRuntimesForScope(ctx, scope); err != nil {
		return err
	}
	if err := host.recoverAndWait(ctx); err != nil {
		return err
	}
	if err := host.installationMaintenance.CalibrateInstalledConnectorsForScope(ctx, scope); err != nil {
		// A timeout or other indeterminate probe must preserve durable truth. The
		// following runtime reconcile remains authoritative and may still recover.
		slog.Warn("connector installation calibration was indeterminate", "error", err)
	}
	if strings.TrimSpace(scope.AccountID) != "" {
		if _, err := host.syncAuthorizationSnapshot(ctx, scope); err != nil {
			// Account authorization is fail-closed independently. Device-local
			// authorization-free connectors can still recover while a later WS or
			// poll retries the authoritative snapshot.
			slog.Warn("connector authorization snapshot unavailable during bootstrap", "error", err)
		} else if host.authorizationReadiness != nil {
			host.authorizationReadiness.SetReady(scope.AccountID, true)
		}
	}
	host.activationGate.setOpen(scope, true)
	reconcileErr := host.runtimeMaintenance.ReconcileInstalledRuntimesForScope(ctx, scope)
	var reconcileFailures *application.RuntimeReconcileFailures
	if reconcileErr != nil && !errors.As(reconcileErr, &reconcileFailures) {
		return reconcileErr
	}
	if reconcileFailures != nil {
		for _, connectorKey := range reconcileFailures.ConnectorKeys() {
			host.runtimeRecoveryPending[connectorKey] = struct{}{}
		}
		host.notifyRuntimeRecovery()
	}
	if err := host.requireBootstrapLifecycle(); err != nil {
		return err
	}
	if err := host.applyCapabilityPublication(ctx, scope, true); err != nil {
		return err
	}
	if err := host.requireBootstrapLifecycle(); err != nil {
		_ = host.applyCapabilityPublication(context.Background(), scope, false)
		return err
	}
	host.activationGate.markRecovered()
	host.bootstrapped = true
	committed = true
	if reconcileFailures != nil {
		slog.Warn("connector market bootstrap completed with unavailable connectors", "error", reconcileFailures)
	}
	return nil
}

func (host *Host) requireBootstrapLifecycle() error {
	host.lifecycleMu.Lock()
	defer host.lifecycleMu.Unlock()
	if host.lifecycleState != LifecycleStateStarting && host.lifecycleState != LifecycleStateRunning {
		return errHostNotRunning
	}
	return nil
}

// NotifyAuthorizationChanged treats realtime events and MCP authorization
// errors as convergence hints. The account snapshot remains the only truth.
func (host *Host) NotifyAuthorizationChanged() {
	if host == nil {
		return
	}
	select {
	case host.authorizationSyncWake <- struct{}{}:
	default:
	}
}

func (host *Host) notifyAuthorizationScopeChanged() {
	select {
	case host.authorizationScopeWake <- struct{}{}:
	default:
	}
}

func (host *Host) runAuthorizationEventWorker(ctx context.Context) {
	retry := time.Second
	for {
		host.bootstrapMu.Lock()
		scope := host.bootstrapScope
		host.bootstrapMu.Unlock()
		if strings.TrimSpace(scope.AccountID) == "" {
			select {
			case <-ctx.Done():
				return
			case <-host.authorizationScopeWake:
				continue
			}
		}
		attemptCtx, cancel := context.WithCancel(ctx)
		result := make(chan error, 1)
		go func(accountID string) {
			result <- host.authorizationEvents.RunAuthorizationEvents(attemptCtx, accountID, host.NotifyAuthorizationChanged)
		}(scope.AccountID)
		select {
		case <-ctx.Done():
			cancel()
			<-result
			return
		case <-host.authorizationScopeWake:
			cancel()
			<-result
			retry = time.Second
			continue
		case err := <-result:
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("connector authorization realtime listener failed", "error", err)
			}
		}
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-host.authorizationScopeWake:
			timer.Stop()
			retry = time.Second
		case <-timer.C:
			if retry < time.Minute {
				retry *= 2
			}
		}
	}
}

type authorizationSyncResult struct {
	changed         []string
	receipts        []string
	becameReady     bool
	calibrate       bool
	snapshotApplied bool
}

func (host *Host) syncAuthorizationSnapshot(ctx context.Context, scope contracts.OperationScope) (authorizationSyncResult, error) {
	if host.authorizationSnapshots == nil || host.authorizationSnapshotStore == nil || strings.TrimSpace(scope.AccountID) == "" {
		return authorizationSyncResult{}, nil
	}
	snapshot, err := host.authorizationSnapshots.AuthorizationSnapshot(ctx, scope.AccountID)
	if err != nil {
		return authorizationSyncResult{}, err
	}
	applied, err := host.authorizationSnapshotStore.ApplyAuthorizationSnapshot(ctx, scope.AccountID, snapshot)
	result := authorizationSyncResult{
		changed:         applied.ChangedConnectorKeys,
		receipts:        applied.PendingReceiptConnectorKeys,
		snapshotApplied: err == nil,
	}
	return result, err
}

func (host *Host) runAuthorizationSnapshotWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		calibrate := false
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			calibrate = true
		case <-host.authorizationSyncWake:
		}
		host.bootstrapMu.Lock()
		bootstrapped, scope := host.bootstrapped, host.bootstrapScope
		host.bootstrapMu.Unlock()
		if !bootstrapped || strings.TrimSpace(scope.AccountID) == "" {
			continue
		}
		syncContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := host.syncAuthorizationSnapshot(syncContext, scope)
		result.calibrate = calibrate
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("connector authorization snapshot sync failed", "error", err)
			}
			continue
		}
		reconcileContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err = host.reconcileAuthorizationChanges(reconcileContext, scope, result)
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("connector authorization runtime reconcile failed", "error", err)
		}
	}
}

func (host *Host) reconcileAuthorizationChanges(ctx context.Context, scope contracts.OperationScope, result authorizationSyncResult) error {
	host.bootstrapMu.Lock()
	defer host.bootstrapMu.Unlock()
	if !host.bootstrapped || host.bootstrapScope != scope || host.activationGate.requiresRecovery() {
		return nil
	}
	if result.snapshotApplied && host.authorizationReadiness != nil {
		result.becameReady = host.authorizationReadiness.SetReady(scope.AccountID, true)
	}
	dirty := host.authorizationDirty[scope.AccountID]
	if dirty == nil {
		dirty = make(map[string]struct{})
		host.authorizationDirty[scope.AccountID] = dirty
	}
	connectorKeys, err := host.authorizationMaintenance.InstalledRemoteAuthorizedConnectorKeys(ctx)
	if err != nil {
		return err
	}
	eligible := make(map[string]struct{}, len(connectorKeys))
	for _, connectorKey := range connectorKeys {
		eligible[connectorKey] = struct{}{}
	}
	for connectorKey := range dirty {
		if _, ok := eligible[connectorKey]; !ok {
			delete(dirty, connectorKey)
		}
	}
	for _, connectorKey := range result.changed {
		if _, ok := eligible[connectorKey]; ok {
			dirty[connectorKey] = struct{}{}
		}
	}
	for _, connectorKey := range result.receipts {
		if _, ok := eligible[connectorKey]; ok {
			dirty[connectorKey] = struct{}{}
		}
	}
	if result.becameReady || result.calibrate {
		for _, connectorKey := range connectorKeys {
			dirty[connectorKey] = struct{}{}
		}
	}
	var reconcileErr error
	for connectorKey := range dirty {
		if err := host.reconcileRuntimeForScopeLocked(ctx, scope, connectorKey); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("%s: %w", connectorKey, err))
			continue
		}
		delete(dirty, connectorKey)
	}
	return reconcileErr
}

// FenceForScope closes publication and runtime authority for an account
// boundary without deleting device installation truth. A later bootstrap,
// including one for the same account, must perform full recovery before routes
// can be published again.
