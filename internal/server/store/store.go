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

	// UpsertTerminal inserts a new session or updates an existing one (except
	// first_seen_at, which is preserved). Called on client Register.
	UpsertTerminal(sessionID, deviceID, hostname, os, osVersion, kernelVersion, clientVersion string,
		macAddrs, ipAddrs []string) error

	// UpdateHeartbeat bumps last_heartbeat and sets online=1 for a session.
	UpdateHeartbeat(sessionID string) error

	// MarkOffline sets online=0 for a session.
	MarkOffline(sessionID string) error

	// GetTerminal returns a single terminal row by session_id, or sql.ErrNoRows.
	GetTerminal(sessionID string) (*Terminal, error)

	// GetTerminalByDevice returns the latest terminal row for a device, or sql.ErrNoRows.
	GetTerminalByDevice(deviceID string) (*Terminal, error)

	// ListTerminals returns terminals ordered by last_seen_at DESC. Pass nil
	// for online to return all; pass offset/limit for pagination. Also returns
	// the total count (before offset/limit).
	// Each device appears only once (the latest session).
	ListTerminals(online *bool, offset, limit int) ([]*Terminal, int, error)

	// MarkBlocked sets blocked=1 for all sessions of the given device.
	MarkBlocked(deviceID string) error

	// UnblockTerminal clears blocked flag for all sessions of the device.
	UnblockTerminal(deviceID string) error

	// IsDeviceBlocked returns true when any session of the device has blocked=1.
	IsDeviceBlocked(deviceID string) (bool, error)

	// ---- Hardware -----------------------------------------------------------

	// SaveHardwareReport persists a hardware report and its sub-entities
	// (disks, nets, BIOS, motherboard) in a single transaction.
	SaveHardwareReport(sessionID, deviceID string, report *proto.ReportRequest) error

	// GetLatestHardware returns the most recent hardware report for a device,
	// or nil if none exists.
	GetLatestHardware(deviceID string) (*proto.HardwareInfo, error)

	// ListHardwareReports returns hardware snapshots for a device within a time
	// range (inclusive). since/until are RFC3339 strings. Pass empty string
	// for unbounded. limit caps the row count; 0 means default (100).
	ListHardwareReports(deviceID string, since, until string, limit int) ([]*HardwareSnapshot, error)

	// ---- Command Logs (Phase 5) ------------------------------------------------

	// SaveCommandLog inserts a new command log row with status=Pending.
	SaveCommandLog(commandID, sessionID, deviceID string, cmdType proto.CommandType,
		params map[string]string) error

	// UpdateCommandResult updates the command result fields after execution.
	UpdateCommandResult(commandID string, status string, exitCode int32,
		stdout, stderr, errorMsg, startedAt, finishedAt string) error

	// UpdateCommandStatus updates only the status field (for Sent, Executing transitions).
	UpdateCommandStatus(commandID, status string) error

	// MarkCommandTimeout sets status=Timeout only if the current status is still
	// non-terminal (Pending, Sent, Executing). Returns the number of rows affected
	// (1 = marked timeout, 0 = already terminal — no-op).
	MarkCommandTimeout(commandID string) error

	// GetCommandLog returns command log entries for a device or session.
	// deviceID takes precedence over sessionID. limit caps results; 0 means default (50).
	GetCommandLog(deviceID, sessionID string, limit int) ([]*CommandLogEntry, error)
}

// Terminal is the query result for a single terminal row.
type Terminal struct {
	SessionID     string
	DeviceID      string
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

// HardwareSnapshot pairs a HardwareInfo proto with its report metadata.
type HardwareSnapshot struct {
	ReportID  string
	CreatedAt string
	Info      *proto.HardwareInfo
}

// CommandLogEntry is the query result for a single command log row.
type CommandLogEntry struct {
	CommandID    string
	SessionID    string
	DeviceID     string
	Type         proto.CommandType
	Params       map[string]string
	Status       string
	ExitCode     int32
	Stdout       string
	Stderr       string
	ErrorMessage string
	CreatedAt    string
	StartedAt    string
	FinishedAt   string
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

	// Allow 4 open conns: WAL-mode SQLite handles concurrent reads + writes
	// safely, and we need more than one when a query result is still being
	// iterated and sub-queries need a fresh connection.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
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
