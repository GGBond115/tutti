package application

import (
	"context"
	"errors"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"reflect"
	"sort"
	"strings"
	"time"
)

const authorizationSessionTTL = 10 * time.Minute

func (application *service) executeRefresh(ctx context.Context, operation contracts.Operation) error {
	if _, err := application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageRefreshing, nil); err != nil {
		return err
	}
	generation, err := application.config.Repository.BeginCatalogRefresh(ctx, application.config.Now().UTC())
	if err != nil {
		return err
	}
	failRefresh := func(refreshErr error) error {
		code := errorCodeOr(refreshErr, contracts.ErrorCodeUpstreamUnavailable)
		markContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if markErr := application.config.Repository.FailCatalogRefresh(
			markContext, generation, string(code), application.config.Now().UTC(),
		); markErr != nil {
			return errors.Join(refreshErr, markErr)
		}
		return refreshErr
	}
	catalog, err := application.config.CatalogSource.FetchSnapshot(ctx)
	if err != nil {
		return failRefresh(preserveCatalogSourceError("connector catalog refresh failed", err))
	}
	releases, err := validateCatalogSnapshot(catalog)
	if err != nil {
		return failRefresh(err)
	}
	var applied bool
	err = application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		applied, err = tx.ReplaceCatalogSnapshot(generation, catalog, application.config.Now().UTC())
		if err != nil || !applied {
			return err
		}
		storedOperation, err := tx.Operation(operation.OperationID)
		if err != nil {
			return err
		}
		existing, err := tx.Connectors()
		if err != nil {
			return err
		}
		byKey := make(map[string]contracts.Connector, len(existing))
		for _, connector := range existing {
			byKey[connector.Key] = connector
		}
		revision := tx.AdvanceRevision()
		accepted := make(map[string]bool, len(releases))
		for _, release := range releases {
			accepted[release.ConnectorKey] = true
			connector, ok := byKey[release.ConnectorKey]
			if !ok {
				connector = newCatalogConnector(release)
			}
			connector.Authorization = authorizationForManifest(connector.Authorization, release.Manifest.AuthorizationKind)
			connector.Release = release
			compatibility, err := application.compatibilityFor(release.Manifest)
			if err != nil {
				return err
			}
			connector.Compatibility = compatibility
			connector.Revision = revision
			if err := tx.SaveConnector(connector); err != nil {
				return err
			}
		}
		for _, connector := range existing {
			if accepted[connector.Key] {
				continue
			}
			if connector.Installation.State == contracts.InstallationStateNotInstalled {
				if err := tx.DeleteConnector(connector.Key); err != nil {
					return err
				}
				continue
			}
			connector.Compatibility = contracts.Compatibility{
				State:  contracts.CompatibilityStateUnsupportedVersion,
				Reason: "removed_from_catalog",
			}
			connector.Revision = revision
			if err := tx.SaveConnector(connector); err != nil {
				return err
			}
		}
		storedOperation.State = contracts.OperationStateCompleted
		storedOperation.Stage = contracts.OperationStageCompleted
		storedOperation.UpdatedAt = application.config.Now().UTC()
		if err := tx.SaveOperation(storedOperation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			OperationID: storedOperation.OperationID,
			Revision:    revision,
		})
	})
	if err != nil {
		return failRefresh(err)
	}
	if !applied {
		return application.completeSupersededCatalogRefresh(ctx, operation.OperationID)
	}
	return nil
}

