package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DiffEntry annotates a per-repo change with a human-readable note.
// FullName is the org/name of the affected repo. Note is a short string
// rendered in parentheses next to the name (e.g. an SPDX id, the names of
// failed jobs, a from->to value).
type DiffEntry struct {
	FullName string
	Note     string
}

// Diff is the result of comparing two snapshots. Slice fields are sorted
// alphabetically for stable output. A Diff with all empty slices means the
// snapshots are equivalent for our purposes.
type Diff struct {
	FromDate time.Time
	ToDate   time.Time

	Added   []string
	Removed []string

	LicenseAdded   []DiffEntry
	LicenseRemoved []DiffEntry
	LicenseChanged []DiffEntry

	ComplianceAdded   []string
	ComplianceRemoved []string

	Archived   []string
	Unarchived []string

	SecretsVulnFlippedToFailure []string
	SecretsVulnFlippedToSuccess []string
	LicenseFlippedToFailure     []string
	LicenseFlippedToSuccess     []string

	BecameStale  []string
	BecameActive []string
}

// IsEmpty reports whether the diff contains no changes at all.
func (d Diff) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 &&
		len(d.LicenseAdded) == 0 && len(d.LicenseRemoved) == 0 && len(d.LicenseChanged) == 0 &&
		len(d.ComplianceAdded) == 0 && len(d.ComplianceRemoved) == 0 &&
		len(d.Archived) == 0 && len(d.Unarchived) == 0 &&
		len(d.SecretsVulnFlippedToFailure) == 0 && len(d.SecretsVulnFlippedToSuccess) == 0 &&
		len(d.LicenseFlippedToFailure) == 0 && len(d.LicenseFlippedToSuccess) == 0 &&
		len(d.BecameStale) == 0 && len(d.BecameActive) == 0
}

// snapshot bundles the repos plus the date the snapshot was collected. The
// date helps the diff header distinguish snapshots that lived under
// directories like "2026-04-30" from those under "latest".
type snapshot struct {
	Date  time.Time
	Repos []Repository
}

// LoadSnapshot reads either a single OrgInventory JSON file or a directory of
// them and returns the concatenated repository list along with a representative
// collection date. Files that fail to parse are skipped silently — the diff
// command should not abort when one snapshot is partially corrupt.
func LoadSnapshot(path string) ([]Repository, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("stat %q: %w", path, err)
	}

	var files []string
	if info.IsDir() {
		matches, err := filepath.Glob(filepath.Join(path, "*.json"))
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("glob %q: %w", path, err)
		}
		sort.Strings(matches)
		files = matches
	} else {
		files = []string{path}
	}

	var repos []Repository
	var date time.Time
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var inv OrgInventory
		if err := json.Unmarshal(body, &inv); err != nil {
			continue
		}
		if date.IsZero() {
			if !inv.CollectedAt.IsZero() {
				date = inv.CollectedAt
			} else if len(inv.Repositories) > 0 {
				date = inv.Repositories[0].CollectedAt
			}
		}
		repos = append(repos, inv.Repositories...)
	}
	return repos, date, nil
}

// ComputeDiff compares two snapshots keyed on FullName. The from/to slices are
// the per-repo data from the older and newer snapshots respectively; pass
// dates separately when they're known so the rendered header can show them.
func ComputeDiff(fromRepos, toRepos []Repository, fromDate, toDate time.Time) Diff {
	d := Diff{FromDate: fromDate, ToDate: toDate}

	fromIdx := indexByFullName(fromRepos)
	toIdx := indexByFullName(toRepos)

	for name := range toIdx {
		if _, ok := fromIdx[name]; !ok {
			d.Added = append(d.Added, name)
		}
	}
	for name := range fromIdx {
		if _, ok := toIdx[name]; !ok {
			d.Removed = append(d.Removed, name)
		}
	}

	for name, to := range toIdx {
		from, ok := fromIdx[name]
		if !ok {
			continue
		}

		switch {
		case from.LicenseSPDXID == "" && to.LicenseSPDXID != "":
			d.LicenseAdded = append(d.LicenseAdded, DiffEntry{FullName: name, Note: to.LicenseSPDXID})
		case from.LicenseSPDXID != "" && to.LicenseSPDXID == "":
			d.LicenseRemoved = append(d.LicenseRemoved, DiffEntry{FullName: name, Note: from.LicenseSPDXID})
		case from.LicenseSPDXID != "" && to.LicenseSPDXID != "" && from.LicenseSPDXID != to.LicenseSPDXID:
			d.LicenseChanged = append(d.LicenseChanged, DiffEntry{
				FullName: name,
				Note:     fmt.Sprintf("%s -> %s", from.LicenseSPDXID, to.LicenseSPDXID),
			})
		}

		if !from.UsesComplianceWorkflow && to.UsesComplianceWorkflow {
			d.ComplianceAdded = append(d.ComplianceAdded, name)
		} else if from.UsesComplianceWorkflow && !to.UsesComplianceWorkflow {
			d.ComplianceRemoved = append(d.ComplianceRemoved, name)
		}

		if !from.IsArchived && to.IsArchived {
			d.Archived = append(d.Archived, name)
		} else if from.IsArchived && !to.IsArchived {
			d.Unarchived = append(d.Unarchived, name)
		}

		// Per-check transitions. Compare success<->failure flips for each
		// check kind separately. Skip entirely when either side has no
		// ComplianceChecks (workflow appearance/disappearance is already
		// covered by ComplianceAdded/Removed above).
		if from.ComplianceChecks != nil && to.ComplianceChecks != nil {
			recordCheckFlip(name,
				from.ComplianceChecks.SecretsVuln.Status,
				to.ComplianceChecks.SecretsVuln.Status,
				&d.SecretsVulnFlippedToFailure, &d.SecretsVulnFlippedToSuccess)
			recordCheckFlip(name,
				from.ComplianceChecks.License.Status,
				to.ComplianceChecks.License.Status,
				&d.LicenseFlippedToFailure, &d.LicenseFlippedToSuccess)
		}

		fromStale := isStale(from)
		toStale := isStale(to)
		if !fromStale && toStale {
			d.BecameStale = append(d.BecameStale, name)
		} else if fromStale && !toStale {
			d.BecameActive = append(d.BecameActive, name)
		}
	}

	sortDiff(&d)
	return d
}

