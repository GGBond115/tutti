package relaytransport

import (
	"errors"
	"math"
	"net/http"
	"testing"
	"time"
)

type testRetryError struct{ value string }

func (testRetryError) Error() string            { return "retry requested" }
func (e testRetryError) HTTPRetryAfter() string { return e.value }

func TestRetryDelayParsesDeltaSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		err  error
		want time.Duration
	}{
		{name: "delta seconds", err: testRetryError{value: " 17 "}, want: 17 * time.Second},
		{name: "future HTTP date", err: testRetryError{value: now.Add(45 * time.Second).Format(http.TimeFormat)}, want: 45 * time.Second},
		{name: "past HTTP date", err: testRetryError{value: now.Add(-time.Second).Format(http.TimeFormat)}},
		{name: "invalid", err: testRetryError{value: "later"}},
		{name: "unrelated error", err: errors.New("network down")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryDelay(tt.err, now); got != tt.want {
				t.Fatalf("retryDelay() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRetryDelaySaturatesLargeDeltaSeconds(t *testing.T) {
	err := testRetryError{value: "18446744073709551615"}
	if got := retryDelay(err, time.Now()); got != time.Duration(math.MaxInt64) {
		t.Fatalf("retryDelay() = %s, want MaxInt64", got)
	}
}

func TestCombineRetryDelayAddsServerDelayAfterBackoff(t *testing.T) {
	if got := combineRetryDelay(250*time.Millisecond, 2*time.Second); got != 2250*time.Millisecond {
		t.Fatalf("combineRetryDelay() = %s, want 2.25s", got)
	}
	if got := combineRetryDelay(time.Duration(math.MaxInt64-1), time.Second); got != time.Duration(math.MaxInt64) {
		t.Fatalf("overflow result = %s, want MaxInt64", got)
	}
}
