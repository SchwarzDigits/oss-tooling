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
	Total          int
	WithLicense    int
	MissingLicense int
	Stale          int
	UsesCompliance int
}

type totalRow struct {
	Total          int
	WithLicense    int
	MissingLicense int
	Stale          int
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
	Status     string // "active" | "stale" | "fork"
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

	langCounts := map[string]int{}
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
		if r.HasLicense {
			row.WithLicense++
			data.Totals.WithLicense++
		} else {
			row.MissingLicense++
			data.Totals.MissingLicense++
		}
		if r.CommitsLast365d == 0 && r.PushedAt.Before(staleCutoff) {
			row.Stale++
			data.Totals.Stale++
		}
		if r.UsesComplianceWorkflow {
			row.UsesCompliance++
			data.Totals.UsesCompliance++
		}
		if lang := r.PrimaryLang; lang != "" {
			langCounts[lang]++
		}
	}
	data.Orgs = orgs

	data.StatusComplianceAdoption = formatRatio(data.Totals.UsesCompliance, data.Totals.Total)
	data.StatusLicense = formatRatio(data.Totals.WithLicense, data.Totals.Total)

	if data.HasDiff {
		data.StatusComplianceDelta = formatDelta(
			countCompliance(allRepos) - countCompliance(prevRepos))
		data.StatusLicenseDelta = formatDelta(
			countLicense(allRepos) - countLicense(prevRepos))
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
		data.MigrationPriority = append(data.MigrationPriority, migrationPriorityRow{
			Org:        r.Org,
			Name:       r.Name,
			URL:        r.URL,
			Stars:      r.Stars,
			PushedAt:   r.PushedAt,
			Status:     migrationStatus(r, staleCutoff),
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
		stale := r.CommitsLast365d == 0 && r.PushedAt.Before(staleCutoff)
		if !stale {
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

func countCompliance(repos []Repository) int {
	n := 0
	for _, r := range repos {
		if r.UsesComplianceWorkflow {
			n++
		}
	}
	return n
}

func countLicense(repos []Repository) int {
	n := 0
	for _, r := range repos {
		if r.HasLicense {
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
func ownerLabel(r Repository) string {
	if r.LikelyOwner == "" {
		return "(unowned)"
	}
	switch r.LikelyOwnerSource {
	case "codeowners":
		owner := r.LikelyOwner
		if !strings.HasPrefix(owner, "@") {
			owner = "@" + owner
		}
		return fmt.Sprintf("%s (CODEOWNERS)", owner)
	case "top_committer_90d":
		return fmt.Sprintf("@%s (recent committer)", strings.TrimPrefix(r.LikelyOwner, "@"))
	default:
		return r.LikelyOwner
	}
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

// migrationStatus labels a repo for the Migration Priority table. "fork"
// overrides the active/stale distinction because a fork's compliance posture
// often follows its upstream's rather than the org's policy.
func migrationStatus(r Repository, staleCutoff time.Time) string {
	if r.IsFork {
		return "fork"
	}
	if r.PushedAt.Before(staleCutoff) {
		return "stale"
	}
	return "active"
}
