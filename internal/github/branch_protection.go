package github

import (
	"context"
	"errors"
	"net/http"

	gogithub "github.com/google/go-github/v66/github"
)

// BranchProtectionResult is the fail-soft return from GetDefaultBranchProtection.
// ErrMessage is empty on success or on the "no rule defined" 404 case.
type BranchProtectionResult struct {
	Protected  bool
	ErrMessage string
}

// GetDefaultBranchProtection asks GitHub whether the default branch has a
// protection rule. It is fail-soft: per-repo errors are returned as data,
// never as a Go error. The only failure modes that propagate are context
// cancellation (which the caller handles) and Go-level programming bugs.
func GetDefaultBranchProtection(ctx context.Context, c *gogithub.Client, owner, repo, branch string) BranchProtectionResult {
	if branch == "" {
		return BranchProtectionResult{Protected: false, ErrMessage: "no default branch"}
	}

	res, err := WithRetry(ctx, func(ctx context.Context) (*gogithub.Protection, error) {
		p, _, err := c.Repositories.GetBranchProtection(ctx, owner, repo, branch)
		return p, err
	})
	if err == nil && res != nil {
		return BranchProtectionResult{Protected: true}
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Caller will see ctx.Err() through the surrounding pipeline.
		return BranchProtectionResult{Protected: false, ErrMessage: "context canceled"}
	}

	// go-github translates the 404 "Branch not protected" body into a
	// sentinel error rather than an *ErrorResponse. That's the same
	// "no rule defined" state we treat as not-an-error.
	if errors.Is(err, gogithub.ErrBranchNotProtected) {
		return BranchProtectionResult{Protected: false}
	}

	var ghErr *gogithub.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil {
		switch ghErr.Response.StatusCode {
		case http.StatusNotFound:
			// No protection rule defined — that's a valid state, not an error.
			return BranchProtectionResult{Protected: false}
		case http.StatusForbidden:
			return BranchProtectionResult{Protected: false, ErrMessage: "forbidden: token lacks admin:repo scope"}
		}
	}

	if err != nil {
		return BranchProtectionResult{Protected: false, ErrMessage: err.Error()}
	}
	return BranchProtectionResult{Protected: false, ErrMessage: "unknown"}
}
