package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

// OrgRepoNode is the per-repository GraphQL node returned by ListOrgRepos.
// Field shapes mirror the GraphQL schema; the inventory package translates
// these into its on-disk Repository struct.
type OrgRepoNode struct {
	Name           string
	NameWithOwner  string
	Description    string
	Visibility     string
	IsArchived     bool
	IsDisabled     bool
	IsFork         bool
	URL            string `graphql:"url"`
	CreatedAt      githubv4.DateTime
	UpdatedAt      githubv4.DateTime
	PushedAt       githubv4.DateTime
	StargazerCount int
	ForkCount      int

	Watchers struct {
		TotalCount int
	}
	Issues struct {
		TotalCount int
	} `graphql:"issues(states: OPEN)"`
	PullRequests struct {
		TotalCount int
	} `graphql:"pullRequests(states: OPEN)"`

	RepositoryTopics struct {
		Nodes []struct {
			Topic struct {
				Name string
			}
		}
	} `graphql:"repositoryTopics(first: 50)"`

	PrimaryLanguage struct {
		Name string
	}

	LicenseInfo struct {
		SpdxID string `graphql:"spdxId"`
		Name   string
	}

	DefaultBranchRef struct {
		Name   string
		Target struct {
			Commit struct {
				Commits30d struct {
					TotalCount int
				} `graphql:"commits30d: history(since: $since30)"`
				Commits90d struct {
					TotalCount int
				} `graphql:"commits90d: history(since: $since90)"`
				Commits365d struct {
					TotalCount int
				} `graphql:"commits365d: history(since: $since365)"`
				Authors90d struct {
					Nodes []struct {
						Author struct {
							User struct {
								Login string
							}
						}
					}
				} `graphql:"authors90d: history(since: $since90, first: 50)"`
			} `graphql:"... on Commit"`
		}
	}

	RootTree struct {
		Tree struct {
			Entries []struct {
				Name string
				Type string
			}
		} `graphql:"... on Tree"`
	} `graphql:"rootTree: object(expression: \"HEAD:\")"`

	Workflows struct {
		Tree struct {
			Entries []struct {
				Name   string
				Object struct {
					Blob struct {
						Text     string
						IsBinary bool
					} `graphql:"... on Blob"`
				}
			}
		} `graphql:"... on Tree"`
	} `graphql:"workflows: object(expression: \"HEAD:.github/workflows\")"`
}

// orgReposQuery is the paginated GraphQL query used by ListOrgRepos. It
// fetches up to pageSize non-archived, non-locked repositories per page.
type orgReposQuery struct {
	Organization struct {
		Repositories struct {
			PageInfo struct {
				HasNextPage bool
				EndCursor   githubv4.String
			}
			Nodes []OrgRepoNode
		} `graphql:"repositories(first: $pageSize, after: $cursor, isArchived: false, isLocked: false, orderBy: {field: NAME, direction: ASC})"`
	} `graphql:"organization(login: $org)"`
	RateLimit struct {
		Remaining int
		ResetAt   githubv4.DateTime
		Cost      int
	}
}

// ListOrgRepos paginates through every non-archived, non-locked repository
// owned by org and returns the GraphQL nodes verbatim. Caller is responsible
// for translating to the on-disk Repository struct.
func (c *Clients) ListOrgRepos(ctx context.Context, org string, since30, since90, since365 githubv4.GitTimestamp) ([]OrgRepoNode, error) {
	const pageSize = 25
	var (
		out    []OrgRepoNode
		cursor *githubv4.String
	)

	for {
		vars := map[string]any{
			"org":      githubv4.String(org),
			"cursor":   cursor,
			"pageSize": githubv4.Int(pageSize),
			"since30":  since30,
			"since90":  since90,
			"since365": since365,
		}

		page, err := WithRetry(ctx, func(ctx context.Context) (orgReposQuery, error) {
			var q orgReposQuery
			if err := c.GQL.Query(ctx, &q, vars); err != nil {
				return orgReposQuery{}, err
			}
			return q, nil
		})
		if err != nil {
			return nil, fmt.Errorf("graphql org repos for %q: %w", org, err)
		}

		out = append(out, page.Organization.Repositories.Nodes...)

		if !page.Organization.Repositories.PageInfo.HasNextPage {
			break
		}
		c := page.Organization.Repositories.PageInfo.EndCursor
		cursor = &c
	}

	return out, nil
}
