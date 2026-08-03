package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maximumPolicyResponseBytes = 64 * 1024

type Checker interface {
	Check(context.Context, Identity) ([]byte, error)
}

type HTTPChecker struct {
	Client    *http.Client
	Endpoint  string
	UserAgent string
}

func (checker HTTPChecker) Check(ctx context.Context, identity Identity) ([]byte, error) {
	if checker.Client == nil {
		return nil, errors.New("desktop update admission HTTP client is not configured")
	}
	if strings.TrimSpace(checker.Endpoint) == "" {
		return nil, errors.New("desktop update admission endpoint is not configured")
	}
	body, err := json.Marshal(identity)
	if err != nil {
		return nil, fmt.Errorf("encode desktop update admission request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, checker.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create desktop update admission request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if checker.UserAgent != "" {
		request.Header.Set("User-Agent", checker.UserAgent)
	}
	response, err := checker.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send desktop update admission request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumPolicyResponseBytes))
		return nil, fmt.Errorf("desktop update admission request returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumPolicyResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read desktop update admission response: %w", err)
	}
	if len(raw) > maximumPolicyResponseBytes {
		return nil, errors.New("desktop update admission response exceeds size limit")
	}
	return raw, nil
}
