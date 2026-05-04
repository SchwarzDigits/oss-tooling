package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
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

// runScanLimit is how many of the most recent runs we walk in Phase 1.
// Bounds the worst-case API spend per repo when many runs in a row
// skipped license-and-sbom (doc-only pushes etc.).
const runScanLimit = 30

// Recognized names for each real check in the central Compliance workflow.
//
// GitHub returns reusable-workflow job names in the format
// "<caller-job-id> / <job-name>" — for example
// "compliance / Secret and vulnerability scan". The caller-job-id is up to
// the consumer; we anchor on the suffix instead. Both the display name (set
// via `name:` in the reusable workflow yaml) and the YAML job key are
// accepted, so renaming either in the central workflow doesn't immediately
// break this tool. Add new variants here if the central workflow ever
// renames a job.
var (
	secretsVulnJobNames = []string{
		"Secret and vulnerability scan", // current display name
		"secret-and-vuln-scan",          // YAML job key fallback
	}
	licenseJobNames = []string{
		"License analysis and SBOM", // current display name
		"license-and-sbom",          // YAML job key fallback
	}
)

// GetComplianceCheckStatuses returns the latest meaningful run of each
// real check in the central Compliance workflow.
//
// workflowFile is the consumer's caller workflow file path relative to the
// repo root (e.g. ".github/workflows/oss-compliance.yml"). The caller's
// filename is up to the consumer — repos that adopt the central reusable
// workflow can name their wrapper anything — so we can't hardcode it.
// An empty string returns (nil, nil), as does the file not being a
// registered workflow in the repo.
//
// Per-check status "no_run" means no meaningful execution was found in the
// lookback window (defined below).
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
// Errors are returned only for genuine API failures.
func GetComplianceCheckStatuses(ctx context.Context, c *gogithub.Client, owner, repo, workflowFile string) (*ComplianceCheckStatuses, error) {
	if workflowFile == "" {
		return nil, nil
	}
	workflowID, err := findWorkflowIDByPath(ctx, c, owner, repo, workflowFile)
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
		name := j.GetName()
		var slot *ComplianceCheck
		switch {
		case matchesJobName(name, secretsVulnJobNames):
			slot = &s.SecretsVuln
		case matchesJobName(name, licenseJobNames):
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

// matchesJobName reports whether name corresponds to one of the known
// candidates, accepting both the bare candidate and the
// "<caller-job-id> / <candidate>" form GitHub uses for reusable-workflow
// jobs.
func matchesJobName(name string, candidates []string) bool {
	for _, c := range candidates {
		if name == c || strings.HasSuffix(name, " / "+c) {
			return true
		}
	}
	return false
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

// findWorkflowIDByPath returns the numeric workflow ID for the workflow file
// at the given repo-relative path (e.g. ".github/workflows/foo.yml"), or 0
// if no such file is a registered workflow in the repo.
func findWorkflowIDByPath(ctx context.Context, c *gogithub.Client, owner, repo, path string) (int64, error) {
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
			if wf.GetPath() == path && wf.ID != nil {
				return *wf.ID, nil
			}
		}
		if nextPage == 0 {
			return 0, nil
		}
		opt.Page = nextPage
	}
}