func validateCatalogSnapshot(snapshot contracts.CatalogSnapshot) ([]contracts.Release, error) {
	if len(snapshot.Categories) == 0 {
		return nil, invalidManifest("connector catalog has no categories", nil)
	}
	categoryIDs := make(map[string]struct{}, len(snapshot.Categories))
	for _, category := range snapshot.Categories {
		if strings.TrimSpace(category.CategoryID) == "" ||
			(category.Kind != "category" && category.Kind != "featured") || category.ItemCount < 0 {
			return nil, invalidManifest("connector catalog contains an invalid category", nil)
		}
		if _, exists := categoryIDs[category.CategoryID]; exists {
			return nil, invalidManifest("connector catalog contains duplicate categories", nil)
		}
		categoryIDs[category.CategoryID] = struct{}{}
	}
	releasesByKey := make(map[string]contracts.Release, len(snapshot.Entries))
	placements := make(map[string]map[int]struct{})
	sectionConnectors := make(map[string]map[string]struct{})
	sectionCounts := make(map[string]int64, len(snapshot.Categories))
	for _, entry := range snapshot.Entries {
		if _, exists := categoryIDs[entry.SectionID]; !exists {
			return nil, invalidManifest("connector catalog placement section is unknown", nil)
		}
		if _, exists := categoryIDs[entry.CategoryID]; !exists {
			return nil, invalidManifest("connector catalog placement category is unknown", nil)
		}
		if entry.Order < 0 {
			return nil, invalidManifest("connector catalog placement order is invalid", nil)
		}
		if placements[entry.SectionID] == nil {
			placements[entry.SectionID] = make(map[int]struct{})
		}
		if _, exists := placements[entry.SectionID][entry.Order]; exists {
			return nil, invalidManifest("connector catalog contains duplicate placement order", nil)
		}
		placements[entry.SectionID][entry.Order] = struct{}{}
		if sectionConnectors[entry.SectionID] == nil {
			sectionConnectors[entry.SectionID] = make(map[string]struct{})
		}
		if _, exists := sectionConnectors[entry.SectionID][entry.Release.ConnectorKey]; exists {
			return nil, invalidManifest("connector catalog contains duplicate section placements", nil)
		}
		sectionConnectors[entry.SectionID][entry.Release.ConnectorKey] = struct{}{}
		sectionCounts[entry.SectionID]++
		if err := contracts.ValidateReleaseShape(entry.Release); err != nil {
			return nil, err
		}
		if entry.Release.Status != contracts.ReleaseStatusAvailable {
			return nil, invalidManifest("active catalog releases must have available status", nil)
		}
		if existing, exists := releasesByKey[entry.Release.ConnectorKey]; exists && !reflect.DeepEqual(existing, entry.Release) {
			return nil, invalidManifest("connector catalog contains conflicting releases", nil)
		}
		releasesByKey[entry.Release.ConnectorKey] = entry.Release
	}
	for _, category := range snapshot.Categories {
		if sectionCounts[category.CategoryID] != category.ItemCount {
			return nil, invalidManifest("connector catalog category count does not match its placements", nil)
		}
	}
	keys := make([]string, 0, len(releasesByKey))
	for key := range releasesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	releases := make([]contracts.Release, 0, len(keys))
	for _, key := range keys {
		releases = append(releases, releasesByKey[key])
	}
	return releases, nil
}

func (application *service) completeSupersededCatalogRefresh(ctx context.Context, operationID string) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted {
			return nil
		}
		operation.State = contracts.OperationStateCompleted
		operation.Stage = contracts.OperationStageCompleted
		operation.UpdatedAt = application.config.Now().UTC()
		return tx.SaveOperation(operation)
	})
}

