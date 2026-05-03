package inventory

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
)

//go:embed report.tmpl
var reportTemplate string

// reportData is the rendered model passed to the template.
type reportData struct {
	GeneratedAt time.Time
	InputDir    string

	HasDiff      bool
	DiffMarkdown string

	StatusComplianceAdoption string
	StatusComplianceDelta    string
	StatusLicense            string
	StatusLicenseDelta       string
	StatusFailingRuns        int
	StatusActiveCount        int
	StatusActiveDelta        string
	HasActiveCountDelta      bool
	StatusNewSincePrev       int
	StatusArchivedSincePrev  int

	Orgs   []orgRow
	Totals totalRow

	Compliance         []complianceRow
	ComplianceCount    int
	ComplianceGreen    int
	ComplianceFailing  int
	ComplianceRunning  int
	ComplianceNeverRun int

	MissingLicensePublic []missingLicenseRow
	MigrationPriority    []migrationPriorityRow
	ArchiveCandidates    []archiveRow

	Languages []langRow
	TopStars  []topStarRow
}

type orgRow struct {
	Name           string
	Active         int
	Stale          int
	Archived       int
	Total          int
	WithLicense    int
	UsesCompliance int
}

type totalRow struct {
	Active         int
	Stale          int
	Archived       int
	Total          int
	WithLicense    int
	UsesCompliance int
}

type complianceRow struct {
	FullName    string
	URL         string // repo URL — currently unused in the rendered table but kept for future drill-downs
	RunURL      string // workflow run URL; empty when the workflow has never been triggered
	LastRunRel  string
	StatusEmoji string
	FailedJobs  string
}

type missingLicenseRow struct {
	Org        string
	Name       string
	URL        string
	PushedAt   time.Time
	OwnerLabel string
}

type migrationPriorityRow struct {
	Org        string
	Name       string
	URL        string
	Stars      int
	PushedAt   time.Time
	Status     string // "active" | "stale" — archived repos are filtered out of this section
	OwnerLabel string
}

type archiveRow struct {
	Org      string
	Name     string
	URL      string
	Stars    int
	Forks    int
	PushedAt time.Time
	Hint     string
}

type langRow struct {
	Name  string
	Count int
}

type topStarRow struct {
	Org   string
	Name  string
	Stars int
	Forks int
}

