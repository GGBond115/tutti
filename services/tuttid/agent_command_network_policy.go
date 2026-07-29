package main

import "github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"

func tuttiDesktopCommandNetworkAccessPolicy(provider string) bool {
	descriptor, ok := providerregistry.Find(provider)
	return ok &&
		descriptor.Runtime.Kind == providerregistry.RuntimeKindCodexAppServer &&
		descriptor.Sidecar.ExecutionEnvironment == providerregistry.SidecarExecutionEnvironmentLocalIPC
}
