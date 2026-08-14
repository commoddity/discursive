// Package vision cache: durable content-hash keyed image-description store.
// The in-memory cache only survives a single process and its short TTL is too
// small to span the vision provider's multi-hour rate-limit window. This store
// persists descriptions so a historical image (resent by Cursor on every turn)
// never re-invokes the vision model once it has been described.
package vision

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// descCache persists image hash -> description mappings in SQLite.
// Implementations must be safe for concurrent use.
type descCache interface {
	Get(hash string) (string, bool)
	Put(hash, desc string) error
}

// sqliteDescCache is a durable content-hash description cache.
type sqliteDescCache struct {
	db *sql.DB
}

// newSQLiteDescCache opens (creating if needed) the vision cache database in
// dir. dir is the usage data directory (e.g. {dataRoot}/usage).
func newSQLiteDescCache(dir string) (*sqliteDescCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("vision cache: create dir: %w", err)
	}
	dbPath := filepath.Join(dir, "vision_cache.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("vision cache: open db: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS descriptions (
		hash        TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		created_at  TEXT NOT NULL
	); CREATE INDEX IF NOT EXISTS idx_descriptions_created ON descriptions(created_at);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vision cache: init schema: %w", err)
	}
	return &sqliteDescCache{db: db}, nil
}

// NewPersistentCache opens (creating if needed) the durable vision description
// cache in dir, which is expected to be the usage data directory (e.g.
// {dataRoot}/usage). The caller is responsible for calling Close.
func NewPersistentCache(dir string) (*sqliteDescCache, error) {
	return newSQLiteDescCache(dir)
}

// Close closes the underlying database.
func (c *sqliteDescCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Get returns the cached description for hash, if present.
func (c *sqliteDescCache) Get(hash string) (string, bool) {
	if c == nil || c.db == nil {
		return "", false
	}
	var desc string
	err := c.db.QueryRow(`SELECT description FROM descriptions WHERE hash = ?`, hash).Scan(&desc)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return desc, true
}

// Put stores a description keyed by hash.
func (c *sqliteDescCache) Put(hash, desc string) error {
	if c == nil || c.db == nil {
		return nil
	}
	_, err := c.db.Exec(
		`INSERT INTO descriptions (hash, description, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(hash) DO UPDATE SET description = excluded.description`,
		hash, desc, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}
