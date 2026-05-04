package inventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateReport_FromFixtures(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport("testdata", out, ""); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(body)

	mustContain(t, got, "# OSS Inventory Report")
	mustContain(t, got, "**Status:**")
	mustContain(t, got, "## Per-organization totals")
	mustContain(t, got, "## Compliance status")
	mustContain(t, got, "## Action: Public repos missing a LICENSE")
	mustContain(t, got, "## Compliance: Migration Priority")
	mustContain(t, got, "## Archive candidates")
	mustContain(t, got, "## Primary languages")
	mustContain(t, got, "## Top 20 most-starred repositories")
	mustContain(t, got, "## Status definitions")

	// Old sections that the merge replaced must be gone.
	mustNotContain(t, got, "## Migration backlog")
	mustNotContain(t, got, "## Risk: top-starred")

	mustNotContain(t, got, "## Changes since previous snapshot")

	// Per-org table uses the unified Active/Stale/Archived columns.
	mustContain(t, got, "| Active | Stale | Archived | Total | With LICENSE | Compliance WF |")

	// Status header percentages now use "active repos" denominator and
	// the license bullet carries an "overall" fragment.
	mustContain(t, got, "active repos (")
	mustContain(t, got, "— overall:")

	// Methodology hints. Migration Priority and Archive use the shortened
	// versions that defer to the Status definitions footer.
	mustContain(t, got, "_Sorted by last push descending. Recently active repos appear first")
	mustContain(t, got, "Status is \"active\" or \"stale\"")
	mustContain(t, got, "_Stale repos and triage hints to support archive/keep decisions.")

	// Specific fixtures appear in their expected sections.
	mustContain(t, got, "lonely-repo")     // public, no license -> Action section
	mustContain(t, got, "ancient-stuff")   // stale -> Archive candidates
	mustContain(t, got, "platform-cli")    // active no-compliance -> Migration Priority
	mustContain(t, got, "oss-tooling")     // Compliance status row

	// Per-org totals row should reflect the fixture counts.
	mustContain(t, got, "| SchwarzDigits |")
	mustContain(t, got, "| SchwarzIT |")
	mustContain(t, got, "| **All** |")

	// Compliance status table is per-check: each row has a Secrets/Vuln
	// cell and a License (ORT) cell. Each non-no_run cell links to the
	// run in which that check executed.
	mustContain(t, got, "| Repository | Secrets/Vuln | License (ORT) |")
	mustContain(t, got, "| SchwarzDigits/compliance-but-no-security |")
	mustContain(t, got, "https://github.com/SchwarzDigits/oss-tooling/actions/runs/100")
	mustContain(t, got, "https://github.com/SchwarzDigits/compliance-but-no-security/actions/runs/55")

	// The compliance-but-no-security row is the natrium failure case:
	// secrets/vuln green and license red on the same row. Walking the
	// rendered row text confirms both signals make it through.
	cbnsRow := sliceBetween(got, "| SchwarzDigits/compliance-but-no-security |", "\n")
	if !strings.Contains(cbnsRow, "✅") || !strings.Contains(cbnsRow, "❌") {
		t.Errorf("compliance-but-no-security row should show green secrets/vuln and red license, got: %q", cbnsRow)
	}

	// no_run is rendered as "– never" — distinct from "❌ failure". The
	// SchwarzIT/newly-onboarded fixture exercises this case.
	mustContain(t, got, "– never")
	mustContain(t, got, "SchwarzIT/newly-onboarded")

	// New status-header bullets surface the per-check rollups.
	mustContain(t, got, "Latest secrets/vuln checks:")
	mustContain(t, got, "Latest license checks:")
	mustContain(t, got, "Compliance workflow adoption:")
	// Old "Failing compliance runs" bullet is gone.
	mustNotContain(t, got, "Failing compliance runs:")

	// Owner labels render with their (short) source suffix; both
	// CODEOWNERS-sourced and committer-sourced rows appear in the fixtures.
	mustContain(t, got, "(committer)")
	mustContain(t, got, "(CO)")
	// The old long suffixes must not leak through.
	mustNotContain(t, got, "(recent committer)")
	mustNotContain(t, got, "(CODEOWNERS)")

	// Section ordering after the merge: Compliance < Action < Migration
	// Priority < Archive candidates.
	complianceIdx := strings.Index(got, "## Compliance status")
	actionIdx := strings.Index(got, "## Action: Public repos missing a LICENSE")
	migrationIdx := strings.Index(got, "## Compliance: Migration Priority")
	archiveIdx := strings.Index(got, "## Archive candidates")
	if !(complianceIdx < actionIdx && actionIdx < migrationIdx && migrationIdx < archiveIdx) {
		t.Errorf("section ordering wrong: compliance=%d action=%d migration=%d archive=%d",
			complianceIdx, actionIdx, migrationIdx, archiveIdx)
	}

	// fork-of-something is stale + a fork — it should appear in archive
	// candidates with the fork hint, not in the missing-license section.
	licSection := sliceBetween(got, "## Action: Public repos missing a LICENSE", "## ")
	if strings.Contains(licSection, "fork-of-something") {
		t.Errorf("fork-of-something has a license but appears in missing-license section")
	}
	archiveSection := sliceBetween(got, "## Archive candidates", "## ")
	if !strings.Contains(archiveSection, "Fork — check if upstream still relevant") {
		t.Errorf("expected fork hint in archive candidates section, got:\n%s", archiveSection)
	}

	// Migration Priority should include active repos but not archived ones.
	// platform-cli is active and lacks compliance workflow → expect it here
	// with the active status label.
	migrationSection := sliceBetween(got, "## Compliance: Migration Priority", "## ")
	if !strings.Contains(migrationSection, "platform-cli") {
		t.Errorf("platform-cli missing from migration priority section:\n%s", migrationSection)
	}
	if !strings.Contains(migrationSection, "active") {
		t.Errorf("expected active status label in migration priority, got:\n%s", migrationSection)
	}
}