func (application *service) executeInstall(ctx context.Context, operation contracts.Operation) error {
	release, err := frozenRelease(operation)
	if err != nil {
		return err
	}
	if err := application.config.ImplementationRegistry.Validate(release.Manifest); err != nil {
		return err
	}
	operation, err = application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageInstalling, nil)
	if err != nil {
		return err
	}
	installed, installErr := application.config.ReleaseInstallations.InstallRelease(ctx, contracts.InstallReleaseRequest{
		OperationID: operation.OperationID,
		Scope:       operation.Scope,
		Generation:  operation.HostGeneration,
		Release:     release,
	})
	if installErr != nil {
		var domainError *contracts.DomainError
		if errors.As(installErr, &domainError) {
			return installErr
		}
		return contracts.NewDomainError(contracts.ErrorCodeInstallFailed, "connector release installation failed", true, installErr)
	}
	if err := validateReleaseInstallationReceipt(operation, release, installed); err != nil {
		return err
	}
	operation, err = application.updateOperationStage(
		ctx,
		operation.OperationID,
		contracts.OperationStageInstalled,
		func(current *contracts.Operation) { current.Execution.ReleaseInstallation = &installed },
	)
	if err != nil {
		return err
	}
	connector, err := application.config.Repository.Connector(ctx, operation.ConnectorKey)
	if err != nil {
		return err
	}
	connector.Release = release
	binding, err := application.resolveRuntimeBinding(ctx, operation, connector, release, contracts.RuntimeBindingPurposePlan)
	if err != nil {
		return err
	}
	defer clear(binding.CredentialBrokerGrant)
	if len(binding.CredentialBrokerGrant) != 0 {
		return invalidOperationReceipt("runtime planning returned a credential grant")
	}
	// The prepared receipt above is durable before this idempotent physical
	// commit. A crash after the commit leaves a running operation with enough
	// evidence for the continuous recovery scanner to replay this exact target.
	if err := application.config.ReleaseInstallations.CommitReleaseInstallation(ctx, contracts.CommitReleaseInstallationRequest{
		OperationID: operation.OperationID, Scope: operation.Scope, Generation: operation.HostGeneration,
		Release: release, Receipt: installed,
	}); err != nil {
		return contracts.NewDomainError(contracts.ErrorCodeInstallFailed, "connector release installation commit failed", true, err)
	}
	if err := application.prepareInstallRuntimeDesired(ctx, operation.OperationID, release, binding); err != nil {
		return err
	}
	if err := application.awaitRuntimeDesired(ctx, operation.Scope, operation.ConnectorKey); err != nil {
		return err
	}
	return application.finalizeInstallAfterRuntime(ctx, operation.OperationID)
}

func (application *service) installedReleaseEvidence(ctx context.Context, connector contracts.Connector) (contracts.Release, error) {
	release, err := application.config.Repository.InstalledRelease(ctx, connector.Key, connector.Installation.InstalledReleaseDigest)
	if err == nil && release.ReleaseDigest == connector.Installation.InstalledReleaseDigest {
		return release, nil
	}
	if connector.Release.ReleaseDigest == connector.Installation.InstalledReleaseDigest {
		return connector.Release, nil
	}
	return contracts.Release{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "installed connector release evidence is unavailable", false, err)
}

func (application *service) executeUninstall(ctx context.Context, operation contracts.Operation) error {
	if operation.Target == nil || strings.TrimSpace(operation.Target.ReleaseDigest) == "" {
		return invalidOperationReceipt("uninstall operation target is missing")
	}
	operation, err := application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageDeactivating, nil)
	if err != nil {
		return err
	}
	connector, err := application.config.Repository.Connector(ctx, operation.ConnectorKey)
	if err != nil {
		return err
	}
	release, err := application.installedReleaseEvidence(ctx, connector)
	if err != nil {
		return err
	}
	binding, err := application.resolveRuntimeBinding(ctx, operation, connector, release, contracts.RuntimeBindingPurposeDeactivate)
	if err != nil {
		return err
	}
	clear(binding.CredentialBrokerGrant)
	binding.Enabled = false
	if err := application.prepareUninstallRuntimeDisabled(ctx, operation.OperationID, release, binding); err != nil {
		return err
	}
	if err := application.awaitRuntimeDesired(ctx, operation.Scope, operation.ConnectorKey); err != nil {
		return err
	}
	operation, err = application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageRemoving, nil)
	if err != nil {
		return err
	}
	if err := application.config.ReleaseInstallations.UninstallRelease(ctx, contracts.UninstallReleaseRequest{
		OperationID: operation.OperationID,
		Scope:       operation.Scope,
		Generation:  operation.HostGeneration,
		Release:     release,
	}); err != nil {
		return contracts.NewDomainError(contracts.ErrorCodeInstallFailed, "connector release cleanup failed", true, err)
	}
	return application.completeUninstall(ctx, operation.OperationID)
}

