// Package config provides loaders for optional repo-level configuration files
// that influence what the inventory tool collects and reports.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

// Inventory is the parsed shape of config/inventory.yml. Both fields are
// optional: an empty Orgs list means "no default — caller must supply orgs
// via CLI flag", and a nil Excludes excludes nothing.
type Inventory struct {
	Orgs     []string
	Excludes *Excludes
}

type inventoryFile struct {
	Orgs     []string `yaml:"orgs"`
	Excludes []string `yaml:"excludes"`
}

// LoadInventory reads and parses an inventory YAML file. A missing file
// returns an error wrapping fs.ErrNotExist; callers that prefer to proceed
// without config should use LoadInventoryOrEmpty.
func LoadInventory(path string) (*Inventory, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw inventoryFile
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return &Inventory{
		Orgs:     raw.Orgs,
		Excludes: NewExcludes(raw.Excludes),
	}, nil
}

// LoadInventoryOrEmpty wraps LoadInventory with the policy "missing file is
// fine". A missing file logs at INFO and returns an empty config; any other
// parse error logs at WARN and also returns an empty config. This keeps the
// CLI usable in environments that haven't seeded the file (fresh clones,
// one-off invocations).
func LoadInventoryOrEmpty(path string, logger *slog.Logger) *Inventory {
	if logger == nil {
		logger = slog.Default()
	}
	cfg, err := LoadInventory(path)
	if err == nil {
		return cfg
	}
	if errors.Is(err, fs.ErrNotExist) {
		logger.Info("inventory config not found, proceeding with defaults", "path", path)
		return &Inventory{Excludes: NewExcludes(nil)}
	}
	logger.Warn("inventory config unreadable, proceeding with defaults", "path", path, "err", err)
	return &Inventory{Excludes: NewExcludes(nil)}
}
