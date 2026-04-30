package inventory

import "time"

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

	DefaultBranchProtected bool   `json:"default_branch_protected"`
	BranchProtectionError  string `json:"branch_protection_error,omitempty"`

	CommitsLast30d  int `json:"commits_last_30d"`
	CommitsLast90d  int `json:"commits_last_90d"`
	CommitsLast365d int `json:"commits_last_365d"`

	ActiveContributors90d int `json:"active_contributors_90d"`

	Manifests []string `json:"manifests"`

	CollectedAt time.Time `json:"collected_at"`
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
