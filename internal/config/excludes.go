package config

import (
	"log/slog"
	"strings"
)

// Excludes is a compiled list of "skip this repo" patterns. Use IsExcluded
// to test a candidate (org, repo) pair. The zero value is a valid empty
// matcher that excludes nothing.
type Excludes struct {
	exact  map[string]struct{} // "<org>/<name>"
	anyOrg map[string]struct{} // "<name>" derived from "*/<name>"
}

// NewExcludes compiles a slice of pattern strings into an Excludes matcher.
// Supported patterns:
//
//   - "<org>/<repo>" — exact, case-sensitive match against "<org>/<repo>"
//   - "*/<repo>"     — match the repo segment across any org
//
// Anything else is logged at WARN and ignored. Empty / whitespace-only
// patterns are silently dropped.
func NewExcludes(patterns []string) *Excludes {
	logger := slog.Default()
	e := &Excludes{
		exact:  make(map[string]struct{}),
		anyOrg: make(map[string]struct{}),
	}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch {
		case strings.HasPrefix(p, "*/"):
			name := strings.TrimPrefix(p, "*/")
			if name == "" || strings.Contains(name, "/") {
				logger.Warn("excludes: ignoring invalid wildcard pattern", "pattern", p)
				continue
			}
			e.anyOrg[name] = struct{}{}
		case strings.Count(p, "/") == 1 && !strings.ContainsAny(p, "*"):
			e.exact[p] = struct{}{}
		default:
			logger.Warn("excludes: ignoring unsupported pattern", "pattern", p)
		}
	}
	return e
}

// IsExcluded reports whether (org, repo) matches any compiled pattern.
// Comparison is case-sensitive — GitHub treats org and repo names as
// case-insensitive in URLs but the underlying API returns canonical casing,
// which is what callers should pass in.
func (e *Excludes) IsExcluded(org, repo string) bool {
	if e == nil {
		return false
	}
	if _, ok := e.anyOrg[repo]; ok {
		return true
	}
	if _, ok := e.exact[org+"/"+repo]; ok {
		return true
	}
	return false
}
