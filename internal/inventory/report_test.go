package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateReport_FromFixtures(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport("testdata", out); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(body)

	mustContain(t, got, "# OSS Inventory Report")
	mustContain(t, got, "## Per-organization totals")
	mustContain(t, got, "## Repositories missing a LICENSE")
	mustContain(t, got, "## Repositories missing a local SECURITY.md")
	mustContain(t, got, "## Stale repositories (no commits in last 365 days)")
	mustContain(t, got, "## Repositories using the central compliance workflow")
	mustContain(t, got, "## Primary languages")
	mustContain(t, got, "## Top 20 most-starred repositories")

	// Specific fixtures appear in their expected sections.
	mustContain(t, got, "lonely-repo")   // missing license
	mustContain(t, got, "ancient-stuff") // stale (2022)
	mustContain(t, got, "compliance-but-no-security")
	mustContain(t, got, "platform-cli") // most-starred (88)
	mustContain(t, got, "oss-tooling")  // uses compliance workflow

	// Per-org totals row should reflect the fixture counts.
	mustContain(t, got, "| SchwarzDigits |")
	mustContain(t, got, "| SchwarzIT |")
	mustContain(t, got, "| **All** |")

	// fork-of-something has a license, so its name must not appear inside
	// the missing-license table.
	licSection := sliceBetween(got, "## Repositories missing a LICENSE", "## ")
	if strings.Contains(licSection, "fork-of-something") {
		t.Errorf("fork-of-something has a license but appears in missing-license section")
	}
}

// sliceBetween returns the substring of s between (exclusive) the first
// occurrence of start and the first occurrence of end after that. If either
// marker is missing, returns "".
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

func TestGenerateReport_EmptyInput(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport(in, out); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(body)
	mustContain(t, got, "# OSS Inventory Report")
	// All sections should render with their "_None._" placeholders.
	mustContain(t, got, "_None._")
}

func TestGenerateReport_SkipsInvalidJSON(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "bogus.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed bogus: %v", err)
	}
	out := filepath.Join(t.TempDir(), "summary.md")
	if err := GenerateReport(in, out); err != nil {
		t.Fatalf("GenerateReport should not fail on invalid JSON: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q", needle)
	}
}