func (application *service) completeUninstall(ctx context.Context, operationID string) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted {
			return nil
		}
		connector, err := tx.Connector(operation.ConnectorKey)
		if err != nil {
			return err
		}
		revision := tx.AdvanceRevision()
		connector.Installation = contracts.Installation{State: contracts.InstallationStateNotInstalled}
		// Local uninstall changes only device installation truth. Authorization is
		// a separate lifecycle: remote authorization is projected from the account
		// snapshot, while local providers are disconnected only through the explicit
		// DisconnectAuthorization operation.
		connector.Revision = revision
		operation.State, operation.Stage, operation.FailureCode = contracts.OperationStateCompleted, contracts.OperationStageCompleted, ""
		operation.UpdatedAt = application.config.Now().UTC()
		if err := tx.DeleteRuntimeConvergence(operation.Scope, connector.Key); err != nil {
			return err
		}
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: connector.Key, OperationID: operation.OperationID, Revision: revision})
	})
}

const defaultConnectorConnectionID = "default"

func validateRuntimeReceipt(receipt contracts.RuntimeReceipt, operationID, connectionID, connectorKey,
	releaseDigest string, generation contracts.HostGeneration, expectedEnabled bool) error {
	if receipt.OperationID != operationID || receipt.ConnectionID != connectionID ||
		receipt.ConnectorKey != connectorKey || receipt.ReleaseDigest != releaseDigest || receipt.Generation != generation {
		return invalidOperationReceipt("implementation host returned a mismatched runtime receipt")
	}
	if !expectedEnabled {
		if receipt.Readiness.State != contracts.RuntimeReadinessBlocked ||
			receipt.Readiness.ReasonCode != contracts.RuntimeReadinessReasonRuntimeDisabled ||
			len(receipt.Readiness.Interfaces) != 0 {
			return invalidOperationReceipt("implementation host returned invalid disabled runtime readiness")
		}
		return nil
	}
	if receipt.Readiness.State != contracts.RuntimeReadinessReady {
		return invalidOperationReceipt("implementation host did not return a ready runtime receipt")
	}
	if receipt.Summary == nil {
		return invalidOperationReceipt("implementation host returned no matching connector summary")
	}
	if err := contracts.ValidateConnectorSummary(*receipt.Summary, connectorKey); err != nil {
		return invalidOperationReceipt("implementation host returned an invalid connector summary")
	}
	if len(receipt.Readiness.Interfaces) == 0 {
		return invalidOperationReceipt("implementation host returned no ready interfaces")
	}
	readyInterfaces := make(map[string]struct{}, len(receipt.Readiness.Interfaces))
	for _, readiness := range receipt.Readiness.Interfaces {
		if (readiness.Kind != "mcp" && readiness.Kind != "cli") || readiness.State != contracts.RuntimeReadinessReady {
			return invalidOperationReceipt("implementation host returned invalid interface readiness")
		}
		readyInterfaces[readiness.Kind] = struct{}{}
	}
	if len(readyInterfaces) != len(receipt.Summary.Interfaces) {
		return invalidOperationReceipt("implementation host returned inconsistent interface summary")
	}
	for _, summary := range receipt.Summary.Interfaces {
		if _, ok := readyInterfaces[summary.Kind]; !ok {
			return invalidOperationReceipt("implementation host returned inconsistent interface summary")
		}
	}
	return nil
}

