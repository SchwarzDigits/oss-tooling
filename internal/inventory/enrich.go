package inventory

import (
	"context"
	"log/slog"

	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
)

// enrichRepo runs the per-repo REST work that GraphQL doesn't cover.
//
// Each helper is fail-soft: errors are logged at WARN and the corresponding
// fields are left zero-valued. The Go error return is reserved for ctx
// cancellation, which the surrounding worker pool propagates upward.
func enrichRepo(ctx context.Context, c *gh.Clients, r *Repository) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	res := gh.GetDefaultBranchProtection(ctx, c.REST, r.Org, r.Name, r.DefaultBranch)
	r.DefaultBranchProtected = res.Protected
	r.BranchProtectionError = res.ErrMessage

	logger := slog.Default()

	if r.UsesComplianceWorkflow {
		checks, err := gh.GetComplianceCheckStatuses(ctx, c.REST, r.Org, r.Name, r.ComplianceWorkflowFile)
		if err != nil {
			logger.Warn("compliance check status lookup failed", "repo", r.FullName, "err", err)
		} else if checks != nil {
			r.ComplianceChecks = &ComplianceChecks{
				SecretsVuln: toModelCheck(checks.SecretsVuln),
				License:     toModelCheck(checks.License),
			}
		}
	}

	owners, err := gh.GetCodeOwners(ctx, c.REST, r.Org, r.Name, r.DefaultBranch)
	if err != nil {
		logger.Warn("codeowners lookup failed", "repo", r.FullName, "err", err)
	}
	if len(owners) > 0 {
		r.LikelyOwner = owners[0]
		r.LikelyOwnerSource = "codeowners"
		return nil
	}

	login, err := gh.GetTopCommitter(ctx, c.REST, r.Org, r.Name, r.DefaultBranch)
	if err != nil {
		logger.Warn("top committer lookup failed", "repo", r.FullName, "err", err)
	}
	if login != "" {
		r.LikelyOwner = login
		r.LikelyOwnerSource = "top_committer_recent"
	}
	return nil
}
