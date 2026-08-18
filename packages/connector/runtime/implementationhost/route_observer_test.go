package implementationhost

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
	"github.com/tutti-os/tutti/packages/connector/runtime/mcp"
	connectorprocess "github.com/tutti-os/tutti/packages/connector/runtime/process"
)

const routeObserverTestDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

type exitedMCPConnection struct{ delivered bool }

func (*exitedMCPConnection) Send([]byte) error { return nil }
func (*exitedMCPConnection) Close() error      { return nil }
func (*exitedMCPConnection) CloseInput() error { return nil }
func (*exitedMCPConnection) Terminate() error  { return nil }
func (*exitedMCPConnection) Kill() error       { return nil }
func (connection *exitedMCPConnection) Recv() (connectorprocess.Frame, error) {
	if connection.delivered {
		return connectorprocess.Frame{}, io.EOF
	}
	connection.delivered = true
	exitCode := 17
	return connectorprocess.Frame{ExitCode: &exitCode}, nil
}

func TestMonitorMCPRoutePublishesUnexpectedCurrentRouteExit(t *testing.T) {
	routes := connectorruntime.NewRouteTable()
	host := &Host{routes: routes, observations: newRouteObservationHub()}
	watchContext, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	watch, err := host.Watch(watchContext)
	if err != nil {
		t.Fatal(err)
	}
	generation := contracts.HostGeneration{BootEpoch: "boot-1", Generation: 4}
	route := &connectorRoute{id: connectorRouteKey("connection-1", "calendar"), connectionID: "connection-1",
		connectorKey: "calendar", releaseDigest: routeObserverTestDigest, generation: generation,
		readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady}, processes: connectorprocess.NewGroup()}
	if err := routes.Commit(route); err != nil {
		t.Fatal(err)
	}
	client, err := mcp.NewStdioClient(mcp.StdioClientConfig{Connection: &exitedMCPConnection{}, ProcessName: "calendar"})
	if err != nil {
		t.Fatal(err)
	}
	host.monitorMCPRoute(route, client)
	event := <-watch.Events
	if event.Revision != watch.Revision+1 || event.Kind != contracts.PhysicalRouteEventUnexpectedExit ||
		event.Route.ConnectorKey != "calendar" || event.Route.ConnectionID != "connection-1" ||
		event.Route.ReleaseDigest != routeObserverTestDigest || event.Route.Generation != generation {
		t.Fatalf("route event = %+v, watch revision = %d", event, watch.Revision)
	}
	if routes.IsCurrent(route) {
		t.Fatal("exited MCP route remained current")
	}
}

func TestMonitorMCPRouteDoesNotReportIntentionalRemovalAsUnexpected(t *testing.T) {
	for _, action := range []string{"remove", "replace", "close"} {
		t.Run(action, func(t *testing.T) {
			routes := connectorruntime.NewRouteTable()
			host := &Host{routes: routes, observations: newRouteObservationHub()}
			watchContext, cancelWatch := context.WithCancel(context.Background())
			defer cancelWatch()
			watch, err := host.Watch(watchContext)
			if err != nil {
				t.Fatal(err)
			}
			generation := contracts.HostGeneration{BootEpoch: "boot-1", Generation: 1}
			route := &connectorRoute{id: connectorRouteKey("connection-1", "calendar"), connectionID: "connection-1",
				connectorKey: "calendar", releaseDigest: routeObserverTestDigest, generation: generation,
				processes: connectorprocess.NewGroup()}
			if err := routes.Commit(route); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "remove":
				err = routes.Remove(route.id, generation, routeObserverTestDigest, time.Now().Add(time.Second))
			case "replace":
				replacement := &connectorRoute{id: route.id, connectionID: route.connectionID, connectorKey: route.connectorKey,
					releaseDigest: route.releaseDigest, generation: contracts.HostGeneration{BootEpoch: "boot-1", Generation: 2},
					processes: connectorprocess.NewGroup()}
				err = routes.Commit(replacement)
			case "close":
				err = routes.Close(time.Now().Add(time.Second))
			}
			if err != nil {
				t.Fatal(err)
			}
			client, clientErr := mcp.NewStdioClient(mcp.StdioClientConfig{Connection: &exitedMCPConnection{}, ProcessName: "calendar"})
			if clientErr != nil {
				t.Fatal(clientErr)
			}
			host.monitorMCPRoute(route, client)
			select {
			case event := <-watch.Events:
				t.Fatalf("intentional %s emitted event = %+v", action, event)
			default:
			}
		})
	}
}

func TestPhysicalRouteSnapshotIncludesUnpublishedLogicalRoute(t *testing.T) {
	routes := connectorruntime.NewRouteTable()
	host := &Host{routes: routes, observations: newRouteObservationHub()}
	generation := contracts.HostGeneration{BootEpoch: "boot-1", Generation: 9}
	route := &connectorRoute{id: connectorRouteKey("connection-1", "calendar"), connectionID: "connection-1",
		connectorKey: "calendar", releaseDigest: routeObserverTestDigest, generation: generation,
		readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady}, processes: connectorprocess.NewGroup(),
		cliLaunch: &managedCLILaunch{}}
	if err := routes.Commit(route); err != nil {
		t.Fatal(err)
	}
	routes.SetPublished(false)
	snapshot, err := host.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Routes) != 1 || snapshot.Routes[0].ConnectorKey != "calendar" ||
		snapshot.Routes[0].Generation != generation || snapshot.Routes[0].State != contracts.PhysicalRouteStateReady {
		t.Fatalf("physical route snapshot = %+v", snapshot)
	}
}

func TestPhysicalRouteWatchClosesOnBoundedBufferOverflow(t *testing.T) {
	hub := newRouteObservationHub()
	watchContext, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	watch, err := hub.watch(watchContext)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= physicalRouteWatchBuffer; index++ {
		hub.publish(contracts.PhysicalRouteEventChanged, contracts.PhysicalRoute{ConnectorKey: "calendar"})
	}
	count := 0
	for range watch.Events {
		count++
	}
	if count != physicalRouteWatchBuffer {
		t.Fatalf("buffered events before overflow close = %d, want %d", count, physicalRouteWatchBuffer)
	}
}

func TestPhysicalRouteSnapshotRevisionIsLinearWithRoutes(t *testing.T) {
	host := &Host{routes: connectorruntime.NewRouteTable(), observations: newRouteObservationHub()}
	writerDone := make(chan error, 1)
	go func() {
		for generation := uint64(1); generation <= 200; generation++ {
			route := &connectorRoute{id: connectorRouteKey("connection-1", "calendar"), connectionID: "connection-1",
				connectorKey: "calendar", releaseDigest: routeObserverTestDigest,
				generation: contracts.HostGeneration{BootEpoch: "boot-1", Generation: generation},
				readiness:  contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady}, processes: connectorprocess.NewGroup()}
			host.routeObservationMu.Lock()
			err := host.routes.Commit(route)
			if err == nil {
				host.observations.publish(contracts.PhysicalRouteEventChanged, physicalRoute(route))
			}
			host.routeObservationMu.Unlock()
			if err != nil {
				writerDone <- err
				return
			}
		}
		writerDone <- nil
	}()
	for {
		snapshot, err := host.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Routes) == 1 && snapshot.Revision != snapshot.Routes[0].Generation.Generation {
			t.Fatalf("non-linear snapshot revision=%d route generation=%d", snapshot.Revision,
				snapshot.Routes[0].Generation.Generation)
		}
		select {
		case err := <-writerDone:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}
}
