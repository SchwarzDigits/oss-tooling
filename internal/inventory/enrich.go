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
		run, err := gh.GetLatestComplianceRun(ctx, c.REST, r.Org, r.Name)
		if err != nil {
			logger.Warn("compliance run lookup failed", "repo", r.FullName, "err", err)
		} else {
			r.LastComplianceRunAt = run.StartedAt
			r.LastComplianceRunStatus = run.Status
			r.LastComplianceRunConclusion = run.Conclusion
			r.LastComplianceRunURL = run.URL
			r.LastComplianceRunFailedJobs = run.FailedJobs
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

	login, err := gh.GetTopCommitter90d(ctx, c.REST, r.Org, r.Name, r.DefaultBranch)
	if err != nil {
		logger.Warn("top committer lookup failed", "repo", r.FullName, "err", err)
	}
	if login != "" {
		r.LikelyOwner = login
		r.LikelyOwnerSource = "top_committer_90d"
	}
	return nil
}
