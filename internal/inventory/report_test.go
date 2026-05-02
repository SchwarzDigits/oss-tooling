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
	mustContain(t, got, "## Migration backlog: repos without the central compliance workflow")
	mustContain(t, got, "## Archive candidates")
	mustContain(t, got, "## Risk: top-starred repos without the central compliance workflow")
	mustContain(t, got, "## Primary languages")
	mustContain(t, got, "## Top 20 most-starred repositories")

	mustNotContain(t, got, "## Changes since previous snapshot")

	// Specific fixtures appear in their expected sections.
	mustContain(t, got, "lonely-repo")     // public, no license -> Action section
	mustContain(t, got, "ancient-stuff")   // stale -> Archive candidates
	mustContain(t, got, "platform-cli")    // top-risk no compliance + most-starred
	mustContain(t, got, "oss-tooling")     // Compliance status row

	// Per-org totals row should reflect the fixture counts.
	mustContain(t, got, "| SchwarzDigits |")
	mustContain(t, got, "| SchwarzIT |")
	mustContain(t, got, "| **All** |")

	// Compliance run statuses are reflected in the table.
	mustContain(t, got, "✅ success")
	mustContain(t, got, "❌ failure")
	mustContain(t, got, "license-and-sbom")

	// Owner labels render with their source suffix.
	mustContain(t, got, "(recent committer)")

	// Section ordering: Compliance status before Action section before
	// Migration backlog before Archive candidates.
	complianceIdx := strings.Index(got, "## Compliance status")
	actionIdx := strings.Index(got, "## Action: Public repos missing a LICENSE")
	backlogIdx := strings.Index(got, "## Migration backlog")
	archiveIdx := strings.Index(got, "## Archive candidates")
	riskIdx := strings.Index(got, "## Risk: top-starred")
	if !(complianceIdx < actionIdx && actionIdx < backlogIdx && backlogIdx < archiveIdx && archiveIdx < riskIdx) {
		t.Errorf("section ordering wrong: compliance=%d action=%d backlog=%d archive=%d risk=%d",
			complianceIdx, actionIdx, backlogIdx, archiveIdx, riskIdx)
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

	// Migration backlog must NOT include archived or stale repos.
	backlogSection := sliceBetween(got, "## Migration backlog", "## ")
	if strings.Contains(backlogSection, "ancient-stuff") {
		t.Errorf("stale repo ancient-stuff appears in migration backlog")
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
