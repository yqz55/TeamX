package store

import (
	"database/sql"
	"fmt"
	"time"
)

// ---- Terminal CRUD ---------------------------------------------------------

// UpsertTerminal inserts a new session or updates every field except
// first_seen_at (which is preserved on conflict). MacAddrs and IPAddrs are
// stored as JSON arrays.
func (s *sqliteStore) UpsertTerminal(sessionID, deviceID, hostname, os, osVersion, kernelVersion, clientVersion string,
	macAddrs, ipAddrs []string) error {

	now := time.Now().UTC().Format(time.RFC3339)

	const query = `
INSERT INTO terminals (session_id, device_id, hostname, os, os_version, kernel_version, client_version,
                       mac_addrs, ip_addrs, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    device_id       = excluded.device_id,
    hostname        = excluded.hostname,
    os              = excluded.os,
    os_version      = excluded.os_version,
    kernel_version  = excluded.kernel_version,
    client_version  = excluded.client_version,
    mac_addrs       = excluded.mac_addrs,
    ip_addrs        = excluded.ip_addrs,
    last_seen_at    = excluded.last_seen_at
`
	_, err := s.db.Exec(query,
		sessionID, deviceID, hostname, os, osVersion, kernelVersion, clientVersion,
		marshalJSONArray(macAddrs), marshalJSONArray(ipAddrs),
		now, now,
	)
	if err != nil {
		return fmt.Errorf("store: upsert terminal %s: %w", sessionID, err)
	}
	return nil
}

// UpdateHeartbeat updates last_heartbeat and sets online=1 for the given session.
func (s *sqliteStore) UpdateHeartbeat(sessionID string) error {
	const query = `UPDATE terminals SET last_heartbeat = ?, online = 1 WHERE session_id = ?`
	_, err := s.db.Exec(query, nowUTC(), sessionID)
	if err != nil {
		return fmt.Errorf("store: update heartbeat %s: %w", sessionID, err)
	}
	return nil
}

// MarkOffline sets online=0 for the given session.
func (s *sqliteStore) MarkOffline(sessionID string) error {
	const query = `UPDATE terminals SET online = 0 WHERE session_id = ?`
	_, err := s.db.Exec(query, sessionID)
	if err != nil {
		return fmt.Errorf("store: mark offline %s: %w", sessionID, err)
	}
	return nil
}

// GetTerminal returns a single session, or sql.ErrNoRows if not found.
func (s *sqliteStore) GetTerminal(sessionID string) (*Terminal, error) {
	const query = `
SELECT session_id, device_id, hostname, os, os_version, kernel_version, client_version,
       online, blocked, last_heartbeat, last_seen_at, first_seen_at
FROM terminals WHERE session_id = ?`

	return s.scanTerminal(s.db.QueryRow(query, sessionID))
}

// GetTerminalByDevice returns the latest session for a device.
func (s *sqliteStore) GetTerminalByDevice(deviceID string) (*Terminal, error) {
	const query = `
SELECT session_id, device_id, hostname, os, os_version, kernel_version, client_version,
       online, blocked, last_heartbeat, last_seen_at, first_seen_at
FROM terminals WHERE device_id = ? ORDER BY last_seen_at DESC LIMIT 1`

	return s.scanTerminal(s.db.QueryRow(query, deviceID))
}

func (s *sqliteStore) scanTerminal(row *sql.Row) (*Terminal, error) {
	t := &Terminal{}
	var lastHB, lastSeen, firstSeen sql.NullString
	err := row.Scan(
		&t.SessionID, &t.DeviceID, &t.Hostname, &t.OS, &t.OSVersion, &t.KernelVersion, &t.ClientVersion,
		&t.Online, &t.Blocked, &lastHB, &lastSeen, &firstSeen,
	)
	if err != nil {
		return nil, err
	}
	t.LastHeartbeat = nullStr(lastHB)
	t.LastSeenAt = nullStr(lastSeen)
	t.FirstSeenAt = nullStr(firstSeen)
	return t, nil
}