// TestArchiveHint exercises the four-level triage logic with explicit
// (now, pushed_at) values so the assertions don't drift with the wall clock.
func TestArchiveHint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		repo Repository
		want string
	}{
		{
			name: "fork beats every other rule",
			repo: Repository{IsFork: true, Stars: 100, PushedAt: now.AddDate(-3, 0, 0)},
			want: "Fork — check if upstream still relevant",
		},
		{
			name: "popular repo with stars > 5",
			repo: Repository{Stars: 50, PushedAt: now.AddDate(-3, 0, 0)},
			want: "Decision needed",
		},
		{
			name: "popular repo with forks > 5",
			repo: Repository{Forks: 12, PushedAt: now.AddDate(-3, 0, 0)},
			want: "Decision needed",
		},
		{
			name: "ancient and unpopular -> archive recommended",
			repo: Repository{Stars: 2, PushedAt: now.AddDate(-3, 0, 0)},
			want: "Archive recommended",
		},
		{
			name: "12-24 month range -> possibly still active",
			repo: Repository{Stars: 2, PushedAt: now.AddDate(0, -18, 0)},
			want: "Possibly still active — verify before archiving",
		},
		{
			name: "exactly at 24-month boundary rolls into archive",
			repo: Repository{Stars: 2, PushedAt: now.Add(-730 * 24 * time.Hour)},
			want: "Archive recommended",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := archiveHint(tc.repo, now)
			if got != tc.want {
				t.Errorf("archiveHint(%+v, %v) = %q, want %q", tc.repo, now, got, tc.want)
			}
		})
	}
}