// recordCheckFlip appends name to the appropriate "flipped" slice when a
// check transitioned between success and failure. Other transitions
// (no_run -> success, in_progress -> success, etc.) are intentionally not
// surfaced — they're noisy and the report's Compliance status section
// already shows the current state.
func recordCheckFlip(name, fromStatus, toStatus string, toFailure, toSuccess *[]string) {
	switch {
	case fromStatus == "success" && toStatus == "failure":
		*toFailure = append(*toFailure, name)
	case fromStatus == "failure" && toStatus == "success":
		*toSuccess = append(*toSuccess, name)
	}
}

// isStale mirrors the rule used by collector.go and report.go: zero commits
// in the last 365 days *and* a pushed_at older than 365 days. Using the
// repo's own CollectedAt as "now" keeps the result stable across re-runs.
func isStale(r Repository) bool {
	now := r.CollectedAt
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-365 * 24 * time.Hour)
	return r.CommitsLast365d == 0 && r.PushedAt.Before(cutoff)
}

func indexByFullName(repos []Repository) map[string]Repository {
	out := make(map[string]Repository, len(repos))
	for _, r := range repos {
		out[r.FullName] = r
	}
	return out
}

func sortDiff(d *Diff) {
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.ComplianceAdded)
	sort.Strings(d.ComplianceRemoved)
	sort.Strings(d.Archived)
	sort.Strings(d.Unarchived)
	sort.Strings(d.SecretsVulnFlippedToFailure)
	sort.Strings(d.SecretsVulnFlippedToSuccess)
	sort.Strings(d.LicenseFlippedToFailure)
	sort.Strings(d.LicenseFlippedToSuccess)
	sort.Strings(d.BecameStale)
	sort.Strings(d.BecameActive)
	sortEntries(d.LicenseAdded)
	sortEntries(d.LicenseRemoved)
	sortEntries(d.LicenseChanged)
}

func sortEntries(es []DiffEntry) {
	sort.Slice(es, func(i, j int) bool { return es[i].FullName < es[j].FullName })
}

// RenderDiffMarkdown produces the Markdown body for either the standalone
// `diff` command or the report's "Changes since previous snapshot" section.
// The header line is included; callers wanting a subsection should prepend
// their own. Empty diffs render a single placeholder line.
func RenderDiffMarkdown(d Diff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Diff: %s -> %s\n\n", formatDiffDate(d.FromDate), formatDiffDate(d.ToDate))
	if d.IsEmpty() {
		b.WriteString("No changes between snapshots.\n")
		return b.String()
	}

	writeBullets(&b, "Repositories added", d.Added)
	writeBullets(&b, "Repositories removed", d.Removed)
	writeEntries(&b, "License added", d.LicenseAdded)
	writeEntries(&b, "License removed", d.LicenseRemoved)
	writeEntries(&b, "License changed", d.LicenseChanged)
	writeBullets(&b, "Compliance workflow added", d.ComplianceAdded)
	writeBullets(&b, "Compliance workflow removed", d.ComplianceRemoved)
	writeBullets(&b, "Archived", d.Archived)
	writeBullets(&b, "Unarchived", d.Unarchived)
	writeBullets(&b, "Secrets/vuln check flipped to failure", d.SecretsVulnFlippedToFailure)
	writeBullets(&b, "Secrets/vuln check flipped to success", d.SecretsVulnFlippedToSuccess)
	writeBullets(&b, "License check flipped to failure", d.LicenseFlippedToFailure)
	writeBullets(&b, "License check flipped to success", d.LicenseFlippedToSuccess)
	writeBullets(&b, "Became stale", d.BecameStale)
	writeBullets(&b, "Became active", d.BecameActive)
	return b.String()
}

func writeBullets(b *strings.Builder, header string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n", header)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
	b.WriteString("\n")
}

func writeEntries(b *strings.Builder, header string, items []DiffEntry) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n", header)
	for _, it := range items {
		if it.Note != "" {
			fmt.Fprintf(b, "- %s (%s)\n", it.FullName, it.Note)
		} else {
			fmt.Fprintf(b, "- %s\n", it.FullName)
		}
	}
	b.WriteString("\n")
}

func formatDiffDate(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	return t.UTC().Format("2006-01-02")
}
