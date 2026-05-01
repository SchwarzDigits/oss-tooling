package inventory

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"

	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
)

// complianceWorkflowRE matches a `uses:` directive that calls the central
// reusable workflow at SchwarzDigits/oss-compliance/.github/workflows/<file>.
// We match the full path, not just the org/repo, so that the oss-compliance
// repo itself — which references its own internal workflows via relative
// paths (`uses: ./.github/workflows/...`) — is not wrongly flagged as an
// adopter of itself.
var complianceWorkflowRE = regexp.MustCompile(
	`(?m)^\s*uses:\s*SchwarzDigits/oss-compliance/\.github/workflows/`)

// Manifests we recognize at the repository root. Case-sensitive — these are
// the canonical filenames.
var rootManifestFiles = map[string]struct{}{
	"go.mod":           {},
	"package.json":     {},
	"requirements.txt": {},
	"pyproject.toml":   {},
	"Cargo.toml":       {},
	"pom.xml":          {},
	"build.gradle":     {},
	"build.gradle.kts": {},
	"Gemfile":          {},
	"composer.json":    {},
	"Pipfile":          {},
}

// rootDocFile maps a normalized (case-folded, extension-stripped) basename to
// the corresponding bool we want to flip on Repository.
type rootDocKind int

const (
	rootDocReadme rootDocKind = iota + 1
	rootDocLicense
	rootDocSecurity
	rootDocContributing
	rootDocCodeOfConduct
)

func classifyRootEntry(name string) rootDocKind {
	upper := strings.ToUpper(name)
	// Strip a trailing extension if present (LICENSE.md, README.txt, ...).
	if idx := strings.LastIndex(upper, "."); idx >= 0 {
		upper = upper[:idx]
	}
	switch upper {
	case "README":
		return rootDocReadme
	case "LICENSE", "LICENCE", "COPYING":
		return rootDocLicense
	case "SECURITY":
		return rootDocSecurity
	case "CONTRIBUTING":
		return rootDocContributing
	case "CODE_OF_CONDUCT":
		return rootDocCodeOfConduct
	}
	return 0
}

// ListRepositories runs the consolidated GraphQL query for org and translates
// each node into a Repository. Repositories with IsDisabled==true are dropped.
func ListRepositories(ctx context.Context, c *gh.Clients, org string, now time.Time) ([]Repository, error) {
	since30 := githubv4.GitTimestamp{Time: now.Add(-30 * 24 * time.Hour)}
	since90 := githubv4.GitTimestamp{Time: now.Add(-90 * 24 * time.Hour)}
	since365 := githubv4.GitTimestamp{Time: now.Add(-365 * 24 * time.Hour)}

	nodes, err := c.ListOrgRepos(ctx, org, since30, since90, since365)
	if err != nil {
		return nil, err
	}

	out := make([]Repository, 0, len(nodes))
	for _, n := range nodes {
		if n.IsDisabled {
			continue
		}
		out = append(out, translateNode(n, org, now))
	}
	return out, nil
}

func translateNode(n gh.OrgRepoNode, org string, now time.Time) Repository {
	r := Repository{
		Org:           org,
		Name:          n.Name,
		FullName:      n.NameWithOwner,
		Description:   n.Description,
		Visibility:    strings.ToLower(n.Visibility),
		IsArchived:    n.IsArchived,
		IsDisabled:    n.IsDisabled,
		IsFork:        n.IsFork,
		URL:           n.URL,
		DefaultBranch: n.DefaultBranchRef.Name,
		// PrimaryLang is GitHub Linguist's classification (from the GraphQL
		// primaryLanguage field), not something this tool computes.
		PrimaryLang:   n.PrimaryLanguage.Name,
		CreatedAt:     n.CreatedAt.Time,
		UpdatedAt:     n.UpdatedAt.Time,
		PushedAt:      n.PushedAt.Time,
		Stars:         n.StargazerCount,
		Forks:         n.ForkCount,
		Watchers:      n.Watchers.TotalCount,
		OpenIssues:    n.Issues.TotalCount,
		OpenPRs:       n.PullRequests.TotalCount,
		LicenseSPDXID: n.LicenseInfo.SpdxID,
		LicenseName:   n.LicenseInfo.Name,
		CollectedAt:   now,
	}

	for _, t := range n.RepositoryTopics.Nodes {
		r.Topics = append(r.Topics, t.Topic.Name)
	}

	// Commit windows from defaultBranchRef.target.history (Commit fragment).
	cmt := n.DefaultBranchRef.Target.Commit
	r.CommitsLast30d = cmt.Commits30d.TotalCount
	r.CommitsLast90d = cmt.Commits90d.TotalCount
	r.CommitsLast365d = cmt.Commits365d.TotalCount

	// Distinct active contributors over last 90 days (capped at 100 commits).
	authors := make(map[string]struct{})
	for _, cn := range cmt.Authors90d.Nodes {
		login := cn.Author.User.Login
		if login != "" {
			authors[login] = struct{}{}
		}
	}
	r.ActiveContributors90d = len(authors)

	// Root tree: docs and manifests.
	hasLicenseFile := false
	for _, e := range n.RootTree.Tree.Entries {
		if _, ok := rootManifestFiles[e.Name]; ok && e.Type == "blob" {
			r.Manifests = append(r.Manifests, e.Name)
		}
		switch classifyRootEntry(e.Name) {
		case rootDocReadme:
			r.HasReadme = true
		case rootDocLicense:
			hasLicenseFile = true
		case rootDocSecurity:
			r.HasSecurity = true
		case rootDocContributing:
			r.HasContributing = true
		case rootDocCodeOfConduct:
			r.HasCodeOfConduct = true
		}
	}
	r.HasLicense = hasLicenseFile || n.LicenseInfo.SpdxID != ""

	// Workflows tree: file names + compliance-workflow detection.
	for _, e := range n.Workflows.Tree.Entries {
		r.WorkflowFiles = append(r.WorkflowFiles, e.Name)
		if !e.Object.Blob.IsBinary && e.Object.Blob.Text != "" {
			if complianceWorkflowRE.MatchString(e.Object.Blob.Text) {
				r.UsesComplianceWorkflow = true
			}
		}
	}
	r.HasGithubWorkflows = len(r.WorkflowFiles) > 0

	// Stable empty slices instead of nil for cleaner JSON.
	if r.Topics == nil {
		r.Topics = []string{}
	}
	if r.WorkflowFiles == nil {
		r.WorkflowFiles = []string{}
	}
	if r.Manifests == nil {
		r.Manifests = []string{}
	}

	return r
}
