package connectormarket

import (
	"context"
	"testing"
	"time"

	market "github.com/tutti-os/tutti/packages/connector/market/daemon"
)

type activationGateDelegate struct {
	reconciles int
	revokes    int
}

func (delegate *activationGateDelegate) Reconcile(_ context.Context, request market.WorkspaceReconcileRequest) (market.WorkspaceRuntimeReceipt, error) {
	delegate.reconciles++
	return market.WorkspaceRuntimeReceipt{OperationID: request.OperationID, WorkspaceID: request.WorkspaceID,
		ConnectorKey: request.Connector.Key, ReleaseDigest: request.Connector.Release.ReleaseDigest, Generation: request.Generation}, nil
}
func (delegate *activationGateDelegate) Revoke(context.Context, market.SecurityRevocationRequest) error {
	delegate.revokes++
	return nil
}
func (*activationGateDelegate) FailClosed(context.Context, time.Time) error { return nil }

func TestActivationGateStagesRecoveryUntilTrustedBootstrapCommit(t *testing.T) {
	delegate := &activationGateDelegate{}
	gate := newActivationGateHost(delegate)
	request := market.WorkspaceReconcileRequest{OperationID: "recover-1", WorkspaceID: "workspace-1", Enabled: true,
		Generation: market.HostGeneration{BootEpoch: "boot-1", Generation: 7}, Connector: market.Connector{Key: "github",
			Release: market.Release{ReleaseDigest: "release-1"}}}
	receipt, err := gate.Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if delegate.reconciles != 0 || receipt.Generation != request.Generation {
		t.Fatalf("closed gate delegated recovery: reconciles=%d receipt=%#v", delegate.reconciles, receipt)
	}
	gate.setOpen(true)
	if _, err := gate.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if delegate.reconciles != 1 {
		t.Fatalf("open gate reconciles = %d, want 1", delegate.reconciles)
	}
}

func TestActivationGateNeverStagesSecurityRevocation(t *testing.T) {
	delegate := &activationGateDelegate{}
	gate := newActivationGateHost(delegate)
	if err := gate.Revoke(context.Background(), market.SecurityRevocationRequest{WorkspaceID: "workspace-1", ConnectorKey: "github"}); err != nil {
		t.Fatal(err)
	}
	if delegate.revokes != 1 {
		t.Fatalf("revokes = %d, want 1", delegate.revokes)
	}
}
