package sched

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// zoneDir is where Go looks first for zones on Linux and macOS.
var zoneDir = "/usr/share/zoneinfo"

// LoadZone is time.LoadLocation with exact-case names on every system. macOS's
// zoneinfo directory is case-insensitive, so "europe/istanbul" loads there while
// Linux, and cron on it, rejects it: a crontab audited on a Mac would pass.
func LoadZone(name string) (*time.Location, error) {
	loc, err := time.LoadLocation(name)
	if err != nil || name == "" || name == "UTC" || name == "Local" {
		return loc, err
	}
	dir := zoneDir
	for _, seg := range strings.Split(name, "/") {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return loc, nil // no system database: Go's embedded copy matches case exactly
		}
		if slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == seg }) {
			dir = filepath.Join(dir, seg)
			continue
		}
		if slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.EqualFold(e.Name(), seg) }) {
			return nil, fmt.Errorf("unknown time zone %s (names are case-sensitive)", name)
		}
		return loc, nil // not in the system database at all, so Go loaded its embedded copy
	}
	return loc, nil
}
