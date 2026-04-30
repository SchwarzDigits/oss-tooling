package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	gogithub "github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Clients bundles the REST and GraphQL clients sharing a single rate-limit-
// aware HTTP transport.
type Clients struct {
	REST *gogithub.Client
	GQL  *githubv4.Client
}

// NewClients constructs production clients pointing at api.github.com.
func NewClients(ctx context.Context, token string, logger *slog.Logger) (*Clients, error) {
	return NewClientsWithBaseURL(ctx, token, "", logger)
}

// NewClientsWithBaseURL is the test-friendly constructor. If baseURL is empty,
// the production GitHub endpoints are used. baseURL is the root of a server
// that responds to both REST and GraphQL paths (tests typically use one
// httptest.Server demuxed by path).
func NewClientsWithBaseURL(ctx context.Context, token, baseURL string, logger *slog.Logger) (*Clients, error) {
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := authedHTTPClient(ctx, token, logger)
	rest := gogithub.NewClient(httpClient)

	var gql *githubv4.Client
	if baseURL == "" {
		gql = githubv4.NewClient(httpClient)
	} else {
		u, err := normalizeBaseURL(baseURL)
		if err != nil {
			return nil, err
		}
		rest.BaseURL = u
		rest.UploadURL = u
		gql = githubv4.NewEnterpriseClient(strings.TrimRight(baseURL, "/")+"/graphql", httpClient)
	}

	return &Clients{REST: rest, GQL: gql}, nil
}

func authedHTTPClient(ctx context.Context, token string, logger *slog.Logger) *http.Client {
	var base http.RoundTripper = http.DefaultTransport
	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		base = oauth2.NewClient(ctx, ts).Transport
	}
	rl := newRateLimitTransport(base, logger)
	return &http.Client{Transport: rl}
}

func normalizeBaseURL(raw string) (*url.URL, error) {
	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	return u, nil
}
