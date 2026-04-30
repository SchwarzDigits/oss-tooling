package inventory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v66/github"

	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
)

// Collector orchestrates listing + enrichment + writing for one or more orgs.
type Collector struct {
	Clients     *gh.Clients
	Logger      *slog.Logger
	Concurrency int
	OutputDir   string
	// Exclude is a list of repository names (not full names) to skip.
	// Comparison is case-insensitive. A common default is [".github"].
	Exclude []string
	Now     func() time.Time
}

// Run processes orgs sequentially. Per-repo enrichment is parallel within
// each org. Returns the run summary plus a hard error if collection failed
// outright (e.g., bad token, missing org). Per-repo issues are folded into
// the data and do not abort the run.
func (c *Collector) Run(ctx context.Context, orgs []string) (Summary, error) {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}

	now := c.Now().UTC()
	dateStamp := now.Format("2006-01-02")

	summary := Summary{Orgs: len(orgs)}

	for _, org := range orgs {
		if err := ctx.Err(); err != nil {
			return summary, err
		}

		c.Logger.Info("collecting", "org", org)
		repos, err := ListRepositories(ctx, c.Clients, org, now)
		if err != nil {
			if isAuthError(err) {
				return summary, fmt.Errorf("authentication failed: check GITHUB_TOKEN: %w", err)
			}
			return summary, fmt.Errorf("list repositories for %q: %w", org, err)
		}
		listed := len(repos)
		repos = filterExcluded(repos, c.Exclude)
		excluded := listed - len(repos)
		if excluded > 0 {
			c.Logger.Info("listed", "org", org, "repos", len(repos), "excluded", excluded)
		} else {
			c.Logger.Info("listed", "org", org, "repos", len(repos))
		}

		if err := runPool(ctx, repos, c.Concurrency, func(ctx context.Context, r *Repository) error {
			return enrichRepo(ctx, c.Clients, r)
		}); err != nil {
			return summary, err
		}

		inv := OrgInventory{
			Org:           org,
			CollectedAt:   now,
			SchemaVersion: SchemaVersion,
			Repositories:  repos,
		}
		if err := writeOrgInventory(c.OutputDir, dateStamp, inv); err != nil {
			return summary, fmt.Errorf("write inventory for %q: %w", org, err)
		}

		c.Logger.Info("wrote", "org", org, "repos", len(repos),
			"file", fmt.Sprintf("%s/%s/%s.json", c.OutputDir, dateStamp, org))

		updateSummary(&summary, repos, now)
	}

	return summary, nil
}

// filterExcluded returns repos with any entry whose Name (case-insensitive)
// matches an item in exclude removed. Empty exclude returns repos unchanged.
func filterExcluded(repos []Repository, exclude []string) []Repository {
	if len(exclude) == 0 {
		return repos
	}
	skip := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		skip[strings.ToLower(e)] = struct{}{}
	}
	if len(skip) == 0 {
		return repos
	}
	out := make([]Repository, 0, len(repos))
	for _, r := range repos {
		if _, drop := skip[strings.ToLower(r.Name)]; drop {
			continue
		}
		out = append(out, r)
	}
	return out
}

func updateSummary(s *Summary, repos []Repository, now time.Time) {
	staleCutoff := now.Add(-365 * 24 * time.Hour)
	for _, r := range repos {
		s.TotalRepos++
		if r.HasLicense {
			s.WithLicense++
		} else {
			s.WithoutLicense++
		}
		if !r.HasReadme {
			s.MissingReadme++
		}
		if !r.HasSecurity {
			s.MissingSecurity++
		}
		if r.UsesComplianceWorkflow {
			s.UsesComplianceWorkflow++
		}
		if r.CommitsLast365d == 0 && r.PushedAt.Before(staleCutoff) {
			s.StaleNoCommits365d++
		}
	}
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	var ghErr *gogithub.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil &&
		ghErr.Response.StatusCode == http.StatusUnauthorized {
		return true
	}
	// githubv4 wraps 401s as plain errors carrying "401 Unauthorized" or
	// "Bad credentials" in the message string.
	msg := err.Error()
	return strings.Contains(msg, "401 Unauthorized") || strings.Contains(msg, "Bad credentials")
}