// GenerateReport reads OrgInventory JSON files from inputDir and writes a
// Markdown summary to outputPath (atomic tmp+rename). When diffFromPath is
// non-empty, a "Changes since previous snapshot" section is rendered near
// the top using ComputeDiff against that older snapshot. Files that fail
// to parse are skipped with a warning log.
func GenerateReport(inputDir, outputPath, diffFromPath string) error {
	logger := slog.Default()

	matches, err := filepath.Glob(filepath.Join(inputDir, "*.json"))
	if err != nil {
		return fmt.Errorf("glob %q: %w", inputDir, err)
	}
	sort.Strings(matches)

	var allRepos []Repository
	orgIndex := make(map[string]int)
	var orgs []orgRow

	for _, path := range matches {
		body, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("skipping unreadable file", "path", path, "err", err)
			continue
		}
		var inv OrgInventory
		if err := json.Unmarshal(body, &inv); err != nil {
			logger.Warn("skipping invalid JSON", "path", path, "err", err)
			continue
		}
		if _, seen := orgIndex[inv.Org]; seen {
			logger.Warn("duplicate org file, last one wins", "org", inv.Org, "path", path)
		}
		orgIndex[inv.Org] = len(orgs)
		orgs = append(orgs, orgRow{Name: inv.Org})
		allRepos = append(allRepos, inv.Repositories...)
	}

	now := time.Now().UTC()
	data := reportData{
		GeneratedAt: now,
		InputDir:    inputDir,
	}

	var prevRepos []Repository
	if diffFromPath != "" {
		fromRepos, fromDate, err := LoadSnapshot(diffFromPath)
		if err != nil {
			logger.Warn("diff-from snapshot unreadable, omitting diff section", "path", diffFromPath, "err", err)
		} else {
			prevRepos = fromRepos
			toDate := snapshotDate(allRepos)
			diff := ComputeDiff(fromRepos, allRepos, fromDate, toDate)
			data.HasDiff = true
			data.DiffMarkdown = strings.TrimRight(stripDiffHeader(RenderDiffMarkdown(diff)), "\n") + "\n"
			data.StatusNewSincePrev = len(diff.Added)
			data.StatusArchivedSincePrev = len(diff.Archived)
		}
	}

	staleCutoff := now.Add(-365 * 24 * time.Hour)

	// Roll up per-org and total counts in a single pass. Status
	// (active/stale/archived) is computed via repoStatus and used for both
	// the per-org table and the active-denominator percentages in the
	// status header.
	langCounts := map[string]int{}
	var activeUsesCompliance, activeWithLicense int
	for _, r := range allRepos {
		idx, ok := orgIndex[r.Org]
		if !ok {
			orgIndex[r.Org] = len(orgs)
			orgs = append(orgs, orgRow{Name: r.Org})
			idx = orgIndex[r.Org]
		}
		row := &orgs[idx]
		row.Total++
		data.Totals.Total++
		status := repoStatus(r, staleCutoff)
		switch status {
		case "active":
			row.Active++
			data.Totals.Active++
		case "stale":
			row.Stale++
			data.Totals.Stale++
		case "archived":
			row.Archived++
			data.Totals.Archived++
		}
		if r.HasLicense {
			row.WithLicense++
			data.Totals.WithLicense++
			if status == "active" {
				activeWithLicense++
			}
		}
		if r.UsesComplianceWorkflow {
			row.UsesCompliance++
			data.Totals.UsesCompliance++
			if status == "active" {
				activeUsesCompliance++
			}
		}
		if lang := r.PrimaryLang; lang != "" {
			langCounts[lang]++
		}
	}
	data.Orgs = orgs
	data.StatusActiveCount = data.Totals.Active

	// Status-header percentages use the active count as denominator —
	// stale and archived repos can't realistically be onboarded, and
	// including them understates real adoption progress.
	data.StatusComplianceAdoption = formatActiveRatio(activeUsesCompliance, data.Totals.Active)
	data.StatusLicense = formatActiveAndOverall(activeWithLicense, data.Totals.Active,
		data.Totals.WithLicense, data.Totals.Total)

	if data.HasDiff {
		data.StatusComplianceDelta = formatDelta(
			countActiveCompliance(allRepos, staleCutoff) - countActiveCompliance(prevRepos, staleCutoff))
		data.StatusLicenseDelta = formatDelta(
			countActiveLicense(allRepos, staleCutoff) - countActiveLicense(prevRepos, staleCutoff))

		prevActive := countActive(prevRepos, staleCutoff)
		if delta := data.StatusActiveCount - prevActive; delta != 0 {
			data.HasActiveCountDelta = true
			data.StatusActiveDelta = formatDelta(delta)
		}
	}

	for _, r := range allRepos {
		if r.UsesComplianceWorkflow {
			row := complianceRow{
				FullName:    r.FullName,
				URL:         r.URL,
				RunURL:      r.LastComplianceRunURL,
				LastRunRel:  humanRelative(r.LastComplianceRunAt, now),
				StatusEmoji: statusEmoji(r.LastComplianceRunStatus, r.LastComplianceRunConclusion),
				FailedJobs:  formatFailedJobs(r.LastComplianceRunConclusion, r.LastComplianceRunFailedJobs),
			}
			data.Compliance = append(data.Compliance, row)
			data.ComplianceCount++
			switch {
			case r.LastComplianceRunConclusion == "success":
				data.ComplianceGreen++
			case r.LastComplianceRunConclusion == "failure":
				data.ComplianceFailing++
			case r.LastComplianceRunStatus == "in_progress" || r.LastComplianceRunStatus == "queued":
				data.ComplianceRunning++
			default:
				data.ComplianceNeverRun++
			}
		}
	}
	data.StatusFailingRuns = data.ComplianceFailing
	sort.Slice(data.Compliance, func(i, j int) bool {
		return data.Compliance[i].FullName < data.Compliance[j].FullName
	})

	for _, r := range allRepos {
		if r.HasLicense || r.Visibility != "public" {
			continue
		}
		data.MissingLicensePublic = append(data.MissingLicensePublic, missingLicenseRow{
			Org:        r.Org,
			Name:       r.Name,
			URL:        r.URL,
			PushedAt:   r.PushedAt,
			OwnerLabel: ownerLabel(r),
		})
	}
	sort.Slice(data.MissingLicensePublic, func(i, j int) bool {
		return data.MissingLicensePublic[i].PushedAt.After(data.MissingLicensePublic[j].PushedAt)
	})

	for _, r := range allRepos {
		if r.UsesComplianceWorkflow || r.IsArchived || r.Visibility != "public" {
			continue
		}
		// Status here is "active" or "stale" only — archived repos were
		// dropped above; the unified status helper would otherwise also
		// return "archived".
		data.MigrationPriority = append(data.MigrationPriority, migrationPriorityRow{
			Org:        r.Org,
			Name:       r.Name,
			URL:        r.URL,
			Stars:      r.Stars,
			PushedAt:   r.PushedAt,
			Status:     repoStatus(r, staleCutoff),
			OwnerLabel: ownerLabel(r),
		})
	}
	sort.Slice(data.MigrationPriority, func(i, j int) bool {
		a, b := data.MigrationPriority[i], data.MigrationPriority[j]
		if a.Stars != b.Stars {
			return a.Stars > b.Stars
		}
		return a.PushedAt.After(b.PushedAt)
	})

	for _, r := range allRepos {
		if repoStatus(r, staleCutoff) != "stale" {
			continue
		}
		data.ArchiveCandidates = append(data.ArchiveCandidates, archiveRow{
			Org:      r.Org,
			Name:     r.Name,
			URL:      r.URL,
			Stars:    r.Stars,
			Forks:    r.Forks,
			PushedAt: r.PushedAt,
			Hint:     archiveHint(r, now),
		})
	}
	sort.Slice(data.ArchiveCandidates, func(i, j int) bool {
		return data.ArchiveCandidates[i].PushedAt.Before(data.ArchiveCandidates[j].PushedAt)
	})

	for name, count := range langCounts {
		data.Languages = append(data.Languages, langRow{Name: name, Count: count})
	}
	sort.Slice(data.Languages, func(i, j int) bool {
		if data.Languages[i].Count != data.Languages[j].Count {
			return data.Languages[i].Count > data.Languages[j].Count
		}
		return data.Languages[i].Name < data.Languages[j].Name
	})

	starSorted := make([]Repository, len(allRepos))
	copy(starSorted, allRepos)
	sort.Slice(starSorted, func(i, j int) bool {
		if starSorted[i].Stars != starSorted[j].Stars {
			return starSorted[i].Stars > starSorted[j].Stars
		}
		return starSorted[i].FullName < starSorted[j].FullName
	})
	limit := 20
	if len(starSorted) < limit {
		limit = len(starSorted)
	}
	for i := 0; i < limit; i++ {
		r := starSorted[i]
		data.TopStars = append(data.TopStars, topStarRow{
			Org:   r.Org,
			Name:  r.Name,
			Stars: r.Stars,
			Forks: r.Forks,
		})
	}

	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
	}
	return atomicWrite(filepath.Dir(outputPath), outputPath, buf.Bytes())
}

