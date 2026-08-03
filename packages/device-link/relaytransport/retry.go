package relaytransport

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type retryAfterError interface {
	HTTPRetryAfter() string
}

func retryDelay(err error, now time.Time) time.Duration {
	var retryErr retryAfterError
	if !errors.As(err, &retryErr) {
		return 0
	}
	value := strings.TrimSpace(retryErr.HTTPRetryAfter())
	if value == "" {
		return 0
	}
	if seconds, parseErr := strconv.ParseUint(value, 10, 64); parseErr == nil {
		maxSeconds := uint64(math.MaxInt64 / int64(time.Second))
		if seconds > maxSeconds {
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(seconds) * time.Second
	}
	retryAt, parseErr := http.ParseTime(value)
	if parseErr != nil || !retryAt.After(now) {
		return 0
	}
	return retryAt.Sub(now)
}

func combineRetryDelay(backoff, requested time.Duration) time.Duration {
	if requested <= 0 {
		return backoff
	}
	if backoff > time.Duration(math.MaxInt64)-requested {
		return time.Duration(math.MaxInt64)
	}
	return backoff + requested
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