func (application *service) beginAuthorizationSession(
	ctx context.Context,
	operation contracts.Operation,
	secret []byte,
	replacementPolicy contracts.AuthorizationReplacementPolicy,
) (contracts.AuthorizationSession, error) {
	release, err := frozenRelease(operation)
	if err != nil {
		return contracts.AuthorizationSession{}, err
	}
	if operation.State == contracts.OperationStateCompleted && operation.Execution.AuthorizationSession != nil &&
		operation.Execution.AuthorizationSession.IsResolved() {
		session := *operation.Execution.AuthorizationSession
		session.AuthorizationURL = ""
		switch session.Resolution {
		case contracts.AuthorizationSessionResolutionProviderConnected, contracts.AuthorizationSessionResolutionAccountStateConverged:
			session.State = contracts.AuthorizationStateConnected
		default:
			session.State = contracts.AuthorizationStateFailed
		}
		return session, nil
	}
	if operation.State == contracts.OperationStateAccepted {
		operation, err = application.markOperationRunning(ctx, operation.OperationID)
		if err != nil {
			return contracts.AuthorizationSession{}, err
		}
	}
	connector, err := application.config.Repository.Connector(ctx, operation.ConnectorKey)
	if err != nil {
		return contracts.AuthorizationSession{}, err
	}
	if operation.State != contracts.OperationStateCompleted {
		operation, err = application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageAuthorizing, nil)
		if err != nil {
			return contracts.AuthorizationSession{}, err
		}
	}
	session, err := application.config.Authorization.Begin(ctx, contracts.AuthorizationStartRequest{
		OperationID:       operation.OperationID,
		ClientRequestID:   operation.ClientRequestID,
		ReplacementPolicy: replacementPolicy,
		Scope:             operation.Scope,
		Connector:         connector,
		Release:           release,
		Secret:            secret,
	})
	if err != nil {
		return contracts.AuthorizationSession{}, contracts.NewDomainError(
			contracts.ErrorCodeAuthorizationFailed,
			"connector authorization could not be started",
			true,
			err,
		)
	}
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = application.config.Now().UTC().Add(authorizationSessionTTL)
	}
	if session.OperationID != operation.OperationID || session.ConnectorKey != operation.ConnectorKey ||
		strings.TrimSpace(session.SessionID) == "" || !validAuthorizationSessionAction(session) {
		return contracts.AuthorizationSession{}, invalidOperationReceipt("authorization provider returned an invalid session")
	}
	remote := release.Manifest.Implementation.RemoteStreamableHTTP != nil
	accountScoped := strings.TrimSpace(operation.Scope.AccountID) != ""
	if session.State == contracts.AuthorizationStateConnected && !remote {
		session.Resolution = contracts.AuthorizationSessionResolutionProviderConnected
	} else {
		session.Resolution = contracts.AuthorizationSessionResolutionUnresolved
	}
	projectDeviceState := !remote && (!accountScoped || connector.Authorization.State != contracts.AuthorizationStateConnected)
	if err := application.completeAuthorizationStart(ctx, operation.OperationID, session, projectDeviceState); err != nil {
		return contracts.AuthorizationSession{}, err
	}
	if session.State == contracts.AuthorizationStateConnected || (!remote && accountScoped) {
		if err := application.projectAuthorizationAndScheduleRuntime(ctx, operation.Scope, operation.ConnectorKey, session.ConnectionID, session.State, ""); err != nil {
			return contracts.AuthorizationSession{}, err
		}
	}
	return session, nil
}

func validAuthorizationSessionAction(session contracts.AuthorizationSession) bool {
	switch strings.TrimSpace(session.ActionType) {
	case "":
		return (session.State == contracts.AuthorizationStatePending && strings.TrimSpace(session.AuthorizationURL) != "") ||
			(session.State == contracts.AuthorizationStateConnected && strings.TrimSpace(session.AuthorizationURL) == "" && strings.TrimSpace(session.ConnectionID) != "")
	case "redirect":
		return session.State == contracts.AuthorizationStatePending && strings.TrimSpace(session.AuthorizationURL) != ""
	case "submit_secret":
		return session.State == contracts.AuthorizationStateConnected && strings.TrimSpace(session.AuthorizationURL) == "" && strings.TrimSpace(session.ConnectionID) != ""
	default:
		return false
	}
}

