// Package monitor is the heartbeat monitor behind "cronplain serve".
package monitor

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Habibeyilmazz/cronplain/internal/sched"
	_ "modernc.org/sqlite" // pure Go, so the binary cross-compiles without cgo
)

// Job is a monitored scheduled job.
type Job struct {
	ID         string     `json:"id"`
	Schedule   string     `json:"schedule"`
	TZ         string     `json:"tz"`
	Grace      int        `json:"grace_seconds"`
	CreatedAt  time.Time  `json:"created_at"`
	LastPing   *time.Time `json:"last_ping"`
	AlertedDue *time.Time `json:"-"` // due time we already alerted about
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Validate checks user input and returns the parsed schedule and zone.
func (j Job) Validate() (*sched.Schedule, *time.Location, error) {
	if !idPattern.MatchString(j.ID) {
		return nil, nil, fmt.Errorf("id must be 1-64 lowercase letters, digits, - or _, starting with a letter or digit")
	}
	if j.Grace < 0 || j.Grace > 7*24*3600 {
		return nil, nil, fmt.Errorf("grace must be between 0 seconds and 7 days")
	}
	loc, err := time.LoadLocation(j.TZ)
	if err != nil || j.TZ == "" || j.TZ == "Local" {
		return nil, nil, fmt.Errorf("unknown time zone %q (use an IANA name such as Europe/Istanbul or UTC)", j.TZ)
	}
	s, err := sched.Parse(j.Schedule, sched.Auto)
	if err != nil {
		return nil, nil, err
	}
	if s.Reboot {
		return nil, nil, fmt.Errorf("@reboot has no calendar times, so lateness cannot be computed")
	}
	return s, loc, nil
}

// Store keeps jobs in SQLite.
type Store struct{ db *sql.DB }

// OpenStore opens (and creates) the database file.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		schedule TEXT NOT NULL,
		tz TEXT NOT NULL,
		grace INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		last_ping INTEGER,
		alerted_due INTEGER
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Store{db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Upsert adds a job or updates its schedule, zone and grace (keeping its ping history).
func (s *Store) Upsert(j Job) error {
	_, err := s.db.Exec(`INSERT INTO jobs (id, schedule, tz, grace, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET schedule = excluded.schedule, tz = excluded.tz, grace = excluded.grace, alerted_due = NULL`,
		j.ID, j.Schedule, j.TZ, j.Grace, j.CreatedAt.Unix())
	return err
}

// Delete removes a job; it reports whether the job existed.
func (s *Store) Delete(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// List returns all jobs ordered by id.
func (s *Store) List() ([]Job, error) {
	rows, err := s.db.Query(`SELECT id, schedule, tz, grace, created_at, last_ping, alerted_due FROM jobs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var j Job
		var created int64
		var ping, alerted sql.NullInt64
		if err := rows.Scan(&j.ID, &j.Schedule, &j.TZ, &j.Grace, &created, &ping, &alerted); err != nil {
			return nil, err
		}
		j.CreatedAt = time.Unix(created, 0)
		if ping.Valid {
			t := time.Unix(ping.Int64, 0)
			j.LastPing = &t
		}
		if alerted.Valid {
			t := time.Unix(alerted.Int64, 0)
			j.AlertedDue = &t
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ErrNotFound means no job has that id.
var ErrNotFound = errors.New("no such job")

// Ping records a heartbeat and clears any alert; it returns whether the job had been alerted as late.
func (s *Store) Ping(id string, at time.Time) (wasLate bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var alerted sql.NullInt64
	err = tx.QueryRow(`SELECT alerted_due FROM jobs WHERE id = ?`, id).Scan(&alerted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(`UPDATE jobs SET last_ping = ?, alerted_due = NULL WHERE id = ?`, at.Unix(), id); err != nil {
		return false, err
	}
	return alerted.Valid, tx.Commit()
}

// MarkAlerted remembers that the job was reported late for this due time.
func (s *Store) MarkAlerted(id string, due time.Time) error {
	_, err := s.db.Exec(`UPDATE jobs SET alerted_due = ? WHERE id = ?`, due.Unix(), id)
	return err
}

// State is a job's health.
type State string

const (
	StateNew   State = "new"   // never pinged, first run not due yet
	StateUp    State = "up"    // pinged and the next ping is not due yet
	StateLate  State = "late"  // the expected ping is overdue
	StateNever State = "never" // the schedule never fires again
)

// Status works out the job's state and the time its next ping is due by:
// the first scheduled run after the last ping (or creation) plus the grace period.
func Status(j Job, s *sched.Schedule, loc *time.Location, now time.Time) (State, time.Time) {
	base := j.CreatedAt
	if j.LastPing != nil {
		base = *j.LastPing
	}
	runs := s.Next(base, loc, 1)
	if len(runs) == 0 {
		return StateNever, time.Time{}
	}
	// When DST ends, cron and systemd run a fixed-time job once, so do not expect a
	// ping for the second pass through the repeated hour.
	// shortcut: looks at most 64 runs ahead, so every-second schedules may still alert in that hour.
	if runs[0].Again {
		for _, r := range s.Next(base, loc, 64) {
			if !r.Again {
				runs[0] = r
				break
			}
		}
	}
	due := runs[0].Time.Add(time.Duration(j.Grace) * time.Second)
	switch {
	case now.After(due):
		return StateLate, due
	case j.LastPing == nil:
		return StateNew, due
	}
	return StateUp, due
}
