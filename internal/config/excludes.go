// Package config provides loaders for optional repo-level configuration files
// that influence what the inventory tool collects and reports.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Excludes is a compiled list of "skip this repo" patterns. Use IsExcluded
// to test a candidate (org, repo) pair. The zero value is a valid empty
// matcher that excludes nothing.
type Excludes struct {
	exact     map[string]struct{} // "<org>/<name>"
	anyOrg    map[string]struct{} // "<name>" derived from "*/<name>"
}

type excludesFile struct {
	Excludes []string `yaml:"excludes"`
}

// Load reads and parses an excludes YAML file. A missing file returns
// (nil, fs.ErrNotExist) — callers that prefer to proceed without excludes
// should use LoadOrEmpty.
func Load(path string) (*Excludes, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw excludesFile
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return compile(raw.Excludes), nil
}

// LoadOrEmpty wraps Load with the policy "missing file is fine". A missing
// file logs at INFO level and returns an empty matcher; any other parse
// error logs at WARN and also returns an empty matcher. This keeps the CLI
// usable in environments that haven't seeded the config (e.g. fresh clones
// or one-off invocations).
func LoadOrEmpty(path string, logger *slog.Logger) *Excludes {
	if logger == nil {
		logger = slog.Default()
	}
	e, err := Load(path)
	if err == nil {
		return e
	}
	if errors.Is(err, fs.ErrNotExist) {
		logger.Info("excludes config not found, proceeding with no excludes", "path", path)
		return &Excludes{}
	}
	logger.Warn("excludes config unreadable, proceeding with no excludes", "path", path, "err", err)
	return &Excludes{}
}

func compile(patterns []string) *Excludes {
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
