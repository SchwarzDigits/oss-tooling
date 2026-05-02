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

	// Old sections that the merge replaced must be gone.
	mustNotContain(t, got, "## Migration backlog")
	mustNotContain(t, got, "## Risk: top-starred")

	mustNotContain(t, got, "## Changes since previous snapshot")

	// Methodology hints (italic one-liners) live under three sections.
	mustContain(t, got, "_Sorted by last push descending. Recently active repos appear first")
	mustContain(t, got, "_Public repos without the central compliance workflow, sorted by stars descending.")
	mustContain(t, got, "_Repos with no commits in the last 12 months. Hints:")

	// Specific fixtures appear in their expected sections.
	mustContain(t, got, "lonely-repo")     // public, no license -> Action section
	mustContain(t, got, "ancient-stuff")   // stale -> Archive candidates
	mustContain(t, got, "platform-cli")    // active no-compliance -> Migration Priority
	mustContain(t, got, "oss-tooling")     // Compliance status row

	// Per-org totals row should reflect the fixture counts.
	mustContain(t, got, "| SchwarzDigits |")
	mustContain(t, got, "| SchwarzIT |")
	mustContain(t, got, "| **All** |")

	// Compliance run statuses are reflected in the table, and the status
	// cell links to the actual run URL when one is present.
	mustContain(t, got, "✅ success")
	mustContain(t, got, "❌ failure")
	mustContain(t, got, "license-and-sbom")
	mustContain(t, got, "[❌ failure](https://github.com/SchwarzDigits/compliance-but-no-security/actions/runs/55)")
	mustContain(t, got, "[✅ success](https://github.com/SchwarzDigits/oss-tooling/actions/runs/100)")

	// Owner labels render with their source suffix.
	mustContain(t, got, "(recent committer)")

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

// TestMigrationStatus checks the Migration Priority status column logic.
func TestMigrationStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.Add(-365 * 24 * time.Hour)

	cases := []struct {
		name string
		repo Repository
		want string
	}{
		{"fork wins over active", Repository{IsFork: true, PushedAt: now}, "fork"},
		{"fork wins over stale", Repository{IsFork: true, PushedAt: now.AddDate(-3, 0, 0)}, "fork"},
		{"active recent push", Repository{PushedAt: now.AddDate(0, -1, 0)}, "active"},
		{"stale ancient push", Repository{PushedAt: now.AddDate(-2, 0, 0)}, "stale"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := migrationStatus(tc.repo, cutoff)
			if got != tc.want {
				t.Errorf("migrationStatus = %q, want %q", got, tc.want)
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
