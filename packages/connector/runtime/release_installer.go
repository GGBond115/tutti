package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

// ReleaseInstaller composes the same-machine artifact and optional CLI
// installation mechanics behind the host's single physical installation
// boundary. Remote products implement application.ReleaseInstallationManager in
// their control-plane adapter and use the same lower-level importer and CLI
// installer inside the runtime machine.
type ReleaseInstaller struct {
	artifacts application.ArtifactPreparer
	cli       application.CLIInstallationManager
}

var _ application.ReleaseInstallationManager = (*ReleaseInstaller)(nil)

func NewReleaseInstaller(
	artifacts application.ArtifactPreparer,
	cli application.CLIInstallationManager,
) (*ReleaseInstaller, error) {
	if artifacts == nil {
		return nil, errors.New("connector release artifact preparer is required")
	}
	return &ReleaseInstaller{artifacts: artifacts, cli: cli}, nil
}

func (installer *ReleaseInstaller) InstallRelease(
	ctx context.Context,
	request contracts.InstallReleaseRequest,
) (contracts.ReleaseInstallationReceipt, error) {
	if installer == nil || installer.artifacts == nil {
		return contracts.ReleaseInstallationReceipt{}, errors.New("connector release installer is unavailable")
	}
	if err := contracts.ValidateReleaseShape(request.Release); err != nil {
		return contracts.ReleaseInstallationReceipt{}, err
	}
	prepared, err := installer.artifacts.Prepare(ctx, contracts.PrepareArtifactRequest(request))
	if err != nil {
		return contracts.ReleaseInstallationReceipt{}, fmt.Errorf("prepare connector release artifact: %w", err)
	}

	receipt := contracts.ReleaseInstallationReceipt{
		OperationID:    request.OperationID,
		ConnectorKey:   request.Release.ConnectorKey,
		Version:        request.Release.Version,
		ReleaseID:      request.Release.ReleaseID,
		ReleaseDigest:  request.Release.ReleaseDigest,
		ArtifactSHA256: request.Release.Artifact.SHA256,
		Artifact:       prepared,
	}
	if !releaseRequiresCLIInstallation(request.Release) {
		return receipt, nil
	}
	if installer.cli == nil {
		return contracts.ReleaseInstallationReceipt{}, errors.New("connector CLI installation is required but unavailable")
	}
	cliReceipt, err := installer.cli.InstallCLI(ctx, contracts.InstallCLIRequest(request))
	if err != nil {
		rollbackErr := installer.artifacts.Remove(context.WithoutCancel(ctx), contracts.RemoveArtifactRequest{
			OperationID:   request.OperationID,
			Scope:         request.Scope,
			Generation:    request.Generation,
			ConnectorKey:  request.Release.ConnectorKey,
			Version:       request.Release.Version,
			ReleaseDigest: request.Release.ReleaseDigest,
		})
		return contracts.ReleaseInstallationReceipt{}, fmt.Errorf(
			"install connector CLI package: %w",
			errors.Join(err, rollbackErr),
		)
	}
	receipt.CLIInstallation = &cliReceipt
	return receipt, nil
}

func (installer *ReleaseInstaller) InspectReleaseInstallation(
	ctx context.Context,
	request contracts.InspectReleaseInstallationRequest,
) (contracts.ReleaseInstallationObservation, error) {
	observation := contracts.ReleaseInstallationObservation{
		ConnectorKey:  request.Release.ConnectorKey,
		ReleaseDigest: request.Release.ReleaseDigest,
	}
	if installer == nil || installer.artifacts == nil {
		observation.State = contracts.ReleaseInstallationIndeterminate
		observation.ReasonCode = "installation_manager_unavailable"
		return observation, nil
	}
	if err := contracts.ValidateRuntimeReleaseShape(request.Release); err != nil {
		return contracts.ReleaseInstallationObservation{}, err
	}
	prepared, err := installer.artifacts.ResolvePrepared(ctx, request.Release)
	if err != nil {
		return classifyReleaseInstallationError(observation, "artifact", err)
	}
	receipt := contracts.ReleaseInstallationReceipt{
		OperationID:    prepared.OperationID,
		ConnectorKey:   request.Release.ConnectorKey,
		Version:        request.Release.Version,
		ReleaseID:      request.Release.ReleaseID,
		ReleaseDigest:  request.Release.ReleaseDigest,
		ArtifactSHA256: request.Release.Artifact.SHA256,
		Artifact:       prepared,
	}
	if releaseRequiresCLIInstallation(request.Release) {
		if installer.cli == nil {
			observation.State = contracts.ReleaseInstallationInvalid
			observation.ReasonCode = "cli_inspector_unavailable"
			return observation, nil
		}
		cliReceipt, resolveErr := installer.cli.ResolveCLI(ctx, request.Release)
		if resolveErr != nil {
			return classifyReleaseInstallationError(observation, "cli", resolveErr)
		}
		receipt.CLIInstallation = &cliReceipt
	}
	observation.State = contracts.ReleaseInstallationPresent
	observation.Receipt = &receipt
	return observation, nil
}

func classifyReleaseInstallationError(
	observation contracts.ReleaseInstallationObservation,
	component string,
	err error,
) (contracts.ReleaseInstallationObservation, error) {
	switch {
	case errors.Is(err, contracts.ErrReleaseInstallationAbsent):
		observation.State = contracts.ReleaseInstallationAbsent
		observation.ReasonCode = component + "_absent"
		return observation, nil
	case errors.Is(err, contracts.ErrReleaseInstallationInvalid):
		observation.State = contracts.ReleaseInstallationInvalid
		observation.ReasonCode = component + "_invalid"
		return observation, nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		observation.State = contracts.ReleaseInstallationIndeterminate
		observation.ReasonCode = component + "_inspection_interrupted"
		return observation, nil
	default:
		return contracts.ReleaseInstallationObservation{}, err
	}
}

func (installer *ReleaseInstaller) UninstallRelease(
	ctx context.Context,
	request contracts.UninstallReleaseRequest,
) error {
	if installer == nil || installer.artifacts == nil {
		return errors.New("connector release installer is unavailable")
	}
	if err := contracts.ValidateRuntimeReleaseShape(request.Release); err != nil {
		return err
	}
	var cleanupErrors []error
	connectorRemoval := contracts.RemoveConnectorInstallationRequest{
		OperationID:  request.OperationID,
		Scope:        request.Scope,
		Generation:   request.Generation,
		ConnectorKey: request.Release.ConnectorKey,
	}
	if installer.cli == nil {
		if releaseRequiresCLIInstallation(request.Release) {
			cleanupErrors = append(cleanupErrors, errors.New("connector CLI installation manager is unavailable"))
		}
	} else {
		cleanupErrors = append(cleanupErrors, installer.cli.RemoveConnector(ctx, connectorRemoval))
	}
	cleanupErrors = append(cleanupErrors, installer.artifacts.RemoveConnector(ctx, connectorRemoval))
	return errors.Join(cleanupErrors...)
}

func (*ReleaseInstaller) CommitReleaseInstallation(
	context.Context,
	contracts.CommitReleaseInstallationRequest,
) error {
	// Same-machine preparation already atomically published its latest verified
	// archive. Remote adapters defer candidate promotion until this callback.
	return nil
}

func releaseRequiresCLIInstallation(release contracts.Release) bool {
	managed := release.Manifest.Implementation.ManagedStdio
	return managed != nil && managed.CLI != nil && managed.CLI.Install != nil &&
		managed.CLI.Install.NodePackage != nil
}