func (application *service) executeDisconnectAuthorization(ctx context.Context, operation contracts.Operation) error {
	operation, err := application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageDisconnecting, nil)
	if err != nil {
		return err
	}
	connector, err := application.config.Repository.Connector(ctx, operation.ConnectorKey)
	if err != nil {
		return err
	}
	release, err := frozenRelease(operation)
	if err != nil {
		return err
	}
	if err := application.config.Authorization.Disconnect(ctx, contracts.AuthorizationDisconnectRequest{
		OperationID: operation.OperationID,
		Scope:       operation.Scope,
		Connector:   connector,
		Release:     release,
	}); err != nil {
		return contracts.NewDomainError(contracts.ErrorCodeAuthorizationFailed, "connector authorization disconnect failed", true, err)
	}
	remote := release.Manifest.Implementation.RemoteStreamableHTTP != nil
	if err := application.projectAuthorizationAndScheduleRuntime(ctx, operation.Scope, operation.ConnectorKey, "", contracts.AuthorizationStateDisconnected, ""); err != nil {
		return err
	}
	if remote {
		receipts, err := application.config.Repository.UnresolvedAuthorizationSessionOperations(ctx, operation.Scope)
		if err != nil {
			return err
		}
		for _, receipt := range receipts {
			if receipt.ConnectorKey != operation.ConnectorKey {
				continue
			}
			if err := application.config.Repository.ResolveAuthorizationSession(ctx, receipt.OperationID, contracts.AuthorizationSessionResolutionSuperseded); err != nil {
				return err
			}
		}
	}
	return application.completeConnectorOperation(ctx, operation.OperationID, func(connector contracts.Connector) contracts.Connector {
		if !remote {
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
		}
		return connector
	})
}

func (application *service) markOperationRunning(ctx context.Context, operationID string) (contracts.Operation, error) {
	var result contracts.Operation
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted || operation.State == contracts.OperationStateFailed {
			result = operation
			return nil
		}
		revision := tx.AdvanceRevision()
		operation.State = contracts.OperationStateRunning
		operation.Attempt++
		operation.UpdatedAt = application.config.Now().UTC()
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: operation.ConnectorKey,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = operation
		return nil
	})
	return result, err
}

func (application *service) updateOperationStage(
	ctx context.Context,
	operationID string,
	stage contracts.OperationStage,
	mutate func(*contracts.Operation),
) (contracts.Operation, error) {
	var result contracts.Operation
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted || operation.State == contracts.OperationStateFailed {
			result = operation
			return nil
		}
		revision := tx.AdvanceRevision()
		operation.State = contracts.OperationStateRunning
		operation.Stage = stage
		operation.UpdatedAt = application.config.Now().UTC()
		if mutate != nil {
			mutate(&operation)
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: operation.ConnectorKey,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = operation
		return nil
	})
	return result, err
}

func (application *service) completeConnectorOperation(
	ctx context.Context,
	operationID string,
	update func(contracts.Connector) contracts.Connector,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted {
			return nil
		}
		connector, err := tx.Connector(operation.ConnectorKey)
		if err != nil {
			return err
		}
		revision := tx.AdvanceRevision()
		connector = update(connector)
		connector.Revision = revision
		operation.State = contracts.OperationStateCompleted
		operation.Stage = contracts.OperationStageCompleted
		operation.FailureCode = ""
		operation.UpdatedAt = application.config.Now().UTC()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connector.Key,
			OperationID:  operation.OperationID,
			Revision:     revision,
		})
	})
}

