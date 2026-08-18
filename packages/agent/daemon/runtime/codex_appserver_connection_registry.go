package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime"
	"sort"
	"strings"
)

func newAppServerConnectionRegistry(adapter *CodexAppServerAdapter) *appServerConnectionRegistry {
	return &appServerConnectionRegistry{
		adapter:  adapter,
		byKey:    make(map[appServerConnectionKey]*appServerConnection),
		byClient: make(map[*codexAppServerClient]*appServerConnection),
		retired:  make(map[uint64]*appServerConnection),
	}
}

type appServerPreparedLaunch struct {
	spec           ProcessSpec
	profile        *AppServerProcessProfile
	overlay        AppServerThreadOverlay
	processCleanup func(context.Context) error
	threadCleanup  func(context.Context) error
}

func appServerConnectionKeyForLaunch(
	transport ProcessTransport,
	launch appServerPreparedLaunch,
) (appServerConnectionKey, error) {
	spec := launch.spec
	if launch.profile == nil {
		return appServerConnectionKey{}, errors.New(
			"app-server launch requires explicit process profile preparation",
		)
	}
	provider := strings.TrimSpace(spec.Provider)
	key := appServerConnectionKey{Provider: provider}
	if spec.ExecutableIdentity != nil {
		key.ExecutableIdentity = strings.TrimSpace(spec.ExecutableIdentity.SHA256)
	}
	profile := launch.profile
	key.ExecutionHostID = strings.TrimSpace(profile.ExecutionHostID)
	key.RuntimeGeneration = strings.TrimSpace(profile.RuntimeGeneration)
	key.TransportScopeID = strings.TrimSpace(profile.TransportScopeID)
	key.ProcessProfileDigest = strings.TrimSpace(profile.ProcessProfileDigest)
	launchDigest := appServerProcessProfileDigest(spec.Command, spec.Env, spec.CWD)
	key.ProcessProfileDigest = key.ProcessProfileDigest + ":" + launchDigest
	if tracking, ok := transport.(ProviderInputUnitTrackingTransport); ok && tracking.TracksProviderInputUnits() {
		key.CaptureScope = firstNonEmpty(strings.TrimSpace(spec.RootAgentSessionID), strings.TrimSpace(spec.AgentSessionID))
	}
	return key, nil
}

func appServerProcessProfileDigest(command, env []string, cwd string) string {
	return appServerProcessProfileDigestForPlatform(command, env, cwd, runtime.GOOS)
}

