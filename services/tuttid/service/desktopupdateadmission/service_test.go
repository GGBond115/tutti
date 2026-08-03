package desktopupdateadmission

import (
	"context"
	"testing"
)

func TestUnmanagedDaemonDoesNotCreateDesktopAdmissionService(t *testing.T) {
	t.Setenv(managedEnvironment, "")
	service, err := NewFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if service != nil {
		t.Fatalf("service = %#v, want nil", service)
	}
}

func TestManagedDevelopmentDaemonOwnsInProcessPolicy(t *testing.T) {
	t.Setenv(managedEnvironment, "1")
	t.Setenv(packagedEnvironment, "0")
	t.Setenv(currentVersionEnvironment, "1.0.0")
	t.Setenv(platformEnvironment, "macos")
	t.Setenv(architectureEnvironment, "arm64")
	t.Setenv("DESKTOP_UPDATE_ADMISSION_DEV", "1")
	t.Setenv("DESKTOP_UPDATE_ADMISSION_POLICY", "upgradeRequired")
	t.Setenv("DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION", "1.1.0")

	service, err := NewFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	service.Start(context.Background())
	snapshot, err := service.WaitInitial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Policy.Status != "resolved" ||
		snapshot.Policy.Response == nil ||
		snapshot.Policy.Response.Decision != "upgradeRequired" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	service.Close()
}
