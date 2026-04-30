package github

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	gogithub "github.com/google/go-github/v66/github"
)

const maxRetryAttempts = 3

var retryBackoff = []time.Duration{
	1 * time.Second,
	3 * time.Second,
	9 * time.Second,
}

// WithRetry runs op up to 3 times with jittered backoff. It retries on:
//   - context-non-cancellation network errors
//   - HTTP 5xx
//   - HTTP 429
//   - HTTP 403 with rate-limit indicators (Retry-After header or
//     X-RateLimit-Remaining: 0)
//
// It does NOT retry on 401, 404, 422, or any other 4xx, and exits immediately
// on context cancellation.
func WithRetry[T any](ctx context.Context, op func(context.Context) (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		v, err := op(ctx)
		if err == nil {
			return v, nil
		}
		lastErr = err

		if !isRetryable(err) {
			return zero, err
		}

		if attempt == maxRetryAttempts-1 {
			break
		}

		delay := jitter(retryBackoff[attempt])
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}

	return zero, lastErr
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var rateErr *gogithub.RateLimitError
	if errors.As(err, &rateErr) {
		return true
	}
	var abuseErr *gogithub.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return true
	}

	var ghErr *gogithub.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		return retryableStatus(ghErr.Response.StatusCode, ghErr.Response.Header)
	}

	// Network and other transport-level errors.
	return true
}

func retryableStatus(status int, h http.Header) bool {
	switch {
	case status >= 500 && status < 600:
		return true
	case status == http.StatusTooManyRequests:
		return true
	case status == http.StatusForbidden:
		if h.Get("Retry-After") != "" {
			return true
		}
		if h.Get("X-RateLimit-Remaining") == "0" {
			return true
		}
		return false
	default:
		return false
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	span := float64(d) * 0.4
	delta := (rand.Float64() - 0.5) * span
	return d + time.Duration(delta)
}