func (application *service) completeAuthorizationStart(
	ctx context.Context,
	operationID string,
	session contracts.AuthorizationSession,
	projectDeviceState bool,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		connector, err := tx.Connector(operation.ConnectorKey)
		if err != nil {
			return err
		}
		stateChanged := projectDeviceState && connector.Authorization.State != session.State
		if operation.State == contracts.OperationStateCompleted && !stateChanged {
			return nil
		}
		if stateChanged && !contracts.CanTransitionAuthorization(connector.Authorization.State, session.State) {
			return invalidTransition("authorization", string(connector.Authorization.State), string(session.State))
		}
		revision := tx.AdvanceRevision()
		if projectDeviceState {
			connector.Authorization = contracts.Authorization{State: session.State}
		}
		connector.Revision = revision
		if operation.State != contracts.OperationStateCompleted {
			operation.State = contracts.OperationStateCompleted
			operation.Stage = contracts.OperationStageCompleted
		}
		operation.Execution.AuthorizationSession = &session
		operation.UpdatedAt = application.config.Now().UTC()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connector.Key,
			OperationID:  operation.OperationID,
			Revision:     revision,
		})
	})
}

func (application *service) completeAuthorizationObservation(
	ctx context.Context,
	connectorKey string,
	observation contracts.AuthorizationObservation,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		connector, err := tx.Connector(connectorKey)
		if err != nil {
			return err
		}
		if connector.Authorization.State != contracts.AuthorizationStatePending {
			return nil
		}
		target := contracts.AuthorizationStateConnected
		failureCode := ""
		if observation.State == contracts.AuthorizationObservationFailed {
			target = contracts.AuthorizationStateFailed
			failureCode = strings.TrimSpace(observation.FailureCode)
			if failureCode == "" {
				failureCode = string(contracts.ErrorCodeAuthorizationFailed)
			}
		}
		if !contracts.CanTransitionAuthorization(connector.Authorization.State, target) {
			return invalidTransition("authorization", string(connector.Authorization.State), string(target))
		}
		revision := tx.AdvanceRevision()
		connector.Authorization = contracts.Authorization{State: target, FailureCode: failureCode}
		connector.Revision = revision
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: connector.Key, Revision: revision})
	})
}

func (application *service) failOperation(ctx context.Context, operationID string, code contracts.ErrorCode) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted || operation.State == contracts.OperationStateFailed {
			return nil
		}
		revision := tx.AdvanceRevision()
		operation.State = contracts.OperationStateFailed
		operation.Stage = contracts.OperationStageFailed
		operation.FailureCode = string(code)
		operation.UpdatedAt = application.config.Now().UTC()
		if operation.Kind != contracts.OperationKindRefreshCatalog && operation.ConnectorKey != "" {
			connector, err := tx.Connector(operation.ConnectorKey)
			if err != nil && !errors.Is(err, contracts.ErrNotFound) {
				return err
			}
			if err == nil {
				switch operation.Kind {
				case contracts.OperationKindInstall:
					if connector.Installation.CandidateReleaseDigest != "" {
						if err := tx.DeleteRuntimeConvergence(operation.Scope, connector.Key); err != nil {
							return err
						}
					}
					connector.Installation.CandidateVersion = ""
					connector.Installation.CandidateReleaseID = ""
					connector.Installation.CandidateReleaseDigest = ""
					if connector.Installation.InstalledReleaseDigest != "" {
						connector.Installation.State = contracts.InstallationStateInstalled
						connector.Installation.FailureCode = string(code)
						break
					}
					// A failed first installation has no usable artifact to configure or
					// repair. Preserve the failure on the terminal operation and restore
					// the Connector to its pre-install state so the user can try again.
					connector.Installation.State = contracts.InstallationStateNotInstalled
					connector.Installation.FailureCode = ""
				case contracts.OperationKindUninstall:
					connector.Installation.State = contracts.InstallationStateFailed
					connector.Installation.FailureCode = string(code)
				case contracts.OperationKindStartAuthorization, contracts.OperationKindDisconnectAuthorization:
					connector.Authorization.State = contracts.AuthorizationStateFailed
					connector.Authorization.FailureCode = string(code)
				}
				connector.Revision = revision
				if err := tx.SaveConnector(connector); err != nil {
					return err
				}
			}
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: operation.ConnectorKey,
			OperationID:  operation.OperationID,
			Revision:     revision,
		})
	})
}
