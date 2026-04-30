package github

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// rateLimitTransport observes GitHub rate-limit headers and delays subsequent
// requests if the remaining quota is low, or if a Retry-After header was
// returned. It does not retry on its own — that is the caller's responsibility
// via WithRetry.
type rateLimitTransport struct {
	base         http.RoundTripper
	logger       *slog.Logger
	minRemaining int

	mu          sync.Mutex
	nextReadyAt time.Time
}

func newRateLimitTransport(base http.RoundTripper, logger *slog.Logger) *rateLimitTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &rateLimitTransport{
		base:         base,
		logger:       logger,
		minRemaining: 100,
	}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	ready := t.nextReadyAt
	t.mu.Unlock()

	if d := time.Until(ready); d > 0 {
		select {
		case <-time.After(d):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	t.observe(resp)
	return resp, nil
}

func (t *rateLimitTransport) observe(resp *http.Response) {
	rem, remOK := parseIntHeader(resp.Header.Get("X-RateLimit-Remaining"))
	resetUnix, resetOK := parseIntHeader(resp.Header.Get("X-RateLimit-Reset"))

	now := time.Now()
	var newReady time.Time

	if remOK && resetOK && rem < int64(t.minRemaining) {
		newReady = time.Unix(resetUnix, 0)
		t.logger.Warn("approaching GitHub rate limit",
			"remaining", rem,
			"reset_in", time.Until(newReady).Round(time.Second).String())
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
				candidate := now.Add(time.Duration(secs) * time.Second)
				if candidate.After(newReady) {
					newReady = candidate
				}
				t.logger.Warn("GitHub Retry-After received",
					"status", resp.StatusCode,
					"retry_after_seconds", secs)
			}
		}
	}

	if newReady.IsZero() {
		return
	}

	t.mu.Lock()
	if newReady.After(t.nextReadyAt) {
		t.nextReadyAt = newReady
	}
	t.mu.Unlock()
}

func parseIntHeader(v string) (int64, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