// snapshotDate returns the most plausible "as of" date for a slice of repos,
// i.e. the first non-zero CollectedAt encountered.
func snapshotDate(repos []Repository) time.Time {
	for _, r := range repos {
		if !r.CollectedAt.IsZero() {
			return r.CollectedAt
		}
	}
	return time.Time{}
}

// stripDiffHeader removes the leading "## Diff: ..." line so the report's
// own "## Changes since previous snapshot" header isn't followed immediately
// by another H2 of the same logical section.
func stripDiffHeader(md string) string {
	if !strings.HasPrefix(md, "## Diff:") {
		return md
	}
	if i := strings.Index(md, "\n"); i >= 0 {
		return strings.TrimLeft(md[i+1:], "\n")
	}
	return md
}

// formatRatio renders "n / total repos (p%)". When total is zero, percentage
// shows as "0%" rather than dividing by zero.
func formatRatio(n, total int) string {
	if total == 0 {
		return fmt.Sprintf("%d / %d repos (0%%)", n, total)
	}
	pct := int(math.Round(100 * float64(n) / float64(total)))
	return fmt.Sprintf("%d / %d repos (%d%%)", n, total, pct)
}

// formatActiveRatio is formatRatio specialized to the "active" denominator
// used in the status header. Spelling out "active repos" in the rendered
// string is what makes the percentage interpretable to a reader who
// hasn't seen the Status definitions footer yet.
func formatActiveRatio(n, activeTotal int) string {
	if activeTotal == 0 {
		return fmt.Sprintf("%d / %d active repos (0%%)", n, activeTotal)
	}
	pct := int(math.Round(100 * float64(n) / float64(activeTotal)))
	return fmt.Sprintf("%d / %d active repos (%d%%)", n, activeTotal, pct)
}

// formatActiveAndOverall is used for license compliance — legal obligation
// applies regardless of activity, so we surface both the active-only ratio
// (to set realistic targets) and the overall ratio (to keep the absolute
// gap visible).
func formatActiveAndOverall(activeN, activeTotal, overallN, overallTotal int) string {
	active := formatActiveRatio(activeN, activeTotal)
	overallPct := 0
	if overallTotal > 0 {
		overallPct = int(math.Round(100 * float64(overallN) / float64(overallTotal)))
	}
	return fmt.Sprintf("%s — overall: %d / %d (%d%%)", active, overallN, overallTotal, overallPct)
}

