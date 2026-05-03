package github

import (
	"context"
	"regexp"
	"sort"
	"strings"

	gogithub "github.com/google/go-github/v66/github"
)

var (
	botSuffixRE = regexp.MustCompile(`(?i)\[bot\]$`)
	knownBots   = map[string]struct{}{
		"github-actions": {},
		"dependabot":     {},
		"renovate":       {},
	}
)

// GetTopCommitter returns the GitHub login that authored the most non-bot
// commits in the recent history of ref. It looks at the last 100 commits;
// if every one of those is by a bot (rare but possible — e.g. a long
// stretch of dependabot PRs), it falls back to commits 101-200 before
// giving up. Empty repos and bot-only histories return ("", nil).
//
// The window is fixed by commit count rather than by date so a stale repo
// (no recent activity) still attributes correctly to whoever built it,
// whether that activity was last week or years ago.
func GetTopCommitter(ctx context.Context, c *gogithub.Client, owner, repo, ref string) (string, error) {
	if login, err := topCommitterPage(ctx, c, owner, repo, ref, 1); err != nil {
		return "", err
	} else if login != "" {
		return login, nil
	}
	return topCommitterPage(ctx, c, owner, repo, ref, 2)
}

// topCommitterPage fetches one page of 100 commits and returns the top
// non-bot author login on that page, or "" if the page yields nothing.
func topCommitterPage(ctx context.Context, c *gogithub.Client, owner, repo, ref string, page int) (string, error) {
	opt := &gogithub.CommitsListOptions{
		SHA: ref,
		ListOptions: gogithub.ListOptions{
			PerPage: 100,
			Page:    page,
		},
	}
	commits, err := WithRetry(ctx, func(ctx context.Context) ([]*gogithub.RepositoryCommit, error) {
		cs, _, err := c.Repositories.ListCommits(ctx, owner, repo, opt)
		return cs, err
	})
	if err != nil {
		return "", err
	}

	counts := make(map[string]int)
	for _, com := range commits {
		login := com.GetAuthor().GetLogin()
		if login == "" || isBot(login) {
			continue
		}
		counts[login]++
	}
	if len(counts) == 0 {
		return "", nil
	}

	logins := make([]string, 0, len(counts))
	for l := range counts {
		logins = append(logins, l)
	}
	sort.Strings(logins) // alphabetic tie-break for determinism
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
