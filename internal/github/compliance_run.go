package github

import (
	"context"
	"errors"
	"net/http"
	"time"

	gogithub "github.com/google/go-github/v66/github"
)

// ComplianceCheck is one check's most recent meaningful execution. Status is
// one of: "success", "failure", "in_progress", or "no_run". "Meaningful" means
// the job actually ran (conclusion != "skipped"). CompletedAt is nil for
// in_progress and no_run; URL points to the workflow run (not the job page)
// and is empty for no_run.
type ComplianceCheck struct {
	Status      string
	CompletedAt *time.Time
	URL         string
}

// ComplianceCheckStatuses is the per-check view of a repo's central
// Compliance workflow. The decide-ort routing job is intentionally not
// represented — its conclusion has no compliance meaning.
type ComplianceCheckStatuses struct {
	SecretsVuln ComplianceCheck
	License     ComplianceCheck
}

const (
	complianceWorkflowPath = ".github/workflows/compliance.yml"
	jobNameSecretsVuln     = "secret-and-vuln-scan"
	jobNameLicense         = "license-and-sbom"

	// runScanLimit is how many of the most recent runs we walk in Phase 1.
	// Bounds the worst-case API spend per repo when many runs in a row
	// skipped license-and-sbom (doc-only pushes etc.).
	runScanLimit = 30
)

// GetComplianceCheckStatuses returns the latest meaningful run of each
// real check in the central Compliance workflow.
//
// If the workflow file is not present in the repo, returns (nil, nil) —
// the absence of the workflow is a valid state distinct from a check-level
// "no_run". Per-check status "no_run" means no meaningful execution was
// found in the lookback window (defined below).
//
// Lookback is two-phase to balance API cost against accuracy on hot repos
// that flood the recent run list with doc-only pushes (all of which skip
// ORT):
//
//  1. Walk the latest runScanLimit runs of the workflow unfiltered. Fill
//     each slot with the first non-skipped job match. Stop early once both
//     slots are filled.
//  2. If the license slot is still empty, look up the most recent
//     event=schedule run of the workflow (the weekly cron forces a full
//     ORT execution) and try to fill the license slot from there. Phase 2
//     is not invoked for SecretsVuln; that job runs on every trigger.
//
// Errors are returned only for genuine API failures. The workflow not being
// registered in the repo returns (nil, nil); empty run lists fall through
// to "no_run" cleanly.
func GetComplianceCheckStatuses(ctx context.Context, c *gogithub.Client, owner, repo string) (*ComplianceCheckStatuses, error) {
	workflowID, err := findComplianceWorkflowID(ctx, c, owner, repo)
	if err != nil {
		return nil, err
	}
	if workflowID == 0 {
		return nil, nil
	}

	out := &ComplianceCheckStatuses{
		SecretsVuln: ComplianceCheck{Status: "no_run"},
		License:     ComplianceCheck{Status: "no_run"},
	}

	runs, err := listWorkflowRuns(ctx, c, owner, repo, workflowID, "", runScanLimit)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if out.SecretsVuln.Status != "no_run" && out.License.Status != "no_run" {
			break
		}
		if run.ID == nil {
			continue
		}
		jobs, err := listWorkflowJobs(ctx, c, owner, repo, *run.ID)
		if err != nil {
			return nil, err
		}
		fillSlotsFromJobs(out, jobs, run.GetHTMLURL())
	}

	if out.License.Status == "no_run" {
		schedRuns, err := listWorkflowRuns(ctx, c, owner, repo, workflowID, "schedule", 1)
		if err != nil {
			return nil, err
		}
		if len(schedRuns) > 0 && schedRuns[0].ID != nil {
			jobs, err := listWorkflowJobs(ctx, c, owner, repo, *schedRuns[0].ID)
			if err != nil {
				return nil, err
			}
			// Only the license slot needs the fallback; SecretsVuln has
			// already had its full run-window scan.
			licenseOnly := &ComplianceCheckStatuses{
				SecretsVuln: ComplianceCheck{Status: "no_run"},
				License:     ComplianceCheck{Status: "no_run"},
			}
			fillSlotsFromJobs(licenseOnly, jobs, schedRuns[0].GetHTMLURL())
			if licenseOnly.License.Status != "no_run" {
				out.License = licenseOnly.License
			}
		}
	}

	return out, nil
}

