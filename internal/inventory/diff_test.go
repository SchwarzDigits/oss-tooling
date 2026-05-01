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

func TestComputeDiff_RunFlippedToFailureIncludesJobs(t *testing.T) {
	t.Parallel()

	from := []Repository{{
		FullName:                  "Org/alpha",
		LastComplianceRunStatus:   "completed",
		LastComplianceRunConclusion: "success",
	}}
	to := []Repository{{
		FullName:                    "Org/alpha",
		LastComplianceRunStatus:     "completed",
		LastComplianceRunConclusion: "failure",
		LastComplianceRunFailedJobs: []string{"license-and-sbom"},
	}}

	d := ComputeDiff(from, to, time.Time{}, time.Time{})
	if len(d.RunFlippedToFailure) != 1 {
		t.Fatalf("RunFlippedToFailure = %v, want 1 entry", d.RunFlippedToFailure)
	}
	got := d.RunFlippedToFailure[0]
	if got.FullName != "Org/alpha" || !strings.Contains(got.Note, "license-and-sbom") {
		t.Errorf("entry = %+v, want failed jobs noted", got)
	}
}
