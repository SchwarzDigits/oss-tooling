package github

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v66/github"
)

const topCommitterMaxPages = 5

var (
	botSuffixRE = regexp.MustCompile(`(?i)\[bot\]$`)
	knownBots   = map[string]struct{}{
		"github-actions": {},
		"dependabot":     {},
		"renovate":       {},
	}
)

// GetTopCommitter90d returns the GitHub login with the most commits on ref
// in the last 90 days. Bot accounts are skipped. Ties are broken
// alphabetically for determinism. Empty repos return ("", nil).
func GetTopCommitter90d(ctx context.Context, c *gogithub.Client, owner, repo, ref string) (string, error) {
	since := time.Now().Add(-90 * 24 * time.Hour)
	opt := &gogithub.CommitsListOptions{
		SHA:         ref,
		Since:       since,
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}

	counts := make(map[string]int)
	for page := 0; page < topCommitterMaxPages; page++ {
		var nextPage int
		commits, err := WithRetry(ctx, func(ctx context.Context) ([]*gogithub.RepositoryCommit, error) {
			cs, resp, err := c.Repositories.ListCommits(ctx, owner, repo, opt)
			if resp != nil {
				nextPage = resp.NextPage
			}
			return cs, err
		})
		if err != nil {
			return "", err
		}
		for _, com := range commits {
			login := com.GetAuthor().GetLogin()
			if login == "" || isBot(login) {
				continue
			}
			counts[login]++
		}
		if nextPage == 0 {
			break
		}
		opt.Page = nextPage
	}

	if len(counts) == 0 {
		return "", nil
	}
	logins := make([]string, 0, len(counts))
	for l := range counts {
		logins = append(logins, l)
	}
	sort.Strings(logins)
	top := logins[0]
	for _, l := range logins[1:] {
		if counts[l] > counts[top] {
			top = l
		}
	}
	return top, nil
}

func isBot(login string) bool {
	if botSuffixRE.MatchString(login) {
		return true
	}
	_, ok := knownBots[strings.ToLower(login)]
	return ok
}
