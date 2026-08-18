package application

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"strings"
)

const (
	InstallationFailureCodePhysicallyAbsent  = "connector_installation_absent"
	InstallationFailureCodePhysicallyInvalid = "connector_installation_invalid"
)

// CalibrateInstalledConnectorsForScope compares the repository projection
// with the physical installation manager. Inspection is release- and
// receipt-based: Connector-owned commands are never executed. Indeterminate
// observations preserve the current projection.
func (application *service) CalibrateInstalledConnectorsForScope(
	ctx context.Context,
	scope contracts.OperationScope,
) error {
	if application == nil || application.config.ReleaseInstallations == nil {
		return nil
	}
	snapshot, err := application.config.Repository.Snapshot(ctx)
	if err != nil {
		return err
	}
	var calibrationErrors []error
	for _, connector := range snapshot.Connectors {
		if !installationCalibrationCandidate(connector) {
			continue
		}
		release, evidenceErr := application.installedReleaseEvidence(ctx, connector)
		if evidenceErr != nil {
			calibrationErrors = append(calibrationErrors, fmt.Errorf("calibrate connector %s: %w", connector.Key, evidenceErr))
			continue
		}
		operationID := "inspect/" + application.config.BootEpoch + "/" + connector.Key
		observation, inspectErr := application.config.ReleaseInstallations.InspectReleaseInstallation(ctx, contracts.InspectReleaseInstallationRequest{
			OperationID: operationID,
			Scope:       scope,
			Generation:  contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: nextGeneration(connector.Revision)},
			Release:     release,
		})
		if inspectErr != nil {
			calibrationErrors = append(calibrationErrors, fmt.Errorf("calibrate connector %s installation: %w", connector.Key, inspectErr))
			continue
		}
		if observation.ConnectorKey != connector.Key || observation.ReleaseDigest != release.ReleaseDigest ||
			!validReleaseInstallationObservation(observation.State) {
			calibrationErrors = append(calibrationErrors, fmt.Errorf("calibrate connector %s: installation manager returned a mismatched observation", connector.Key))
			continue
		}
		if observation.State == contracts.ReleaseInstallationIndeterminate {
			continue
		}
		if err := application.applyInstallationObservation(ctx, connector.Key, operationID, release.ReleaseDigest, observation.State); err != nil {
			calibrationErrors = append(calibrationErrors, fmt.Errorf("calibrate connector %s projection: %w", connector.Key, err))
		}
	}
	return errors.Join(calibrationErrors...)
}

func installationCalibrationCandidate(connector contracts.Connector) bool {
	if strings.TrimSpace(connector.Installation.InstalledReleaseDigest) == "" {
		return false
	}
	return connector.Installation.State == contracts.InstallationStateInstalled ||
		(connector.Installation.State == contracts.InstallationStateFailed &&
			(connector.Installation.FailureCode == InstallationFailureCodePhysicallyAbsent ||
				connector.Installation.FailureCode == InstallationFailureCodePhysicallyInvalid))
}

func validReleaseInstallationObservation(state contracts.ReleaseInstallationObservationState) bool {
	switch state {
	case contracts.ReleaseInstallationPresent, contracts.ReleaseInstallationAbsent, contracts.ReleaseInstallationInvalid, contracts.ReleaseInstallationIndeterminate:
		return true
	default:
		return false
	}
}

func (application *service) applyInstallationObservation(
	ctx context.Context,
	connectorKey, operationID, releaseDigest string,
	state contracts.ReleaseInstallationObservationState,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		connector, err := tx.Connector(connectorKey)
		if err != nil {
			return err
		}
		if connector.Installation.InstalledReleaseDigest != releaseDigest || !installationCalibrationCandidate(connector) {
			return nil
		}
		next := connector.Installation
		switch state {
		case contracts.ReleaseInstallationPresent:
			if next.State == contracts.InstallationStateInstalled {
				return nil
			}
			next.State, next.FailureCode = contracts.InstallationStateInstalled, ""
		case contracts.ReleaseInstallationAbsent:
			if next.State == contracts.InstallationStateFailed && next.FailureCode == InstallationFailureCodePhysicallyAbsent {
				return nil
			}
			next.State, next.FailureCode = contracts.InstallationStateFailed, InstallationFailureCodePhysicallyAbsent
		case contracts.ReleaseInstallationInvalid:
			if next.State == contracts.InstallationStateFailed && next.FailureCode == InstallationFailureCodePhysicallyInvalid {
				return nil
			}
			next.State, next.FailureCode = contracts.InstallationStateFailed, InstallationFailureCodePhysicallyInvalid
		case contracts.ReleaseInstallationIndeterminate:
			return nil
		default:
			return errors.New("installation observation state is invalid")
		}
		revision := tx.AdvanceRevision()
		connector.Installation = next
		connector.Revision = revision
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: connector.Key,
			OperationID: operationID, Revision: revision})
	})
}
