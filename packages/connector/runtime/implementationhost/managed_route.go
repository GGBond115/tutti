package implementationhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
	"github.com/tutti-os/tutti/packages/connector/runtime/mcp"
	connectorprocess "github.com/tutti-os/tutti/packages/connector/runtime/process"
)

type connectorRoute struct {
	id                     string
	connectionID           string
	connectorKey           string
	connectorVersion       string
	releaseDigest          string
	generation             contracts.HostGeneration
	mcpTools               map[string]registeredMCPTool
	closeMu                sync.Mutex
	mcpClient              *mcp.StdioClient
	remoteMCP              RemoteMCPClient
	executionRoot          string
	installedRoot          string
	displayName            string
	description            string
	routingAliases         []string
	skillRoot              string
	skills                 []contracts.ConnectorSkillSummary
	processes              *connectorprocess.Group
	snapshots              *connectorruntime.ExecutionSnapshotter
	userHome               string
	cliLaunch              *managedCLILaunch
	cliCommand             string
	cliInvocationCommand   string
	cliContractHash        string
	cliCommands            []contracts.CLICommand
	cliShimPath            string
	cliShimContent         []byte
	credentialBrokerLaunch *managedCredentialBrokerLaunch
	readiness              contracts.RuntimeReadiness
}

func (route *connectorRoute) RouteID() string                           { return route.id }
func (route *connectorRoute) RouteGeneration() contracts.HostGeneration { return route.generation }
func (route *connectorRoute) RouteReleaseDigest() string                { return route.releaseDigest }
func (route *connectorRoute) Fence()                                    { route.processes.Fence() }
func (route *connectorRoute) close(deadline time.Time) error            { return route.Close(deadline) }
func (route *connectorRoute) releaseProcess(id uint64, connection connectorprocess.Connection) error {
	if route != nil && route.processes != nil {
		return route.processes.ReleaseWithError(id, connection)
	}
	return nil
}

func (route *connectorRoute) Close(deadline time.Time) error {
	if route == nil {
		return nil
	}
	route.closeMu.Lock()
	defer route.closeMu.Unlock()
	route.removeCLIShimIfCurrent()
	var remoteErr error
	if route.remoteMCP != nil {
		closeCtx, cancel := context.WithDeadline(context.Background(), deadline)
		remoteErr = route.remoteMCP.Close(closeCtx)
		cancel()
	}
	closeErr := route.processes.Close(deadline)
	if closeErr == nil {
		closeErr = remoteErr
	}
	if closeErr == nil && route.executionRoot != "" {
		if err := route.snapshots.Remove(route.executionRoot); err != nil {
			closeErr = err
		} else {
			route.executionRoot = ""
		}
	}
	return closeErr
}

func (route *connectorRoute) prepareCLIShim(binDir string) error {
	if route == nil || route.cliLaunch == nil {
		return nil
	}
	command := "tutti-connector-" + route.connectorKey
	if runtime.GOOS == "windows" {
		command += ".cmd"
	}
	path := filepath.Join(binDir, command)
	content, err := connectorCLIShimContent(route)
	if err != nil {
		return err
	}
	route.cliCommand, route.cliShimPath, route.cliShimContent = command, path, content
	return nil
}

func (route *connectorRoute) activateCLIShim() error {
	if route == nil || route.cliShimPath == "" || len(route.cliShimContent) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(route.cliShimPath), 0o700); err != nil {
		return fmt.Errorf("create connector CLI bin directory: %w", err)
	}
	temporary := route.cliShimPath + ".tmp-" + fmt.Sprintf("%d", route.generation.Generation)
	if err := os.WriteFile(temporary, route.cliShimContent, 0o700); err != nil {
		return fmt.Errorf("write connector CLI shim: %w", err)
	}
	if err := os.Rename(temporary, route.cliShimPath); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("activate connector CLI shim: %w", err)
	}
	return nil
}

func (route *connectorRoute) removeCLIShimIfCurrent() {
	if route == nil || route.cliShimPath == "" {
		return
	}
	current, err := os.ReadFile(route.cliShimPath)
	if err == nil && string(current) == string(route.cliShimContent) {
		_ = os.Remove(route.cliShimPath)
	}
}

func connectorCLIShimContent(route *connectorRoute) ([]byte, error) {
	launch := route.cliLaunch
	if launch == nil || strings.TrimSpace(launch.executable.Path) == "" {
		return nil, errors.New("connector CLI launch is unavailable")
	}
	arguments := append([]string(nil), launch.arguments...)
	if runtime.GOOS == "windows" {
		values := append([]string{launch.executable.Path}, arguments...)
		for _, value := range append(values, launch.cwd, launch.stateDir, route.userHome) {
			if strings.ContainsAny(value, "\r\n\"") {
				return nil, errors.New("connector CLI path cannot be represented by Windows shim")
			}
		}
		quoted := make([]string, 0, len(values))
		for _, value := range values {
			quoted = append(quoted, `"`+value+`"`)
		}
		content := "@echo off\r\n" +
			"set \"TUTTI_CONNECTOR_CONNECTION_ID=" + route.connectionID + "\"\r\n" +
			"set \"TUTTI_CONNECTOR_KEY=" + route.connectorKey + "\"\r\n" +
			"set \"TUTTI_CONNECTOR_LANGUAGE=" + launch.language + "\"\r\n" +
			"set \"TUTTI_CONNECTOR_STATE_DIR=" + launch.stateDir + "\"\r\n" +
			"set \"HOME=" + route.userHome + "\"\r\n" +
			"set \"USERPROFILE=" + route.userHome + "\"\r\n" +
			"cd /d \"" + launch.cwd + "\"\r\n" + strings.Join(quoted, " ") + " %*\r\n"
		return []byte(content), nil
	}
	values := append([]string{launch.executable.Path}, arguments...)
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	content := "#!/bin/sh\n" +
		"export TUTTI_CONNECTOR_CONNECTION_ID=" + shellQuote(route.connectionID) + "\n" +
		"export TUTTI_CONNECTOR_KEY=" + shellQuote(route.connectorKey) + "\n" +
		"export TUTTI_CONNECTOR_LANGUAGE=" + shellQuote(launch.language) + "\n" +
		"export TUTTI_CONNECTOR_STATE_DIR=" + shellQuote(launch.stateDir) + "\n" +
		"export HOME=" + shellQuote(route.userHome) + "\n" +
		"export USERPROFILE=" + shellQuote(route.userHome) + "\n" +
		"cd " + shellQuote(launch.cwd) + " || exit 1\n" +
		"exec " + strings.Join(quoted, " ") + " \"$@\"\n"
	return []byte(content), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

var _ connectorruntime.ManagedRoute = (*connectorRoute)(nil)
