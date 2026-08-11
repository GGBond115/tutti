package agentstatus

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	"github.com/tutti-os/tutti/packages/agent/daemon/providerstatus"
)

const claudeKeychainReadTimeout = 3 * time.Second

func (s Service) resolveRemoteAuthEvidence(
	ctx context.Context,
	spec ProviderSpec,
) (providerstatus.AuthEvidence, bool) {
	if s.RemoteAuthProbe != nil {
		return s.RemoteAuthProbe(ctx, spec)
	}
	token, found, err := s.resolveRemoteAuthCredential(ctx, spec.RemoteAuthProbe.CredentialKind)
	if err != nil {
		slog.Debug("agent provider remote auth credential unavailable",
			"event", "tutti.agent_provider.remote_auth.credential_failed",
			"provider", spec.Provider,
			"error", err,
		)
		return providerstatus.AuthEvidence{
			Kind: providerstatus.AuthEvidenceProbeFailure, Reason: providerstatus.AuthReasonProbeFailed,
		}, true
	}
	if !found {
		return providerstatus.AuthEvidence{}, false
	}
	result := providerstatus.ProbeRemoteAuth(ctx, s.httpClient(), spec.RemoteAuthProbe, token)
	level := slog.LevelDebug
	if result.Evidence.Kind == providerstatus.AuthEvidenceRemoteAuthFailure {
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, "agent provider remote auth probe completed",
		"event", "tutti.agent_provider.remote_auth.completed",
		"provider", spec.Provider,
		"evidence", result.Evidence.Kind,
		"statusCode", result.StatusCode,
		"success", result.Evidence.Kind == providerstatus.AuthEvidenceRemoteSuccess,
	)
	return result.Evidence, true
}

func (s Service) resolveRemoteAuthCredential(
	ctx context.Context,
	kind providerregistry.RemoteAuthCredentialKind,
) (string, bool, error) {
	switch kind {
	case providerregistry.RemoteAuthCredentialKindClaudeOAuth:
		return s.resolveClaudeOAuthAccessToken(ctx)
	default:
		return "", false, fmt.Errorf("remote auth credential kind %q is unsupported", kind)
	}
}

func (s Service) resolveClaudeOAuthAccessToken(ctx context.Context) (string, bool, error) {
	configDir := strings.TrimSpace(s.lookupEnv("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		home, err := s.homeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", false, err
		}
		configDir = filepath.Join(home, ".claude")
	}
	var keychainErr error
	if runtime.GOOS == "darwin" {
		for _, service := range providerstatus.ClaudeOAuthKeychainServices(configDir) {
			content, err := readClaudeKeychainCredential(ctx, service)
			if err != nil {
				keychainErr = err
				continue
			}
			if token, ok := providerstatus.ClaudeOAuthAccessToken(content); ok {
				return token, true, nil
			}
		}
	}
	content, err := os.ReadFile(filepath.Join(configDir, ".credentials.json"))
	if err == nil {
		if token, ok := providerstatus.ClaudeOAuthAccessToken(content); ok {
			return token, true, nil
		}
		return "", false, fmt.Errorf("claude OAuth credentials do not contain an access token")
	}
	if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("read Claude OAuth credentials: %w", err)
	}
	if keychainErr != nil {
		return "", false, keychainErr
	}
	return "", false, nil
}

func readClaudeKeychainCredential(ctx context.Context, service string) ([]byte, error) {
	readCtx, cancel := context.WithTimeout(ctx, claudeKeychainReadTimeout)
	defer cancel()
	output, err := exec.CommandContext(
		readCtx,
		"/usr/bin/security",
		"find-generic-password",
		"-s",
		service,
		"-w",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("read Claude Keychain service %q: %w", service, err)
	}
	if len(strings.TrimSpace(string(output))) == 0 {
		return nil, fmt.Errorf("claude Keychain service %q is empty", service)
	}
	return output, nil
}
