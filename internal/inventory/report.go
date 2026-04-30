package inventory

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"text/template"
	"time"
)

//go:embed report.tmpl
var reportTemplate string

// reportData is the rendered model passed to the template.
type reportData struct {
	GeneratedAt     time.Time
	InputDir        string
	Orgs            []orgRow
	Totals          totalRow
	MissingLicense  []repoRow
	MissingSecurity []repoRow
	Stale           []staleRow
	UsesCompliance  []repoRow
	Languages       []langRow
	TopStars        []topStarRow
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

type repoRow struct {
	Org  string
	Name string
	URL  string
}

type staleRow struct {
	Org      string
	Name     string
	PushedAt time.Time
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
// Markdown summary to outputPath (atomic tmp+rename). Files that fail to
// parse are skipped with a warning log.
func GenerateReport(inputDir, outputPath string) error {
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
		for _, r := range inv.Repositories {
			allRepos = append(allRepos, r)
		}
	}

	now := time.Now().UTC()
	staleCutoff := now.Add(-365 * 24 * time.Hour)
	data := reportData{
		GeneratedAt: now,
		InputDir:    inputDir,
	}

	langCounts := map[string]int{}

	for _, r := range allRepos {
		idx, ok := orgIndex[r.Org]
		if !ok {
			// Org wasn't represented by its own file; create a synthetic row.
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
			data.MissingLicense = append(data.MissingLicense, repoRow{Org: r.Org, Name: r.Name, URL: r.URL})
		}
		if !r.HasSecurity {
			data.MissingSecurity = append(data.MissingSecurity, repoRow{Org: r.Org, Name: r.Name, URL: r.URL})
		}
		if r.CommitsLast365d == 0 && r.PushedAt.Before(staleCutoff) {
			row.Stale++
			data.Totals.Stale++
			data.Stale = append(data.Stale, staleRow{Org: r.Org, Name: r.Name, PushedAt: r.PushedAt})
		}
		if r.UsesComplianceWorkflow {
			row.UsesCompliance++
			data.Totals.UsesCompliance++
			data.UsesCompliance = append(data.UsesCompliance, repoRow{Org: r.Org, Name: r.Name, URL: r.URL})
		}
		if lang := r.PrimaryLang; lang != "" {
			langCounts[lang]++
		}
	}

	data.Orgs = orgs

	for name, count := range langCounts {
		data.Languages = append(data.Languages, langRow{Name: name, Count: count})
	}
	sort.Slice(data.Languages, func(i, j int) bool {
		if data.Languages[i].Count != data.Languages[j].Count {
			return data.Languages[i].Count > data.Languages[j].Count
		}
		return data.Languages[i].Name < data.Languages[j].Name
	})

	sort.Slice(data.MissingLicense, func(i, j int) bool {
		if data.MissingLicense[i].Org != data.MissingLicense[j].Org {
			return data.MissingLicense[i].Org < data.MissingLicense[j].Org
		}
		return data.MissingLicense[i].Name < data.MissingLicense[j].Name
	})
	sort.Slice(data.MissingSecurity, func(i, j int) bool {
		if data.MissingSecurity[i].Org != data.MissingSecurity[j].Org {
			return data.MissingSecurity[i].Org < data.MissingSecurity[j].Org
		}
		return data.MissingSecurity[i].Name < data.MissingSecurity[j].Name
	})
	sort.Slice(data.Stale, func(i, j int) bool {
		return data.Stale[i].PushedAt.Before(data.Stale[j].PushedAt)
	})
	sort.Slice(data.UsesCompliance, func(i, j int) bool {
		if data.UsesCompliance[i].Org != data.UsesCompliance[j].Org {
			return data.UsesCompliance[i].Org < data.UsesCompliance[j].Org
		}
		return data.UsesCompliance[i].Name < data.UsesCompliance[j].Name
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
