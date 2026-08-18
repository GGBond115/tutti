package agenthost

// AppServerRuntimePreparation is provider-preparation data transported through
// Host unchanged. Host does not own the process, Thread, or cleanup lifecycle.
type AppServerRuntimePreparation struct {
	ProviderStateID          string
	ExecutionHostID          string
	RuntimeGeneration        string
	TransportScopeID         string
	ProcessProfileDigest     string
	ProcessCwd               string
	ProcessEnv               []string
	ThreadEnv                []string
	ModelProviderCredentials []AppServerModelProviderCredential
	BaseInstructions         string
	DeveloperInstructions    string
}

type AppServerModelProviderCredential struct {
	ModelProviderID string
	BearerToken     string
}
