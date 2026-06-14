package store

import (
	"database/sql"
	"fmt"
	"time"

	"teamx/internal/proto"

	_ "modernc.org/sqlite"
)

// Store is the persistence abstraction for the TeamX server.
// The current implementation uses SQLite; swap the constructor to migrate to
// another engine without changing callers.
type Store interface {
	Close() error

	// ---- Terminal -----------------------------------------------------------

	// UpsertTerminal inserts a new terminal or updates an existing one (except
	// first_seen_at, which is preserved). Called on client Register.
	UpsertTerminal(clientID, hostname, os, osVersion, kernelVersion, clientVersion string,
		macAddrs, ipAddrs []string) error

	// UpdateHeartbeat bumps last_heartbeat and sets online=1.
	UpdateHeartbeat(clientID string) error

	// MarkOffline sets online=0.
	MarkOffline(clientID string) error

	// GetTerminal returns a single terminal by client_id, or sql.ErrNoRows.
	GetTerminal(clientID string) (*Terminal, error)

	// ListTerminals returns terminals ordered by last_seen_at DESC. Pass nil
	// for online to return all; pass offset/limit for pagination. Also returns
	// the total count (before offset/limit).
	ListTerminals(online *bool, offset, limit int) ([]*Terminal, int, error)

	// ---- Hardware -----------------------------------------------------------

	// SaveHardwareReport persists a hardware report and its sub-entities
	// (disks, nets, BIOS, motherboard) in a single transaction.
	SaveHardwareReport(clientID string, report *proto.ReportRequest) error

	// GetLatestHardware returns the most recent hardware report for a client,
	// or nil if none exists.
	GetLatestHardware(clientID string) (*proto.HardwareInfo, error)

	// ListHardwareReports returns hardware reports for a client within a time
	// range (inclusive). since/until are RFC3339 strings. Pass empty string
	// for unbounded. limit caps the row count; 0 means default (100).
	ListHardwareReports(clientID string, since, until string, limit int) ([]*proto.HardwareInfo, error)
}

// Terminal is the query result for a single terminal row.
type Terminal struct {
	ClientID      string
	Hostname      string
	OS            string
	OSVersion     string
	KernelVersion string
	ClientVersion string
	Online        bool
	Blocked       bool
	LastHeartbeat string
	LastSeenAt    string
	FirstSeenAt   string
}

// ---- SQLite implementation ------------------------------------------------

// sqliteStore implements Store backed by modernc.org/sqlite.
type sqliteStore struct {
	db *sql.DB
}

// OpenSQLite opens (or creates) the SQLite database at path, enables WAL mode,
// sets safe pragmas, and runs migrations. Returns a Store ready for use.
func OpenSQLite(path string) (Store, error) {
	// Use journal_mode=WAL pragma via DSN query parameter so it takes effect
	// before any other pragmas.
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000", path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// SQLite serialises writes; one open conn avoids "database is locked" races
	// while still allowing concurrent reads in WAL mode.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // SQLite connections are not meant to be recycled

	// Verify the connection is alive.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	// Run migrations.
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

// Close implements Store.
func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// ---- helpers ----------------------------------------------------------------

// nowUTC returns the current time as an RFC 3339 string (UTC).
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// marshalJSONArray returns a compact JSON array of strings, e.g. ["a","b"].
// An empty slice returns [].
func marshalJSONArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	b := make([]byte, 0, len(items)*16)
	b = append(b, '[')
	for i, s := range items {
		if i > 0 {
			b = append(b, ',')
		}
		// Escape minimal JSON: " and \ inside strings.
		b = append(b, '"')
		for _, ch := range s {
			if ch == '"' {
				b = append(b, '\\', '"')
			} else if ch == '\\' {
				b = append(b, '\\', '\\')
			} else {
				b = append(b, byte(ch))
			}
		}
		b = append(b, '"')
	}
	b = append(b, ']')
	return string(b)
}
