package devicepresence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/httpx"
)

const (
	DefaultControlPlaneBaseURL = "https://tutti.sh/api/desktop/v1"
	maxControlPlaneResponse    = 1 << 20
)

type ControlPlane interface {
	RegisterCurrentDevice(context.Context, string, DeviceMetadata) (RegisteredDevice, error)
	OpenSession(context.Context, string, string, string) (Lease, error)
	Heartbeat(context.Context, string, string) (Lease, error)
	CloseSession(context.Context, string, string) error
}

type DeviceMetadata struct {
	DeviceID      string
	ReportedName  string
	Platform      string
	Arch          string
	ClientVersion string
}

type RegisteredDevice struct {
	UserDeviceID string
	DeviceID     string
}

type Lease struct {
	PresenceLeaseID          string
	UserDeviceID             string
	State                    string
	HeartbeatIntervalSeconds int
	AuthorityGeneration      string
}

type HTTPControlPlane struct {
	BaseURL    string
	Headers    http.Header
	HTTPClient *http.Client
}

type ControlPlaneError struct {
	StatusCode int
	Code       string
	Reason     string
}

func (e *ControlPlaneError) Error() string {
	if e == nil {
		return ""
	}
	detail := strings.TrimSpace(e.Reason)
	if detail == "" {
		detail = strings.TrimSpace(e.Code)
	}
	if detail == "" {
		detail = "request rejected"
	}
	return fmt.Sprintf("device presence control-plane request failed (%d): %s", e.StatusCode, detail)
}

func (e *ControlPlaneError) IsStatus(statusCode int) bool {
	return e != nil && e.StatusCode == statusCode
}

func (c *HTTPControlPlane) RegisterCurrentDevice(ctx context.Context, cookie string, metadata DeviceMetadata) (RegisteredDevice, error) {
	request := struct {
		DeviceID      string `json:"deviceId"`
		ReportedName  string `json:"reportedName"`
		Platform      string `json:"platform"`
		Arch          string `json:"arch"`
		ClientVersion string `json:"clientVersion"`
	}{
		DeviceID: strings.TrimSpace(metadata.DeviceID), ReportedName: strings.TrimSpace(metadata.ReportedName),
		Platform: strings.TrimSpace(metadata.Platform), Arch: strings.TrimSpace(metadata.Arch),
		ClientVersion: strings.TrimSpace(metadata.ClientVersion),
	}
	var response struct {
		Device struct {
			UserDeviceID string `json:"userDeviceId"`
			DeviceID     string `json:"deviceId"`
		} `json:"device"`
	}
	if err := c.doJSON(ctx, http.MethodPut, "/devices/current", cookie, request, &response); err != nil {
		return RegisteredDevice{}, err
	}
	device := RegisteredDevice{
		UserDeviceID: strings.TrimSpace(response.Device.UserDeviceID),
		DeviceID:     strings.TrimSpace(response.Device.DeviceID),
	}
	if device.UserDeviceID == "" || device.DeviceID != strings.TrimSpace(metadata.DeviceID) {
		return RegisteredDevice{}, errors.New("device presence registration response is incomplete")
	}
	return device, nil
}

func (c *HTTPControlPlane) OpenSession(ctx context.Context, cookie, deviceID, presenceSessionID string) (Lease, error) {
	request := struct {
		DeviceID          string `json:"deviceId"`
		PresenceSessionID string `json:"presenceSessionId"`
	}{DeviceID: strings.TrimSpace(deviceID), PresenceSessionID: strings.TrimSpace(presenceSessionID)}
	var response struct {
		PresenceLeaseID          string `json:"presenceLeaseId"`
		UserDeviceID             string `json:"userDeviceId"`
		State                    string `json:"state"`
		HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds"`
		AuthorityGeneration      string `json:"authorityGeneration"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/device-presence/sessions", cookie, request, &response); err != nil {
		return Lease{}, err
	}
	lease := Lease{
		PresenceLeaseID: strings.TrimSpace(response.PresenceLeaseID), UserDeviceID: strings.TrimSpace(response.UserDeviceID),
		State: strings.TrimSpace(response.State), HeartbeatIntervalSeconds: response.HeartbeatIntervalSeconds,
		AuthorityGeneration: strings.TrimSpace(response.AuthorityGeneration),
	}
	if lease.PresenceLeaseID == "" || lease.UserDeviceID == "" || lease.HeartbeatIntervalSeconds <= 0 {
		return Lease{}, errors.New("device presence open response is incomplete")
	}
	return lease, nil
}

func (c *HTTPControlPlane) Heartbeat(ctx context.Context, cookie, leaseID string) (Lease, error) {
	path := "/device-presence/sessions/" + url.PathEscape(strings.TrimSpace(leaseID)) + "/heartbeat"
	var response struct {
		State               string `json:"state"`
		AuthorityGeneration string `json:"authorityGeneration"`
	}
	if err := c.doJSON(ctx, http.MethodPost, path, cookie, nil, &response); err != nil {
		return Lease{}, err
	}
	if !strings.HasSuffix(strings.ToUpper(strings.TrimSpace(response.State)), "_ACTIVE") {
		return Lease{}, errors.New("device presence heartbeat did not activate the lease")
	}
	return Lease{
		PresenceLeaseID: leaseID, State: strings.TrimSpace(response.State),
		AuthorityGeneration: strings.TrimSpace(response.AuthorityGeneration),
	}, nil
}

func (c *HTTPControlPlane) CloseSession(ctx context.Context, cookie, leaseID string) error {
	path := "/device-presence/sessions/" + url.PathEscape(strings.TrimSpace(leaseID))
	return c.doJSON(ctx, http.MethodDelete, path, cookie, nil, nil)
}

func (c *HTTPControlPlane) doJSON(ctx context.Context, method, path, cookie string, requestBody, responseBody any) error {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultControlPlaneBaseURL
	}
	var body io.Reader
	if requestBody != nil {
		raw, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode device presence request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create device presence request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, values := range c.Headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	request.Header.Set("Cookie", strings.TrimSpace(cookie))
	client := c.HTTPClient
	if client == nil {
		client = httpx.NewClient(5 * time.Second)
	}
	response, err := client.Do(request)
	if err != nil {
		return sanitizedTransportError(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxControlPlaneResponse+1))
	if err != nil {
		return fmt.Errorf("read device presence response: %w", err)
	}
	if len(raw) > maxControlPlaneResponse {
		return fmt.Errorf("device presence response exceeds %d bytes", maxControlPlaneResponse)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return decodeControlPlaneError(response.StatusCode, raw)
	}
	if responseBody == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, responseBody); err != nil {
		return fmt.Errorf("decode device presence response: %w", err)
	}
	return nil
}

func sanitizedTransportError(err error) error {
	for {
		requestErr, ok := err.(*url.Error)
		if !ok {
			break
		}
		err = requestErr.Err
	}
	return &controlPlaneTransportError{err: err}
}

type controlPlaneTransportError struct {
	err error
}

func (e *controlPlaneTransportError) Error() string {
	return "send device presence request: " + e.err.Error()
}

func (e *controlPlaneTransportError) Unwrap() error {
	return e.err
}

func (e *controlPlaneTransportError) Timeout() bool {
	var networkErr net.Error
	return errors.As(e.err, &networkErr) && networkErr.Timeout()
}

func decodeControlPlaneError(statusCode int, raw []byte) error {
	var response struct {
		Error struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &response)
	return &ControlPlaneError{
		StatusCode: statusCode,
		Code:       strings.TrimSpace(response.Error.Code),
		Reason:     strings.TrimSpace(response.Error.Reason),
	}
}
