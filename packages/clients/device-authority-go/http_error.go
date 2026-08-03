package deviceauthority

import (
	"fmt"
	"net/http"
	"strings"
)

const maxHTTPErrorBodyBytes = 4096

// HTTPError preserves bounded control-plane status metadata for adapter-owned
// retry policy. Callers should avoid logging Body because upstream responses
// are product data and may contain sensitive details.
type HTTPError struct {
	StatusCode int
	Body       string
	RetryAfter string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("control-plane request failed (%d)", e.StatusCode)
}

func (e *HTTPError) HTTPStatusCode() int { return e.StatusCode }

func (e *HTTPError) HTTPRetryAfter() string { return e.RetryAfter }

func newHTTPError(statusCode int, body []byte, retryAfter string) *HTTPError {
	if len(body) > maxHTTPErrorBodyBytes {
		body = body[:maxHTTPErrorBodyBytes]
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return &HTTPError{
		StatusCode: statusCode,
		Body:       message,
		RetryAfter: strings.TrimSpace(retryAfter),
	}
}