// ListTerminals returns the latest session per device ordered by last_seen_at
// DESC with optional online filter and pagination.
func (s *sqliteStore) ListTerminals(online *bool, offset, limit int) ([]*Terminal, int, error) {
	// Use a subquery to get the latest session per device.
	latestExpr := `
SELECT session_id, device_id, hostname, os, os_version, kernel_version, client_version,
       online, blocked, last_heartbeat, last_seen_at, first_seen_at,
       ROW_NUMBER() OVER (PARTITION BY device_id ORDER BY last_seen_at DESC) AS rn
FROM terminals`

	var where string
	var args []interface{}

	if online != nil {
		where = "WHERE online = ?"
		if *online {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	// Total: count unique devices.
	countSQL := "SELECT COUNT(*) FROM (SELECT 1 FROM (" + latestExpr + ") WHERE rn = 1"
	if where != "" {
		countSQL += " " + where
	}
	countSQL += ")"
	var total int
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count terminals: %w", err)
	}

	// Paginated query.
	queryStr := "SELECT session_id, device_id, hostname, os, os_version, kernel_version, client_version, online, blocked, last_heartbeat, last_seen_at, first_seen_at FROM (" + latestExpr + ") WHERE rn = 1"
	if where != "" {
		queryStr += " " + where
	}
	queryStr += " ORDER BY last_seen_at DESC LIMIT ? OFFSET ?"

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	args = append(args, limit, offset)

	rows, err := s.db.Query(queryStr, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list terminals: %w", err)
	}
	defer rows.Close()

	var terminals []*Terminal
	for rows.Next() {
		t := &Terminal{}
		var lastHB, lastSeen, firstSeen sql.NullString
		if err := rows.Scan(
			&t.SessionID, &t.DeviceID, &t.Hostname, &t.OS, &t.OSVersion, &t.KernelVersion, &t.ClientVersion,
			&t.Online, &t.Blocked, &lastHB, &lastSeen, &firstSeen,
		); err != nil {
			return nil, 0, fmt.Errorf("store: scan terminal: %w", err)
		}
		t.LastHeartbeat = nullStr(lastHB)
		t.LastSeenAt = nullStr(lastSeen)
		t.FirstSeenAt = nullStr(firstSeen)
		terminals = append(terminals, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: iterate terminals: %w", err)
	}

	if terminals == nil {
		terminals = []*Terminal{}
	}
	return terminals, total, nil
}

// ---- Blocklist ---------------------------------------------------------------

// MarkBlocked sets blocked=1 on all sessions of the given device.
func (s *sqliteStore) MarkBlocked(deviceID string) error {
	const query = `UPDATE terminals SET blocked = 1 WHERE device_id = ?`
	if _, err := s.db.Exec(query, deviceID); err != nil {
		return fmt.Errorf("store: block device %s: %w", deviceID, err)
	}
	return nil
}

// UnblockTerminal clears the blocked flag on all sessions of the device.
func (s *sqliteStore) UnblockTerminal(deviceID string) error {
	const query = `UPDATE terminals SET blocked = 0 WHERE device_id = ?`
	_, err := s.db.Exec(query, deviceID)
	if err != nil {
		return fmt.Errorf("store: unblock device %s: %w", deviceID, err)
	}
	return nil
}

// IsDeviceBlocked returns true if any session of the device has blocked=1.
func (s *sqliteStore) IsDeviceBlocked(deviceID string) (bool, error) {
	const query = `SELECT COUNT(*) FROM terminals WHERE device_id = ? AND blocked = 1`
	var n int
	if err := s.db.QueryRow(query, deviceID).Scan(&n); err != nil {
		return false, fmt.Errorf("store: check blocked device %s: %w", deviceID, err)
	}
	return n > 0, nil
}

// ---- helpers ----------------------------------------------------------------

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
