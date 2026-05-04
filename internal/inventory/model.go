package inventory

import (
	"time"

	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
)

// SchemaVersion is the on-disk format version. Increment when the JSON shape
// changes in a way readers must branch on.
const SchemaVersion = 1

// Repository captures the per-repo compliance metadata written to disk.
// Field order matches the spec for readability; JSON tags use snake_case.
type Repository struct {
	Org         string `json:"org"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`

	IsArchived    bool     `json:"is_archived"`
	IsDisabled    bool     `json:"is_disabled"`
	IsFork        bool     `json:"is_fork"`
	DefaultBranch string   `json:"default_branch"`
	Topics        []string `json:"topics"`
	PrimaryLang   string   `json:"primary_language"`

	URL string `json:"url"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	PushedAt  time.Time `json:"pushed_at"`

	Stars      int `json:"stars"`
	Forks      int `json:"forks"`
	Watchers   int `json:"watchers"`
	OpenIssues int `json:"open_issues"`
	OpenPRs    int `json:"open_prs"`

	LicenseSPDXID string `json:"license_spdx_id"`
	LicenseName   string `json:"license_name"`

	HasReadme        bool `json:"has_readme"`
	HasLicense       bool `json:"has_license"`
	HasSecurity      bool `json:"has_security"`
	HasContributing  bool `json:"has_contributing"`
	HasCodeOfConduct bool `json:"has_code_of_conduct"`

	HasGithubWorkflows     bool     `json:"has_github_workflows"`
	WorkflowFiles          []string `json:"workflow_files"`
	UsesComplianceWorkflow bool     `json:"uses_compliance_workflow"`
	// ComplianceWorkflowFile is the consumer-side workflow file (relative
	// to the repo root, e.g. ".github/workflows/oss-compliance.yml") whose
	// jobs we read for ComplianceChecks. Set during collection from the
	// first workflow file whose body matches complianceWorkflowRE; empty
	// when UsesComplianceWorkflow is false. Required because the caller's
	// filename is not necessarily "compliance.yml" — repos can name their
	// caller workflow anything.
	ComplianceWorkflowFile string `json:"compliance_workflow_file,omitempty"`

	DefaultBranchProtected bool   `json:"default_branch_protected"`
	BranchProtectionError  string `json:"branch_protection_error,omitempty"`

	CommitsLast30d  int `json:"commits_last_30d"`
	CommitsLast90d  int `json:"commits_last_90d"`
	CommitsLast365d int `json:"commits_last_365d"`

	ActiveContributors90d int `json:"active_contributors_90d"`

	Manifests []string `json:"manifests"`

	CollectedAt time.Time `json:"collected_at"`

	ComplianceChecks *ComplianceChecks `json:"compliance_checks,omitempty"`

	LikelyOwner       string `json:"likely_owner,omitempty"`
	LikelyOwnerSource string `json:"likely_owner_source,omitempty"`
}

// ComplianceChecks splits the central Compliance workflow's two real checks
// (secret-and-vuln-scan, license-and-sbom) so each can be tracked independently.
// The decide-ort routing job is intentionally not represented — its conclusion
// has no compliance meaning. Nil at the Repository level distinguishes "repo
// doesn't have the workflow" from "workflow exists but the check hasn't yet
// run" (which uses Status: "no_run" per check).
type ComplianceChecks struct {
	SecretsVuln ComplianceCheck `json:"secrets_vuln"`
	License     ComplianceCheck `json:"license"`
}

// ComplianceCheck is one check's most recent meaningful execution. "Meaningful"
// means the job actually ran (conclusion != "skipped"). Status "no_run"
// indicates that within the lookback window no such run was found — distinct
// from "failure".
type ComplianceCheck struct {
	Status      string     `json:"status"` // success | failure | in_progress | no_run
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	URL         string     `json:"url,omitempty"`
}

// toModelCheck converts a fetcher-side ComplianceCheck (in the github package)
// to the inventory-model representation. The two types are intentionally
// separate so the github package doesn't depend on inventory; this thin
// adapter is the only conversion site.
func toModelCheck(c gh.ComplianceCheck) ComplianceCheck {
	return ComplianceCheck{
		Status:      c.Status,
		CompletedAt: c.CompletedAt,
		URL:         c.URL,
	}
}

// OrgInventory is the on-disk wrapper for a single org's collected data.
type OrgInventory struct {
	Org           string       `json:"org"`
	CollectedAt   time.Time    `json:"collected_at"`
	SchemaVersion int          `json:"schema_version"`
	Repositories  []Repository `json:"repositories"`
}

// Summary is printed to stdout at the end of an inventory run.
type Summary struct {
	Orgs                   int
	TotalRepos             int
	WithLicense            int
	WithoutLicense         int
	MissingReadme          int
	MissingSecurity        int
	UsesComplianceWorkflow int
	StaleNoCommits365d     int
}
