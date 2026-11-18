// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

// Package board is the HPK Board (docs/DESIGN.md §20): who is working on what,
// so two people or two agents never duplicate work without knowing. The data
// lives in <project>/board.db (SQLite, WAL); BOARD.md is a view rendered from it.
// Every check here is SQL and string work: 0 tokens.
package board

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure Go: no C toolchain, cross-compiles to every target

	"hpk.local/hpk/internal/project"
)

// DB is one project's board.
type DB struct {
	sql *sql.DB
	P   *project.Project
	Now func() time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS claims (
  id        TEXT PRIMARY KEY,
  task      TEXT NOT NULL,
  task_key  TEXT NOT NULL,
  lane      TEXT NOT NULL DEFAULT '',
  paths     TEXT NOT NULL DEFAULT '[]',
  wiki      TEXT NOT NULL DEFAULT '[]',
  owner     TEXT NOT NULL DEFAULT '',
  session   TEXT NOT NULL DEFAULT '',
  started   TEXT NOT NULL,
  heartbeat TEXT NOT NULL,
  status    TEXT NOT NULL,
  ended     TEXT
);
CREATE INDEX IF NOT EXISTS claims_status ON claims(status);
CREATE INDEX IF NOT EXISTS claims_session ON claims(session);
CREATE TABLE IF NOT EXISTS notices (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  at      TEXT NOT NULL,
  level   TEXT NOT NULL,
  kind    TEXT NOT NULL,
  owner   TEXT NOT NULL DEFAULT '',
  claim_a TEXT NOT NULL DEFAULT '',
  claim_b TEXT NOT NULL DEFAULT '',
  files   TEXT NOT NULL DEFAULT '[]',
  text    TEXT NOT NULL,
  read    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS asks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  at          TEXT NOT NULL,
  from_sess   TEXT NOT NULL,
  to_sess     TEXT NOT NULL,
  question    TEXT NOT NULL,
  status      TEXT NOT NULL,
  answer      TEXT NOT NULL DEFAULT '{}',
  answered_at TEXT
);
CREATE TABLE IF NOT EXISTS sessions (
  id   TEXT PRIMARY KEY,
  data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS seq (
  name TEXT PRIMARY KEY,
  n    INTEGER NOT NULL
);
`

// Open opens (and creates) <project>/board.db.
func Open(p *project.Project) (*DB, error) {
	path := filepath.Join(p.Dir, "board.db")
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(1) // one connection per process; WAL lets processes share the file
	if _, err := s.Exec(schema); err != nil {
		s.Close()
		return nil, fmt.Errorf("board.db: %w", err)
	}
	return &DB{sql: s, P: p, Now: time.Now}, nil
}

// Close releases the database.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the handle for packages that keep their own tables here (sessions).
func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) nextID(name, prefix string) (string, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO seq(name,n) VALUES(?,0) ON CONFLICT(name) DO NOTHING`, name); err != nil {
		return "", err
	}
	var n int
	if err := tx.QueryRow(`UPDATE seq SET n=n+1 WHERE name=? RETURNING n`, name).Scan(&n); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, n), nil
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
