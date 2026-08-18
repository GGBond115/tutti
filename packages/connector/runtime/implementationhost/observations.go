package implementationhost

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

// AuthorizationObserver receives runtime-side credential binding outcomes.
// The owning product may project them upstream; this Host does not become the
// account authorization truth source.
type AuthorizationObserver interface {
	ObserveAuthorization(context.Context, AuthorizationObservation)
}

type AuthorizationObservation struct {
	ConnectorKey string
	ConnectionID string
	State        contracts.AuthorizationState
	ObservedAt   time.Time
}

const (
	maxPhysicalRouteSnapshot = 4096
	physicalRouteWatchBuffer = 64
)

type routeObservationHub struct {
	mu          sync.Mutex
	revision    uint64
	nextWatcher uint64
	watchers    map[uint64]*routeWatcher
	closed      bool
}

type routeWatcher struct {
	events chan contracts.PhysicalRouteEvent
	done   chan struct{}
}

func newRouteObservationHub() *routeObservationHub {
	return &routeObservationHub{watchers: make(map[uint64]*routeWatcher)}
}

func (hub *routeObservationHub) publish(kind contracts.PhysicalRouteEventKind, route contracts.PhysicalRoute) {
	if hub == nil {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return
	}
	hub.revision++
	event := contracts.PhysicalRouteEvent{Revision: hub.revision, Kind: kind, Route: route}
	for watcherID, watcher := range hub.watchers {
		select {
		case watcher.events <- event:
		default:
			// Losing one edge makes this stream untrustworthy. Closing it forces
			// the consumer through Snapshot instead of silently dropping state.
			delete(hub.watchers, watcherID)
			close(watcher.events)
			close(watcher.done)
		}
	}
}

func (hub *routeObservationHub) watch(ctx context.Context) (contracts.PhysicalRouteWatch, error) {
	if hub == nil || ctx == nil {
		return contracts.PhysicalRouteWatch{}, errors.New("connector physical route watch context is required")
	}
	if err := ctx.Err(); err != nil {
		return contracts.PhysicalRouteWatch{}, err
	}
	hub.mu.Lock()
	if hub.closed {
		hub.mu.Unlock()
		return contracts.PhysicalRouteWatch{}, errors.New("connector physical route observation is closed")
	}
	hub.nextWatcher++
	watcherID := hub.nextWatcher
	watcher := &routeWatcher{events: make(chan contracts.PhysicalRouteEvent, physicalRouteWatchBuffer), done: make(chan struct{})}
	hub.watchers[watcherID] = watcher
	revision := hub.revision
	hub.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			hub.mu.Lock()
			if current := hub.watchers[watcherID]; current == watcher {
				delete(hub.watchers, watcherID)
				close(watcher.events)
				close(watcher.done)
			}
			hub.mu.Unlock()
		case <-watcher.done:
		}
	}()
	return contracts.PhysicalRouteWatch{Revision: revision, Events: watcher.events}, nil
}

func (hub *routeObservationHub) currentRevision() uint64 {
	if hub == nil {
		return 0
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.revision
}

func (hub *routeObservationHub) close() {
	if hub == nil {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.closed {
		return
	}
	hub.closed = true
	for watcherID, watcher := range hub.watchers {
		delete(hub.watchers, watcherID)
		close(watcher.events)
		close(watcher.done)
	}
}

func physicalRoute(route *connectorRoute) contracts.PhysicalRoute {
	state := contracts.PhysicalRouteStateReady
	if route == nil || route.readiness.State != contracts.RuntimeReadinessReady {
		state = contracts.PhysicalRouteStateDegraded
	}
	if route == nil {
		return contracts.PhysicalRoute{State: state}
	}
	return contracts.PhysicalRoute{
		ConnectorKey: route.connectorKey, ConnectionID: route.connectionID,
		ReleaseDigest: route.releaseDigest, Generation: route.generation, State: state,
	}
}

func sortPhysicalRoutes(routes []contracts.PhysicalRoute) {
	sort.Slice(routes, func(left, right int) bool {
		if routes[left].ConnectorKey != routes[right].ConnectorKey {
			return routes[left].ConnectorKey < routes[right].ConnectorKey
		}
		return routes[left].ConnectionID < routes[right].ConnectionID
	})
}