// fillSlotsFromJobs fills any empty (no_run) slot in s from a matching job
// in jobs. Jobs whose conclusion is "skipped" are ignored — they don't
// represent a meaningful execution. runURL is the HTML URL of the parent
// workflow run; we point at the run rather than the job for consistency
// with prior iterations and because the run page is the better landing
// place for triage.
func fillSlotsFromJobs(s *ComplianceCheckStatuses, jobs *gogithub.Jobs, runURL string) {
	if jobs == nil {
		return
	}
	for _, j := range jobs.Jobs {
		if j == nil {
			continue
		}
		var slot *ComplianceCheck
		switch j.GetName() {
		case jobNameSecretsVuln:
			slot = &s.SecretsVuln
		case jobNameLicense:
			slot = &s.License
		default:
			continue
		}
		if slot.Status != "no_run" {
			continue
		}
		check, ok := jobToCheck(j, runURL)
		if !ok {
			continue
		}
		*slot = check
	}
}

// jobToCheck maps a workflow job to a ComplianceCheck. Returns ok=false when
// the job is "skipped" — callers should keep looking. Live (in_progress /
// queued) jobs are reported with a nil CompletedAt; the URL still points
// to the in-flight run so a reader can drill in.
func jobToCheck(j *gogithub.WorkflowJob, runURL string) (ComplianceCheck, bool) {
	conclusion := j.GetConclusion()
	if conclusion == "skipped" {
		return ComplianceCheck{}, false
	}

	check := ComplianceCheck{URL: runURL}
	switch conclusion {
	case "success":
		check.Status = "success"
	case "failure", "timed_out", "cancelled", "action_required":
		check.Status = "failure"
	default:
		// No conclusion yet → live state.
		switch j.GetStatus() {
		case "in_progress", "queued":
			check.Status = "in_progress"
			return check, true
		default:
			// Unknown/empty status with no conclusion: treat as not-meaningful.
			return ComplianceCheck{}, false
		}
	}
	if t := j.GetCompletedAt(); !t.IsZero() {
		ts := t.Time
		check.CompletedAt = &ts
	}
	return check, true
}

// listWorkflowRuns returns up to perPage runs of the given workflow, optionally
// filtered to a single GitHub event name (e.g. "schedule"). Empty event means
// no filter.
func listWorkflowRuns(ctx context.Context, c *gogithub.Client, owner, repo string, workflowID int64, event string, perPage int) ([]*gogithub.WorkflowRun, error) {
	runs, err := WithRetry(ctx, func(ctx context.Context) (*gogithub.WorkflowRuns, error) {
		opts := &gogithub.ListWorkflowRunsOptions{
			ListOptions: gogithub.ListOptions{PerPage: perPage},
		}
		if event != "" {
			opts.Event = event
		}
		r, _, err := c.Actions.ListWorkflowRunsByID(ctx, owner, repo, workflowID, opts)
		return r, err
	})
	if err != nil {
		return nil, err
	}
	if runs == nil {
		return nil, nil
	}
	return runs.WorkflowRuns, nil
}

// listWorkflowJobs returns the jobs for a single workflow run. The Compliance
// workflow has at most three jobs, so a single page suffices.
func listWorkflowJobs(ctx context.Context, c *gogithub.Client, owner, repo string, runID int64) (*gogithub.Jobs, error) {
	return WithRetry(ctx, func(ctx context.Context) (*gogithub.Jobs, error) {
		j, _, err := c.Actions.ListWorkflowJobs(ctx, owner, repo, runID,
			&gogithub.ListWorkflowJobsOptions{ListOptions: gogithub.ListOptions{PerPage: 100}})
		return j, err
	})
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
