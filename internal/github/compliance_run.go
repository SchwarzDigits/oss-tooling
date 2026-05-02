package github

import (
	"context"
	"errors"
	"net/http"
	"time"

	gogithub "github.com/google/go-github/v66/github"
)

// ComplianceRun captures the latest run of a repo's compliance.yml workflow.
// All fields are zero-valued when the workflow file is not present.
type ComplianceRun struct {
	StartedAt  time.Time
	Status     string
	Conclusion string
	URL        string
	FailedJobs []string
}

const complianceWorkflowPath = ".github/workflows/compliance.yml"

// GetLatestComplianceRun returns the most recent run of compliance.yml in the
// given repo. If no workflow with that path exists, it returns a zero
// ComplianceRun and a nil error — that's a valid "repo doesn't run it" state.
// On run failure, the names of failed jobs are populated.
func GetLatestComplianceRun(ctx context.Context, c *gogithub.Client, owner, repo string) (ComplianceRun, error) {
	workflowID, err := findComplianceWorkflowID(ctx, c, owner, repo)
	if err != nil {
		return ComplianceRun{}, err
	}
	if workflowID == 0 {
		return ComplianceRun{}, nil
	}

	runs, err := WithRetry(ctx, func(ctx context.Context) (*gogithub.WorkflowRuns, error) {
		r, _, err := c.Actions.ListWorkflowRunsByID(ctx, owner, repo, workflowID,
			&gogithub.ListWorkflowRunsOptions{ListOptions: gogithub.ListOptions{PerPage: 1}})
		return r, err
	})
	if err != nil {
		return ComplianceRun{}, err
	}
	if runs == nil || len(runs.WorkflowRuns) == 0 {
		return ComplianceRun{}, nil
	}

	run := runs.WorkflowRuns[0]
	out := ComplianceRun{
		StartedAt:  run.GetRunStartedAt().Time,
		Status:     run.GetStatus(),
		Conclusion: run.GetConclusion(),
		URL:        run.GetHTMLURL(),
	}

	if out.Conclusion == "failure" && run.ID != nil {
		jobs, err := WithRetry(ctx, func(ctx context.Context) (*gogithub.Jobs, error) {
			j, _, err := c.Actions.ListWorkflowJobs(ctx, owner, repo, *run.ID,
				&gogithub.ListWorkflowJobsOptions{ListOptions: gogithub.ListOptions{PerPage: 100}})
			return j, err
		})
		if err == nil && jobs != nil {
			for _, j := range jobs.Jobs {
				if j.GetConclusion() == "failure" {
					out.FailedJobs = append(out.FailedJobs, j.GetName())
				}
			}
		}
	}
	return out, nil
}

// findComplianceWorkflowID returns the numeric workflow ID for the file at
// .github/workflows/compliance.yml, or 0 if no such file is registered.
func findComplianceWorkflowID(ctx context.Context, c *gogithub.Client, owner, repo string) (int64, error) {
	opt := &gogithub.ListOptions{PerPage: 100}
	for {
		var nextPage int
		page, err := WithRetry(ctx, func(ctx context.Context) (*gogithub.Workflows, error) {
			p, resp, err := c.Actions.ListWorkflows(ctx, owner, repo, opt)
			if resp != nil {
				nextPage = resp.NextPage
			}
			return p, err
		})
		if err != nil {
			var ghErr *gogithub.ErrorResponse
			if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
				return 0, nil
			}
			return 0, err
		}
		if page == nil {
			return 0, nil
		}
		for _, wf := range page.Workflows {
			if wf.GetPath() == complianceWorkflowPath && wf.ID != nil {
				return *wf.ID, nil
			}
		}
		if nextPage == 0 {
			return 0, nil
		}
		opt.Page = nextPage
	}
}
