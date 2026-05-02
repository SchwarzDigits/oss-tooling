package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInventory_OrgsAndExcludes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.yml")
	body := []byte(`
orgs:
  - SchwarzDigits
  - SchwarzIT
excludes:
  - "*/.github"
  - SchwarzDigits/oss-compliance
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := LoadInventory(path)
	if err != nil {
		t.Fatalf("LoadInventory: %v", err)
	}
	if got, want := cfg.Orgs, []string{"SchwarzDigits", "SchwarzIT"}; !equalStrings(got, want) {
		t.Errorf("Orgs = %v, want %v", got, want)
	}
	if !cfg.Excludes.IsExcluded("SchwarzDigits", ".github") {
		t.Errorf("expected wildcard exclude to match")
	}
	if !cfg.Excludes.IsExcluded("SchwarzDigits", "oss-compliance") {
		t.Errorf("expected exact exclude to match")
	}
	if cfg.Excludes.IsExcluded("SchwarzIT", "platform-cli") {
		t.Errorf("non-listed repo should not match")
	}
}

func TestLoadInventoryOrEmpty_MissingFile(t *testing.T) {
	t.Parallel()

	cfg := LoadInventoryOrEmpty(filepath.Join(t.TempDir(), "missing.yml"), nil)
	if cfg == nil {
		t.Fatalf("LoadInventoryOrEmpty returned nil")
	}
	if len(cfg.Orgs) != 0 {
		t.Errorf("Orgs should be empty for missing file, got %v", cfg.Orgs)
	}
	if cfg.Excludes == nil || cfg.Excludes.IsExcluded("any", "thing") {
		t.Errorf("Excludes should be a non-matching empty matcher")
	}
}

func TestLoadInventory_PartialFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"orgs only", "orgs:\n  - Foo\n"},
		{"excludes only", "excludes:\n  - \"*/x\"\n"},
		{"empty file", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "inventory.yml")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			cfg, err := LoadInventory(path)
			if err != nil {
				t.Fatalf("LoadInventory: %v", err)
			}
			if cfg == nil {
				t.Fatalf("expected non-nil config")
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
