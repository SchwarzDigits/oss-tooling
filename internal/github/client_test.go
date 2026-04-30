package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v66/github"
)

func TestRateLimitTransport_PreflightSleep(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	// X-RateLimit-Reset is serialized as Unix seconds (integer), so we lose up
	// to one second of resolution. Use 3s so that even after truncation the
	// effective sleep is comfortably above the assertion threshold.
	resetIn := 3 * time.Second

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("X-RateLimit-Remaining", "5")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(resetIn).Unix(), 10))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `{"login":"x"}`)
	}))
	defer srv.Close()

	clients, err := NewClientsWithBaseURL(context.Background(), "test-token", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewClientsWithBaseURL: %v", err)
	}

	ctx := context.Background()
	if _, _, err := clients.REST.Users.Get(ctx, "octocat"); err != nil {
		t.Fatalf("first request: %v", err)
	}

	start := time.Now()
	if _, _, err := clients.REST.Users.Get(ctx, "octocat"); err != nil {
		t.Fatalf("second request: %v", err)
	}
	elapsed := time.Since(start)

	const minSleep = 1500 * time.Millisecond
	if elapsed < minSleep {
		t.Fatalf("expected pre-flight sleep ≥ %v, got %v", minSleep, elapsed)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
}

func TestRateLimitTransport_RetryAfter403(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"message":"abuse"}`, http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `{"login":"x"}`)
	}))
	defer srv.Close()

	clients, err := NewClientsWithBaseURL(context.Background(), "test-token", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewClientsWithBaseURL: %v", err)
	}

	start := time.Now()
	_, err = WithRetry(context.Background(), func(ctx context.Context) (*gogithub.User, error) {
		u, _, err := clients.REST.Users.Get(ctx, "octocat")
		return u, err
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("WithRetry: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected retry to wait ≥ ~1s, got %v", elapsed)
	}
}

func TestWithRetry_NoRetryOn404(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	clients, err := NewClientsWithBaseURL(context.Background(), "test-token", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewClientsWithBaseURL: %v", err)
	}

	_, err = WithRetry(context.Background(), func(ctx context.Context) (*gogithub.User, error) {
		u, _, err := clients.REST.Users.Get(ctx, "ghost")
		return u, err
	})
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 call (no retry on 404), got %d", calls.Load())
	}
}

// TestRateLimitTransport_CtxCancelDuringSleep exercises the transport directly
// (without go-github, which has its own client-side rate-limit short-circuit
// that would intercept before our transport's pre-flight sleep runs).
func TestRateLimitTransport_CtxCancelDuringSleep(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rl := newRateLimitTransport(http.DefaultTransport, nil)
	httpClient := &http.Client{Transport: rl}

	// First request primes nextReadyAt to ~60s out.
	req1, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp1, err := httpClient.Do(req1)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	resp1.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err = httpClient.Do(req2)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected ctx-canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("ctx cancel did not interrupt sleep promptly, elapsed=%v", elapsed)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected only 1 call, got %d", calls.Load())
	}
}