// TestRenderCheckCell covers the four status branches plus the
// missing-URL and missing-CompletedAt edges so a rendering regression
// in any branch surfaces here rather than only in the integration test.
func TestRenderCheckCell(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	twoHoursAgo := now.Add(-2 * time.Hour)

	cases := []struct {
		name string
		c    ComplianceCheck
		want string
	}{
		{
			name: "success with URL renders as linked emoji + relative time",
			c:    ComplianceCheck{Status: "success", CompletedAt: &twoHoursAgo, URL: "https://example/run/1"},
			want: "[✅ 2 hours ago](https://example/run/1)",
		},
		{
			name: "failure with URL renders as linked red emoji + relative time",
			c:    ComplianceCheck{Status: "failure", CompletedAt: &twoHoursAgo, URL: "https://example/run/2"},
			want: "[❌ 2 hours ago](https://example/run/2)",
		},
		{
			name: "in_progress with URL is a linked hourglass",
			c:    ComplianceCheck{Status: "in_progress", URL: "https://example/run/3"},
			want: "[⏳ in progress](https://example/run/3)",
		},
		{
			name: "in_progress without URL is unlinked",
			c:    ComplianceCheck{Status: "in_progress"},
			want: "⏳ in progress",
		},
		{
			name: "no_run is the bare em-dash phrase, no link",
			c:    ComplianceCheck{Status: "no_run"},
			want: "– never",
		},
		{
			name: "success with nil CompletedAt falls back to em dash",
			c:    ComplianceCheck{Status: "success", URL: "https://example/run/4"},
			want: "[✅ —](https://example/run/4)",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := renderCheckCell(tc.c, now)
			if got != tc.want {
				t.Errorf("renderCheckCell = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRepoStatus exercises the unified active/stale/archived classifier.
// The "archived overrides everything" rule matters because GitHub allows
// archiving a repo that was pushed seconds before — the lifecycle status
// follows the archive flag, not the push timestamp.
func TestRepoStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.Add(-365 * 24 * time.Hour)

	cases := []struct {
		name string
		repo Repository
		want string
	}{
		{"archived beats recent push", Repository{IsArchived: true, PushedAt: now}, "archived"},
		{"archived beats ancient push", Repository{IsArchived: true, PushedAt: now.AddDate(-5, 0, 0)}, "archived"},
		{"active when pushed inside cutoff", Repository{PushedAt: now.AddDate(0, -1, 0)}, "active"},
		{"active at exact cutoff (just inside)", Repository{PushedAt: cutoff.Add(time.Second)}, "active"},
		{"stale when pushed before cutoff", Repository{PushedAt: now.AddDate(-2, 0, 0)}, "stale"},
		{"fork is metadata, not a status — active fork stays active", Repository{IsFork: true, PushedAt: now}, "active"},
		{"fork is metadata, not a status — stale fork stays stale", Repository{IsFork: true, PushedAt: now.AddDate(-3, 0, 0)}, "stale"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := repoStatus(tc.repo, cutoff)
			if got != tc.want {
				t.Errorf("repoStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGenerateReport_WithDiffFrom verifies that supplying --diff-from injects
// the "Changes since previous snapshot" section and the status-bullet deltas.
func TestGenerateReport_WithDiffFrom(t *testing.T) {
	t.Parallel()

	prevDir := t.TempDir()
	prev := OrgInventory{
		Org:           "SchwarzDigits",
		CollectedAt:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		SchemaVersion: 1,
		Repositories: []Repository{{
			Org: "SchwarzDigits", Name: "oss-tooling",
			FullName: "SchwarzDigits/oss-tooling",
			Visibility: "public",
			URL: "https://github.com/SchwarzDigits/oss-tooling",
			HasLicense: true, LicenseSPDXID: "Apache-2.0",
			UsesComplianceWorkflow: false,
			PushedAt:    time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC),
			CollectedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
	body, _ := json.MarshalIndent(prev, "", "  ")
	if err := os.WriteFile(filepath.Join(prevDir, "SchwarzDigits.json"), body, 0o644); err != nil {
		t.Fatalf("seed prev: %v", err)
	}

	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport("testdata", out, prevDir); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	s := string(got)
	mustContain(t, s, "## Changes since previous snapshot")
	mustContain(t, s, "vs. previous:")
	mustContain(t, s, "New repos since previous snapshot:")
}

func TestGenerateReport_EmptyInput(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport(in, out, ""); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(body)
	mustContain(t, got, "# OSS Inventory Report")
	mustContain(t, got, "_None._")
}

func TestGenerateReport_SkipsInvalidJSON(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "bogus.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed bogus: %v", err)
	}
	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport(in, out, ""); err != nil {
		t.Fatalf("GenerateReport should not fail on invalid JSON: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func sliceBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i+len(start):], end)
	if j < 0 {
		return ""
	}
	return s[i+len(start) : i+len(start)+j]
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q", needle)
	}
}

func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q", needle)
	}
}
