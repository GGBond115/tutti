package main

import (
	"path/filepath"
	"testing"

	accountservice "github.com/tutti-os/tutti/services/tuttid/service/account"
	devicepresenceservice "github.com/tutti-os/tutti/services/tuttid/service/devicepresence"
)

func TestBuildDevicePresenceServiceUsesGenericControlPlaneOverride(t *testing.T) {
	t.Setenv("TUTTI_DESKTOP_CONTROL_PLANE_BASE_URL", "https://desktop.example/api/v1")
	t.Setenv("TUTTI_MOBILE_CONTROL_PLANE_BASE_URL", "https://legacy.example/api/v1")
	t.Setenv("TUTTI_PPE_LANE", "ppe-a")
	stateDir := t.TempDir()
	account := accountservice.NewService(filepath.Join(stateDir, "account", "auth.json"))

	service, err := buildDevicePresenceService(stateDir, account)
	if err != nil {
		t.Fatal(err)
	}
	control, ok := service.Control.(*devicepresenceservice.HTTPControlPlane)
	if !ok {
		t.Fatalf("control plane = %T", service.Control)
	}
	if control.BaseURL != "https://desktop.example/api/v1" || control.Headers.Get("x-zk-ppe-lane") != "ppe-a" {
		t.Fatalf("control plane config = %#v", control)
	}
	if service.Metadata.DeviceID == "" || service.Metadata.Platform == "" || service.Metadata.Arch == "" {
		t.Fatalf("device metadata = %+v", service.Metadata)
	}
}

func TestBuildDevicePresenceServiceFallsBackToLegacyMobileOverride(t *testing.T) {
	t.Setenv("TUTTI_DESKTOP_CONTROL_PLANE_BASE_URL", "")
	t.Setenv("TUTTI_MOBILE_CONTROL_PLANE_BASE_URL", "https://legacy.example/api/v1")
	stateDir := t.TempDir()
	account := accountservice.NewService(filepath.Join(stateDir, "account", "auth.json"))

	service, err := buildDevicePresenceService(stateDir, account)
	if err != nil {
		t.Fatal(err)
	}
	control := service.Control.(*devicepresenceservice.HTTPControlPlane)
	if control.BaseURL != "https://legacy.example/api/v1" {
		t.Fatalf("control plane base URL = %q", control.BaseURL)
	}
}
