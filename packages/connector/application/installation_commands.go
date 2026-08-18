package application

import (
	"context"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) Install(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) (contracts.MutationResult, error) {
	var target contracts.InstallationState
	result, err := application.acceptConnectorOperation(
		ctx,
		mutation,
		contracts.OperationKindInstall,
		func(connector contracts.Connector) (contracts.Connector, error) {
			if connector.Compatibility.State != contracts.CompatibilityStateSupported {
				return contracts.Connector{}, contracts.NewDomainError(
					contracts.ErrorCodeIncompatible,
					"connector is not compatible with this host",
					false,
					nil,
				)
			}
			if connector.Installation.State == contracts.InstallationStateInstalled {
				target = contracts.InstallationStateUpdating
			} else {
				target = contracts.InstallationStateInstalling
			}
			if !contracts.CanTransitionInstallation(connector.Installation.State, target) {
				return contracts.Connector{}, invalidTransition("installation", string(connector.Installation.State), string(target))
			}
			if installationRequiresPhysicalRepair(connector.Installation) {
				// Calibration deliberately retains the last committed release while
				// an installation is absent or invalid so a later observation can
				// restore it without reinstalling. Once the user explicitly repairs
				// the Connector, that evidence no longer describes a usable
				// installation and must not survive the transition to installing.
				connector.Installation = contracts.Installation{}
			}
			connector.Installation.State = target
			connector.Installation.FailureCode = ""
			return connector, nil
		},
	)
	return result, err
}

func installationRequiresPhysicalRepair(installation contracts.Installation) bool {
	if installation.State != contracts.InstallationStateFailed {
		return false
	}
	return installation.FailureCode == InstallationFailureCodePhysicallyAbsent ||
		installation.FailureCode == InstallationFailureCodePhysicallyInvalid
}

// Uninstall removes the Connector runtime and release from this device. It is
// deliberately independent from DisconnectAuthorization: account authorization
// remains server-owned and can be reused by another device or a later reinstall.
func (application *service) Uninstall(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) (contracts.MutationResult, error) {
	return application.acceptConnectorOperation(
		ctx,
		mutation,
		contracts.OperationKindUninstall,
		func(connector contracts.Connector) (contracts.Connector, error) {
			if connector.Installation.InstalledReleaseDigest == "" {
				return contracts.Connector{}, invalidTransition(
					"installation",
					string(connector.Installation.State),
					string(contracts.InstallationStateUninstalling),
				)
			}
			if !contracts.CanTransitionInstallation(connector.Installation.State, contracts.InstallationStateUninstalling) {
				return contracts.Connector{}, invalidTransition(
					"installation",
					string(connector.Installation.State),
					string(contracts.InstallationStateUninstalling),
				)
			}
			connector.Installation.State = contracts.InstallationStateUninstalling
			connector.Installation.FailureCode = ""
			return connector, nil
		},
	)
}
