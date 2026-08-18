package application

import (
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"path/filepath"
	"strings"
)

func (application *service) compatibilityFor(manifest contracts.Manifest) (contracts.Compatibility, error) {
	if !application.config.ImplementationRegistry.Supports(manifest.Implementation.Kind) {
		return contracts.Compatibility{State: contracts.CompatibilityStateUnsupportedImplementation, Reason: "unsupported_implementation"}, nil
	}
	compatibility := application.config.Compatibility.Evaluate(manifest)
	switch compatibility.State {
	case contracts.CompatibilityStateSupported, contracts.CompatibilityStateUnsupportedProduct,
		contracts.CompatibilityStateUnsupportedPlatform, contracts.CompatibilityStateUnsupportedVersion:
		return compatibility, nil
	default:
		return contracts.Compatibility{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable,
			"connector compatibility evaluator returned an invalid state", false, nil)
	}
}

func newCatalogConnector(release contracts.Release) contracts.Connector {
	return contracts.Connector{Key: release.ConnectorKey, Release: release,
		Installation:  contracts.Installation{State: contracts.InstallationStateNotInstalled},
		Authorization: initialAuthorization(release.Manifest.AuthorizationKind),
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported}}
}

func initialAuthorization(kind string) contracts.Authorization {
	if kind == "none" {
		return contracts.Authorization{State: contracts.AuthorizationStateNotRequired}
	}
	return contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
}

// authorizationForManifest migrates stored state when catalog metadata
// corrects whether a connector requires credentials.
func authorizationForManifest(current contracts.Authorization, kind string) contracts.Authorization {
	if kind == "none" {
		return contracts.Authorization{State: contracts.AuthorizationStateNotRequired}
	}
	if current.State == contracts.AuthorizationStateNotRequired {
		return contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	}
	return current
}

func frozenRelease(operation contracts.Operation) (contracts.Release, error) {
	release, err := frozenReleaseIdentity(operation)
	if err != nil {
		return contracts.Release{}, err
	}
	if err := contracts.ValidateReleaseShape(release); err != nil {
		return contracts.Release{}, err
	}
	return release, nil
}

func frozenReleaseIdentity(operation contracts.Operation) (contracts.Release, error) {
	if operation.Target == nil || operation.Target.Release == nil {
		return contracts.Release{}, invalidOperationReceipt("operation does not contain a frozen release")
	}
	release := *operation.Target.Release
	if release.ConnectorKey != operation.ConnectorKey || release.ReleaseID != operation.Target.ReleaseID ||
		release.ReleaseDigest != operation.Target.ReleaseDigest || release.Version != operation.Target.Version {
		return contracts.Release{}, invalidOperationReceipt("operation release identity is inconsistent")
	}
	return release, nil
}

func validatePreparedArtifact(operation contracts.Operation, release contracts.Release, receipt contracts.PreparedArtifactReceipt) error {
	if receipt.OperationID != operation.OperationID || receipt.ConnectorKey != release.ConnectorKey ||
		receipt.Version != release.Version || receipt.ReleaseDigest != release.ReleaseDigest ||
		receipt.ArtifactSHA256 != release.Artifact.SHA256 ||
		!contracts.IsArtifactSHA256(receipt.InventoryDigest) ||
		(strings.TrimSpace(receipt.PreparedPath) == "" && strings.TrimSpace(receipt.OpaqueArtifactRef) == "") {
		return invalidOperationReceipt("artifact preparer returned a mismatched receipt")
	}
	return nil
}

func validateReleaseInstallationReceipt(operation contracts.Operation, release contracts.Release, receipt contracts.ReleaseInstallationReceipt) error {
	if receipt.OperationID != operation.OperationID || receipt.ConnectorKey != release.ConnectorKey ||
		receipt.Version != release.Version || receipt.ReleaseID != release.ReleaseID ||
		receipt.ReleaseDigest != release.ReleaseDigest || receipt.ArtifactSHA256 != release.Artifact.SHA256 {
		return invalidOperationReceipt("release installer returned a mismatched receipt")
	}
	if err := validatePreparedArtifact(operation, release, receipt.Artifact); err != nil {
		return err
	}
	cliInstall := releaseCLIInstallation(release)
	if cliInstall == nil {
		if receipt.CLIInstallation != nil {
			return invalidOperationReceipt("release installer returned an unexpected CLI receipt")
		}
		return nil
	}
	if receipt.CLIInstallation == nil {
		return invalidOperationReceipt("release installer did not return the required CLI receipt")
	}
	return validateCLIInstallationReceipt(operation, release, *cliInstall, *receipt.CLIInstallation)
}

func releaseCLIInstallation(release contracts.Release) *contracts.NodePackageInstallation {
	managed := release.Manifest.Implementation.ManagedStdio
	if managed == nil || managed.CLI == nil || managed.CLI.Install == nil || managed.CLI.Install.NodePackage == nil {
		return nil
	}
	return managed.CLI.Install.NodePackage
}

func validateCLIInstallationReceipt(operation contracts.Operation, release contracts.Release, install contracts.NodePackageInstallation, receipt contracts.CLIInstallationReceipt) error {
	if receipt.SchemaVersion != "tutti.connector.cli-installation.v1" ||
		receipt.OperationID != operation.OperationID || receipt.ConnectorKey != release.ConnectorKey ||
		receipt.ReleaseDigest != release.ReleaseDigest || receipt.Package != install.Package ||
		receipt.PackageVersion != install.Version || receipt.PackageIntegrity != install.Integrity ||
		receipt.LaunchKind != install.Launch.Kind || receipt.EntrypointSize <= 0 ||
		!contracts.IsArtifactSHA256(receipt.NodeSHA256) ||
		!contracts.IsArtifactSHA256(receipt.EntrypointSHA256) ||
		strings.TrimSpace(receipt.RuntimeProfile) == "" || strings.TrimSpace(receipt.RuntimeABI) == "" ||
		strings.TrimSpace(receipt.NodeVersion) == "" || !contracts.IsSafeRelativeEntrypoint(receipt.Entrypoint) {
		return invalidOperationReceipt("CLI installer returned a mismatched receipt")
	}
	localReceipt := filepath.IsAbs(receipt.InstallRoot) && filepath.IsAbs(receipt.StoreRoot) &&
		contracts.IsArtifactSHA256(receipt.LockSHA256)
	remoteReceipt := strings.TrimSpace(receipt.OpaqueInstallationRef) != ""
	if !localReceipt && !remoteReceipt {
		return invalidOperationReceipt("CLI installer returned a mismatched receipt")
	}
	return nil
}

func invalidOperationReceipt(message string) error {
	return contracts.NewDomainError(contracts.ErrorCodeInstallFailed,
		fmt.Sprintf("invalid connector operation receipt: %s", message), false, nil)
}
