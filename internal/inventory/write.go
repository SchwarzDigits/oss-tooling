package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// writeOrgInventory serializes inv as pretty JSON and writes it atomically
// (tmp + rename) to both <outputDir>/<dateStamp>/<org>.json and
// <outputDir>/latest/<org>.json.
func writeOrgInventory(outputDir, dateStamp string, inv OrgInventory) error {
	body, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %q: %w", inv.Org, err)
	}

	dated := filepath.Join(outputDir, dateStamp)
	latest := filepath.Join(outputDir, "latest")
	for _, dir := range []string{dated, latest} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
		target := filepath.Join(dir, inv.Org+".json")
		if err := atomicWrite(dir, target, body); err != nil {
			return err
		}
	}
	return nil
}

func atomicWrite(dir, target string, body []byte) error {
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", tmpPath, target, err)
	}
	cleanup = false
	return nil
}
