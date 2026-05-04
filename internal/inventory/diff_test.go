package inventory

import (
	"strings"
	"testing"
	"time"
)

func TestComputeDiff_AddedAndFieldChanges(t *testing.T) {
	t.Parallel()

	from := []Repository{
		{
			FullName:               "Org/alpha",
			LicenseSPDXID:          "",
			UsesComplianceWorkflow: false,
			IsArchived:             false,
		},
		{
			FullName:               "Org/beta",
			LicenseSPDXID:          "MIT",
			UsesComplianceWorkflow: true,
			IsArchived:             false,
		},
	}
	to := []Repository{
		{
			FullName:               "Org/alpha",
			LicenseSPDXID:          "Apache-2.0",   // license added
			UsesComplianceWorkflow: true,           // compliance added
			IsArchived:             false,
		},
		{
			FullName:               "Org/beta",
			LicenseSPDXID:          "MIT",
			UsesComplianceWorkflow: true,
			IsArchived:             true,           // archive flip
		},
		{
			FullName:               "Org/gamma",   // added repo
			LicenseSPDXID:          "MIT",
			UsesComplianceWorkflow: false,
		},
	}

	d := ComputeDiff(from, to,
		time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	)

	if got := d.Added; len(got) != 1 || got[0] != "Org/gamma" {
		t.Errorf("Added = %v, want [Org/gamma]", got)
	}
	if len(d.Removed) != 0 {
		t.Errorf("Removed = %v, want []", d.Removed)
	}
	if len(d.LicenseAdded) != 1 ||
		d.LicenseAdded[0].FullName != "Org/alpha" ||
		d.LicenseAdded[0].Note != "Apache-2.0" {
		t.Errorf("LicenseAdded = %v, want [{Org/alpha Apache-2.0}]", d.LicenseAdded)
	}
	if got := d.ComplianceAdded; len(got) != 1 || got[0] != "Org/alpha" {
		t.Errorf("ComplianceAdded = %v, want [Org/alpha]", got)
	}
	if got := d.Archived; len(got) != 1 || got[0] != "Org/beta" {
		t.Errorf("Archived = %v, want [Org/beta]", got)
	}
}

func TestComputeDiff_Empty(t *testing.T) {
	t.Parallel()

	repos := []Repository{
		{FullName: "Org/alpha", LicenseSPDXID: "MIT"},
	}
	d := ComputeDiff(repos, repos, time.Time{}, time.Time{})
	if !d.IsEmpty() {
		t.Errorf("expected IsEmpty, got %+v", d)
	}

	md := RenderDiffMarkdown(d)
	if !strings.Contains(md, "No changes between snapshots.") {
		t.Errorf("expected empty placeholder, got:\n%s", md)
	}
	if !strings.Contains(md, "## Diff: ? -> ?") {
		t.Errorf("expected ?->? header for zero dates, got:\n%s", md)
	}
}

func TestRenderDiffMarkdown_BulletsAndHeader(t *testing.T) {
	t.Parallel()

	from := []Repository{{FullName: "Org/alpha"}}
	to := []Repository{
		{FullName: "Org/alpha"},
		{FullName: "Org/gamma"},
	}
	d := ComputeDiff(from, to,
		time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	)
	md := RenderDiffMarkdown(d)

	want := []string{
		"## Diff: 2026-04-30 -> 2026-05-01",
		"### Repositories added",
		"- Org/gamma",
	}
	for _, w := range want {
		if !strings.Contains(md, w) {
			t.Errorf("expected %q in:\n%s", w, md)
		}
	}
}

// Per-check transitions are tracked separately so the diff text can name
// which check broke. Transitioning the license slot from success to failure
// while secrets/vuln stays green should bucket only into LicenseFlippedToFailure.
func TestComputeDiff_PerCheckFlips(t *testing.T) {
	t.Parallel()

	from := []Repository{{
		FullName: "Org/alpha",
		ComplianceChecks: &ComplianceChecks{
			SecretsVuln: ComplianceCheck{Status: "success"},
			License:     ComplianceCheck{Status: "success"},
		},
	}}
	to := []Repository{{
		FullName: "Org/alpha",
		ComplianceChecks: &ComplianceChecks{
			SecretsVuln: ComplianceCheck{Status: "success"},
			License:     ComplianceCheck{Status: "failure"},
		},
	}}

	d := ComputeDiff(from, to, time.Time{}, time.Time{})
	if got := d.LicenseFlippedToFailure; len(got) != 1 || got[0] != "Org/alpha" {
		t.Errorf("LicenseFlippedToFailure = %v, want [Org/alpha]", got)
	}
	if len(d.SecretsVulnFlippedToFailure) != 0 {
		t.Errorf("SecretsVulnFlippedToFailure = %v, want []", d.SecretsVulnFlippedToFailure)
	}
	if len(d.LicenseFlippedToSuccess) != 0 {
		t.Errorf("LicenseFlippedToSuccess = %v, want []", d.LicenseFlippedToSuccess)
	}

	// Now flip back to success on a follow-up snapshot.
	to2 := []Repository{{
		FullName: "Org/alpha",
		ComplianceChecks: &ComplianceChecks{
			SecretsVuln: ComplianceCheck{Status: "success"},
			License:     ComplianceCheck{Status: "success"},
		},
	}}
	d2 := ComputeDiff(to, to2, time.Time{}, time.Time{})
	if got := d2.LicenseFlippedToSuccess; len(got) != 1 || got[0] != "Org/alpha" {
		t.Errorf("LicenseFlippedToSuccess = %v, want [Org/alpha]", got)
	}
}

// When one side has no ComplianceChecks (pre-change snapshot or workflow
// just appeared/disappeared), per-check comparisons must be skipped — the
// workflow-adoption signal already covers the transition and we don't want
// noisy "flipped" entries straddling the schema-shape boundary.
func TestComputeDiff_NilComplianceChecksSkipsPerCheckFlips(t *testing.T) {
	t.Parallel()

	from := []Repository{{FullName: "Org/alpha"}} // ComplianceChecks nil
	to := []Repository{{
		FullName: "Org/alpha",
		ComplianceChecks: &ComplianceChecks{
			SecretsVuln: ComplianceCheck{Status: "failure"},
			License:     ComplianceCheck{Status: "failure"},
		},
	}}

	d := ComputeDiff(from, to, time.Time{}, time.Time{})
	if len(d.SecretsVulnFlippedToFailure)+len(d.LicenseFlippedToFailure) != 0 {
		t.Errorf("expected no per-check flips when one side is nil, got %+v", d)
	}
}