func appServerProcessProfileDigestForPlatform(command, env []string, cwd, goos string) string {
	effective := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			key, value = entry, ""
		}
		if strings.EqualFold(strings.TrimSpace(goos), "windows") {
			key = strings.ToUpper(key)
		}
		effective[key] = value
	}
	keys := make([]string, 0, len(effective))
	for key := range effective {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	normalizedEnv := make([][2]string, 0, len(keys))
	for _, key := range keys {
		normalizedEnv = append(normalizedEnv, [2]string{key, effective[key]})
	}
	payload, _ := json.Marshal(struct {
		Command []string    `json:"command"`
		Env     [][2]string `json:"env"`
		CWD     string      `json:"cwd"`
	}{Command: command, Env: normalizedEnv, CWD: strings.TrimSpace(cwd)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (r *appServerConnectionRegistry) acquire(
	_ context.Context,
	key appServerConnectionKey,
	start func(uint64) (*appServerConnection, error),
) (*appServerConnection, bool, error) {
	if r == nil {
		return nil, false, errors.New("app-server connection registry is unavailable")
	}
	// Holding the registry lock through startup is the single-flight fence.
	// Startup is rare and connection keys are adapter-local; a later scheduler
	// can make unrelated profiles parallel without changing ownership.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return nil, false, errors.New("app-server connection registry is shutting down")
	}
	if existing := r.byKey[key]; existing != nil && existing.healthy() {
		return existing, true, nil
	}
	generation := r.next.Add(1)
	connection, err := start(generation)
	if err != nil {
		return nil, false, err
	}
	connection.registry = r
	connection.key = key
	connection.generation = generation
	r.byKey[key] = connection
	r.byClient[connection.client] = connection
	go connection.watchDone()
	return connection, false, nil
}

func (r *appServerConnectionRegistry) connectionForClient(client *codexAppServerClient) *appServerConnection {
	if r == nil || client == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byClient[client]
}

func (r *appServerConnectionRegistry) retire(connection *appServerConnection) {
	if r == nil || connection == nil {
		return
	}
	r.mu.Lock()
	if r.byKey[connection.key] == connection {
		delete(r.byKey, connection.key)
	}
	delete(r.byClient, connection.client)
	r.retired[connection.generation] = connection
	r.mu.Unlock()
}

func (r *appServerConnectionRegistry) completeRetired(connection *appServerConnection, closeErr error) {
	if r == nil || connection == nil || closeErr != nil {
		return
	}
	r.mu.Lock()
	delete(r.retired, connection.generation)
	r.mu.Unlock()
}

func (r *appServerConnectionRegistry) retryOneRetired() (bool, error) {
	if r == nil {
		return false, nil
	}
	r.mu.Lock()
	var pendingCleanup *appServerPendingCleanup
	if len(r.pendingCleanups) > 0 {
		pendingCleanup = r.pendingCleanups[0]
	}
	connections := make(map[uint64]*appServerConnection, len(r.byKey)+len(r.retired))
	for _, connection := range r.byKey {
		connections[connection.generation] = connection
	}
	for generation, connection := range r.retired {
		connections[generation] = connection
	}
	var target *appServerConnection
	for _, connection := range r.retired {
		target = connection
		break
	}
	r.mu.Unlock()
	if pendingCleanup != nil {
		err := pendingCleanup.cleanup(context.Background())
		if err == nil {
			r.mu.Lock()
			for index, candidate := range r.pendingCleanups {
				if candidate == pendingCleanup {
					r.pendingCleanups = append(r.pendingCleanups[:index], r.pendingCleanups[index+1:]...)
					break
				}
			}
			r.mu.Unlock()
		}
		return true, err
	}
	for _, connection := range connections {
		if attempted, err := connection.retryOneThreadCleanup(); attempted {
			return true, err
		}
	}
	if target == nil {
		return false, nil
	}
	err := target.client.Close()
	r.completeRetired(target, err)
	return true, err
}

func (r *appServerConnectionRegistry) retainCleanup(cleanup func(context.Context) error) {
	if r == nil || cleanup == nil {
		return
	}
	r.mu.Lock()
	r.pendingCleanups = append(r.pendingCleanups, &appServerPendingCleanup{cleanup: cleanup})
	r.mu.Unlock()
}

func (r *appServerConnectionRegistry) cleanupOrRetain(cleanup func(context.Context) error) error {
	err := cleanupPreparedLaunch(cleanup)
	if err != nil {
		r.retainCleanup(cleanup)
	}
	return err
}

func (r *appServerConnectionRegistry) shutdownAll() LiveSessionResourceCleanupResult {
	var result LiveSessionResourceCleanupResult
	if r == nil {
		return result
	}
	r.mu.Lock()
	r.shuttingDown = true
	pendingCleanups := append([]*appServerPendingCleanup(nil), r.pendingCleanups...)
	targets := make(map[uint64]*appServerConnection, len(r.byKey)+len(r.retired))
	for _, connection := range r.byKey {
		targets[connection.generation] = connection
	}
	for generation, connection := range r.retired {
		targets[generation] = connection
	}
	r.byKey = make(map[appServerConnectionKey]*appServerConnection)
	r.byClient = make(map[*codexAppServerClient]*appServerConnection)
	for _, connection := range targets {
		connection.mu.Lock()
		connection.closing = true
		connection.mu.Unlock()
		r.retired[connection.generation] = connection
	}
	r.mu.Unlock()
	for _, pending := range pendingCleanups {
		result.Attempted++
		if err := pending.cleanup(context.Background()); err != nil {
			result.Failed++
			continue
		}
		result.Cleaned++
		r.mu.Lock()
		for index, candidate := range r.pendingCleanups {
			if candidate == pending {
				r.pendingCleanups = append(r.pendingCleanups[:index], r.pendingCleanups[index+1:]...)
				break
			}
		}
		r.mu.Unlock()
	}
	for _, connection := range targets {
		_, attempted, cleaned, failed, cleanupErr := connection.cleanupBindings()
		result.Attempted += attempted
		result.Cleaned += cleaned
		result.Failed += failed
		for {
			cleanupAttempted, retryErr := connection.retryOneThreadCleanup()
			if !cleanupAttempted {
				break
			}
			result.Attempted++
			if retryErr != nil {
				result.Failed++
				cleanupErr = errors.Join(cleanupErr, retryErr)
				break
			}
			result.Cleaned++
		}
		if !connection.hasRetiredThreadCleanups() {
			cleanupErr = nil
		}
		result.Attempted++
		err := errors.Join(cleanupErr, connection.client.Close())
		r.completeRetired(connection, err)
		if err != nil {
			result.Failed++
		} else {
			result.Cleaned++
		}
	}
	return result
}

func (r *appServerConnectionRegistry) closeOneIdle() (bool, error) {
	if r == nil {
		return false, nil
	}
	r.mu.Lock()
	var target *appServerConnection
	for _, connection := range r.byKey {
		connection.mu.Lock()
		idle := !connection.dead && !connection.closing && len(connection.bindingsBySession) == 0 &&
			len(connection.retiredThreadCleanups) == 0
		if idle {
			connection.closing = true
			target = connection
		}
		connection.mu.Unlock()
		if target != nil {
			break
		}
	}
	if target != nil {
		if r.byKey[target.key] == target {
			delete(r.byKey, target.key)
		}
		delete(r.byClient, target.client)
		r.retired[target.generation] = target
	}
	r.mu.Unlock()
	if target == nil {
		return false, nil
	}
	err := target.client.Close()
	r.completeRetired(target, err)
	return true, err
}

func (r *appServerConnectionRegistry) closeIfIdle(target *appServerConnection) (bool, error) {
	if r == nil || target == nil {
		return false, nil
	}
	r.mu.Lock()
	ownedByKey := r.byKey[target.key] == target
	ownedRetired := r.retired[target.generation] == target
	target.mu.Lock()
	idle := !target.dead && len(target.bindingsBySession) == 0 &&
		len(target.retiredThreadCleanups) == 0 &&
		((ownedByKey && !target.closing) || ownedRetired)
	if idle {
		target.closing = true
	}
	target.mu.Unlock()
	if !idle || (!ownedByKey && !ownedRetired) {
		r.mu.Unlock()
		return false, nil
	}
	if ownedByKey {
		delete(r.byKey, target.key)
		delete(r.byClient, target.client)
		r.retired[target.generation] = target
	}
	r.mu.Unlock()
	err := target.client.Close()
	r.completeRetired(target, err)
	return true, err
}

func (c *appServerConnection) healthy() bool {
	if c == nil || c.client == nil {
		return false
	}
	c.mu.Lock()
	dead := c.dead || c.closing
	c.mu.Unlock()
	if dead {
		return false
	}
	select {
	case <-c.client.Done():
		return false
	default:
		return true
	}
}

func (c *appServerConnection) watchDone() {
	if c == nil || c.client == nil {
		return
	}
	<-c.client.Done()
	c.mu.Lock()
	if c.dead {
		c.mu.Unlock()
		return
	}
	c.dead = true
	c.mu.Unlock()
	if c.registry != nil {
		c.registry.retire(c)
	}
	bindings, _, _, _, cleanupErr := c.cleanupBindings()
	closeErr := errors.Join(cleanupErr, c.client.Close())
	if c.registry != nil {
		c.registry.completeRetired(c, closeErr)
	}
	if c.registry != nil && c.registry.adapter != nil {
		c.registry.adapter.handleAppServerConnectionDeath(c, bindings)
	}
}

func (c *appServerConnection) cleanupBindings() ([]*appServerThreadBinding, int, int, int, error) {
	if c == nil {
		return nil, 0, 0, 0, nil
	}
	c.bindingCleanupMu.Lock()
	defer c.bindingCleanupMu.Unlock()
	c.mu.Lock()
	bindings := make([]*appServerThreadBinding, 0, len(c.bindingsBySession))
	seen := make(map[*appServerThreadBinding]struct{}, len(c.bindingsBySession)+len(c.replacementByThread))
	for _, binding := range c.bindingsBySession {
		bindings = append(bindings, binding)
		seen[binding] = struct{}{}
	}
	for _, binding := range c.replacementByThread {
		if _, ok := seen[binding]; ok {
			continue
		}
		bindings = append(bindings, binding)
		seen[binding] = struct{}{}
	}
	c.bindingsBySession = make(map[string]*appServerThreadBinding)
	c.ownerByThread = make(map[string]string)
	c.replacementByThread = make(map[string]*appServerThreadBinding)
	c.unknownByThread = make(map[string][]appServerRoutedMessage)
	c.unknownCount = 0
	c.unknownBytes = 0
	c.mu.Unlock()
	cleaned := 0
	failed := 0
	var cleanupErr error
	for _, binding := range bindings {
		if err := c.closeBinding(binding); err != nil {
			failed++
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			cleaned++
		}
	}
	if c.hasRetiredThreadCleanups() && cleanupErr == nil {
		cleanupErr = errors.New("app-server thread cleanup remains pending")
	}
	return bindings, len(bindings), cleaned, failed, cleanupErr
}

func (c *appServerConnection) retryOneThreadCleanup() (bool, error) {
	if c == nil {
		return false, nil
	}
	c.bindingCleanupMu.Lock()
	defer c.bindingCleanupMu.Unlock()
	c.mu.Lock()
	var binding *appServerThreadBinding
	for candidate := range c.retiredThreadCleanups {
		binding = candidate
		break
	}
	c.mu.Unlock()
	if binding == nil {
		return false, nil
	}
	return true, c.closeBinding(binding)
}

func (c *appServerConnection) hasRetiredThreadCleanups() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.retiredThreadCleanups) > 0
}

func (c *appServerConnection) forceClose() error {
	if c == nil || c.client == nil {
		return nil
	}
	c.mu.Lock()
	if c.dead {
		c.mu.Unlock()
		return nil
	}
	c.closing = true
	c.mu.Unlock()
	if c.registry != nil {
		c.registry.retire(c)
	}
	// watchDone is the single finalizer. It removes registry ownership only
	// after every active Binding has either cleaned successfully or been
	// retained for retry.
	return c.client.Close()
}
