package sched

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadZoneCase(t *testing.T) {
	// The real system database: on macOS "europe/istanbul" loads through
	// time.LoadLocation, on Linux it does not; LoadZone refuses it on both.
	for name, ok := range map[string]bool{
		"Europe/Istanbul": true, "America/New_York": true, "UTC": true, "Etc/GMT+3": true,
		"europe/istanbul": false, "EUROPE/Istanbul": false, "utc": false, "Nowhere/Zone": false,
	} {
		if _, err := LoadZone(name); (err == nil) != ok {
			t.Errorf("LoadZone(%q) error = %v, want ok=%v", name, err, ok)
		}
	}

	// A fake database where the file's case differs from the request, as a
	// case-insensitive file system would serve it, and one missing a zone that
	// Go then takes from its embedded copy.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Europe"), 0o755)
	os.WriteFile(filepath.Join(dir, "Europe", "ISTANBUL"), nil, 0o644)
	defer func(d string) { zoneDir = d }(zoneDir)
	zoneDir = dir
	if _, err := LoadZone("Europe/Istanbul"); err == nil {
		t.Error("Europe/Istanbul matched ISTANBUL only by case, want an error")
	}
	if loc, err := LoadZone("America/New_York"); err != nil || loc.String() != "America/New_York" {
		t.Errorf("zone missing from the system database: %v, %v; want the embedded copy", loc, err)
	}
}