// formatDelta renders "+N" / "-N" / "unchanged" for status-bullet deltas.
func formatDelta(d int) string {
	switch {
	case d > 0:
		return fmt.Sprintf("+%d", d)
	case d < 0:
		return fmt.Sprintf("%d", d)
	default:
		return "unchanged"
	}
}

// countActive returns how many repos in the slice are currently active
// (not archived, pushed within staleCutoff). Used for the active-count
// delta in the status header.
func countActive(repos []Repository, staleCutoff time.Time) int {
	n := 0
	for _, r := range repos {
		if repoStatus(r, staleCutoff) == "active" {
			n++
		}
	}
	return n
}

// countActiveCompliance returns how many active repos use the central
// compliance workflow. Active-only is what the diff delta should compare,
// since adoption progress is only meaningful among onboardable repos.
func countActiveCompliance(repos []Repository, staleCutoff time.Time) int {
	n := 0
	for _, r := range repos {
		if r.UsesComplianceWorkflow && repoStatus(r, staleCutoff) == "active" {
			n++
		}
	}
	return n
}

// countActiveLicense returns how many active repos have a LICENSE file.
func countActiveLicense(repos []Repository, staleCutoff time.Time) int {
	n := 0
	for _, r := range repos {
		if r.HasLicense && repoStatus(r, staleCutoff) == "active" {
			n++
		}
	}
	return n
}

// humanRelative buckets t-now into "X minutes/hours/days/weeks ago".
// Zero times render as "—" so empty cells are visually distinct.
func humanRelative(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := now.Sub(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Hour:
		m := int(d / time.Minute)
		if m <= 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 14*24*time.Hour:
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		weeks := int(d / (7 * 24 * time.Hour))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	}
}

// statusEmoji translates a (status, conclusion) pair to a short emoji label.
// "no run yet" applies when both fields are empty.
func statusEmoji(status, conclusion string) string {
	switch {
	case conclusion == "success":
		return "✅ success"
	case conclusion == "failure":
		return "❌ failure"
	case status == "in_progress" || status == "queued":
		return "⏳ in progress"
	default:
		return "⚪ no run yet"
	}
}

func formatFailedJobs(conclusion string, jobs []string) string {
	if conclusion == "success" || len(jobs) == 0 {
		return "—"
	}
	return strings.Join(jobs, ", ")
}

// ownerLabel renders the per-repo Likely owner cell. Missing values show as
// "(unowned)". CODEOWNERS values already include a leading "@". Top-committer
// values are GitHub logins without a leading "@", so we prepend one.
//
// Suffix vocabulary is explained in the report's "Status definitions"
// footer:
//
//   - (CO)        — owner came from a CODEOWNERS file
//   - (committer) — owner is the dominant author in the last 100 commits
//
// Any "top_committer_*" source is treated the same way so reports can be
// regenerated against snapshots that predate the rename of "top_committer_90d"
// to "top_committer_recent".
func ownerLabel(r Repository) string {
	if r.LikelyOwner == "" {
		return "(unowned)"
	}
	if r.LikelyOwnerSource == "codeowners" {
		owner := r.LikelyOwner
		if !strings.HasPrefix(owner, "@") {
			owner = "@" + owner
		}
		return fmt.Sprintf("%s (CO)", owner)
	}
	if strings.HasPrefix(r.LikelyOwnerSource, "top_committer_") {
		return fmt.Sprintf("@%s (committer)", strings.TrimPrefix(r.LikelyOwner, "@"))
	}
	return r.LikelyOwner
}

// archiveHint produces the per-row triage suggestion. Forks get a dedicated
// hint because the relevance question turns on upstream activity, not stars.
// "Possibly still active" is a softer label for the 12-24 month range — the
// repo crossed the stale threshold but isn't ancient enough to recommend
// archiving outright.
func archiveHint(r Repository, now time.Time) string {
	if r.IsFork {
		return "Fork — check if upstream still relevant"
	}
	if r.Stars > 5 || r.Forks > 5 {
		return "Decision needed"
	}
	// 730 days ≈ 24 months, matching the existing 365-day stale cutoff idiom.
	if now.Sub(r.PushedAt) >= 730*24*time.Hour {
		return "Archive recommended"
	}
	return "Possibly still active — verify before archiving"
}

// repoStatus returns the unified compliance lifecycle state for a repo.
// Evaluation order matters: archived overrides everything (a repo is
// archived even if it was very recently pushed before being archived).
// IsFork is metadata, not a status — actively maintained forks are
// independent software with their own compliance posture.
func repoStatus(r Repository, staleCutoff time.Time) string {
	if r.IsArchived {
		return "archived"
	}
	if r.PushedAt.Before(staleCutoff) {
		return "stale"
	}
	return "active"
}
