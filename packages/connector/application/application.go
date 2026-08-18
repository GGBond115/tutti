package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Repository               Repository
	CatalogSource            CatalogSource
	ReleaseInstallations     ReleaseInstallationManager
	ImplementationCommands   ImplementationCommands
	Authorization            AuthorizationProvider
	AuthorizationProjections AuthorizationProjectionStore
	AuthorizationSnapshots   AuthorizationSnapshotSource
	AuthorizationReadiness   *AuthorizationReadinessGate
	SharedAgentSupport       SharedAgentSupportSource
	AgentConnectorGrants     AgentConnectorGrantSource
	RuntimeBindings          RuntimeBindingResolver
	RuntimeIntents           RuntimeIntentResolver
	Compatibility            CompatibilityEvaluator
	Scheduler                OperationScheduler
	ImplementationRegistry   ImplementationRegistry
	WorkerID                 string
	BootEpoch                string
	LeaseDuration            time.Duration
	Now                      func() time.Time
	NewID                    func() (string, error)
	RuntimeRetryJitter       func(time.Duration) time.Duration
}

type service struct {
	config Config

	// executionMu and inFlight provide process-local ownership for operation
	// execution. Durable recovery remains the repository and adapter contract.
	executionMu           sync.Mutex
	inFlight              map[string]*operationExecution
	authorizationMu       sync.Mutex
	authorizationLanes    map[string]*sync.Mutex
	authorizationRequests map[string]*authorizationRequestExecution
}

type operationExecution struct {
	done chan struct{}
	err  error
}

type authorizationRequestExecution struct {
	clientRequestID string
	context         context.Context
	cancel          context.CancelFunc
	references      int
	replacedLive    bool
}

func newService(config Config) (*service, error) {
	if config.Repository == nil {
		return nil, errors.New("connector market repository is required")
	}
	if config.CatalogSource == nil {
		return nil, errors.New("connector market catalog source is required")
	}
	if config.ReleaseInstallations == nil {
		return nil, errors.New("connector market release installation manager is required")
	}
	if config.ImplementationCommands == nil {
		return nil, errors.New("connector market implementation host is required")
	}
	if config.Authorization == nil {
		return nil, errors.New("connector market authorization provider is required")
	}
	if config.RuntimeBindings == nil {
		config.RuntimeBindings = defaultRuntimeBindingResolver{}
	}
	if config.RuntimeIntents == nil {
		config.RuntimeIntents = defaultRuntimeBindingResolver{}
	}
	if config.Compatibility == nil {
		return nil, errors.New("connector market compatibility evaluator is required")
	}
	if config.Scheduler == nil {
		return nil, errors.New("connector market operation scheduler is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = randomID
	}
	if config.RuntimeRetryJitter == nil {
		config.RuntimeRetryJitter = runtimeFullJitter
	}
	if strings.TrimSpace(config.WorkerID) == "" {
		workerID, err := config.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate connector market worker id: %w", err)
		}
		config.WorkerID = workerID
	}
	if strings.TrimSpace(config.BootEpoch) == "" {
		bootEpoch, err := config.NewID()
		if err != nil {
			return nil, fmt.Errorf("generate connector market boot epoch: %w", err)
		}
		config.BootEpoch = bootEpoch
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 30 * time.Second
	}
	return &service{config: config, inFlight: make(map[string]*operationExecution),
		authorizationLanes:    make(map[string]*sync.Mutex),
		authorizationRequests: make(map[string]*authorizationRequestExecution)}, nil
}
