package inventory

import (
	"context"

	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
)

// enrichRepo runs the per-repo REST work that GraphQL doesn't cover. Today
// that's just default-branch protection. Future enrichment goes here.
//
// Errors are stored as data on the Repository (BranchProtectionError).
// The Go error return is reserved for ctx cancellation.
func enrichRepo(ctx context.Context, c *gh.Clients, r *Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	res := gh.GetDefaultBranchProtection(ctx, c.REST, r.Org, r.Name, r.DefaultBranch)
	r.DefaultBranchProtected = res.Protected
	r.BranchProtectionError = res.ErrMessage
	return nil
}
