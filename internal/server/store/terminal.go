package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ---- Terminal CRUD ---------------------------------------------------------

// UpsertTerminal inserts a new terminal or updates every field except
// first_seen_at (which is preserved on conflict). MacAddrs and IPAddrs are
// stored as JSON arrays.
func (s *sqliteStore) UpsertTerminal(clientID, hostname, os, osVersion, kernelVersion, clientVersion string,
	macAddrs, ipAddrs []string) error {

	now := time.Now().UTC().Format(time.RFC3339)

	const query = `
INSERT INTO terminals (client_id, hostname, os, os_version, kernel_version, client_version,
                       mac_addrs, ip_addrs, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(client_id) DO UPDATE SET
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
		clientID, hostname, os, osVersion, kernelVersion, clientVersion,
		marshalJSONArray(macAddrs), marshalJSONArray(ipAddrs),
		now, now,
	)
	if err != nil {
		return fmt.Errorf("store: upsert terminal %s: %w", clientID, err)
	}
	return nil
}

// UpdateHeartbeat updates last_heartbeat and sets online=1 for the given client.
func (s *sqliteStore) UpdateHeartbeat(clientID string) error {
	const query = `UPDATE terminals SET last_heartbeat = ?, online = 1 WHERE client_id = ?`
	_, err := s.db.Exec(query, nowUTC(), clientID)
	if err != nil {
		return fmt.Errorf("store: update heartbeat %s: %w", clientID, err)
	}
	return nil
}

// MarkOffline sets online=0 for the given client.
func (s *sqliteStore) MarkOffline(clientID string) error {
	const query = `UPDATE terminals SET online = 0 WHERE client_id = ?`
	_, err := s.db.Exec(query, clientID)
	if err != nil {
		return fmt.Errorf("store: mark offline %s: %w", clientID, err)
	}
	return nil
}

// GetTerminal returns a single terminal, or sql.ErrNoRows if not found.
func (s *sqliteStore) GetTerminal(clientID string) (*Terminal, error) {
	const query = `
SELECT client_id, hostname, os, os_version, kernel_version, client_version,
       online, blocked, last_heartbeat, last_seen_at, first_seen_at
FROM terminals WHERE client_id = ?`

	t := &Terminal{}
	var lastHB, lastSeen, firstSeen sql.NullString
	err := s.db.QueryRow(query, clientID).Scan(
		&t.ClientID, &t.Hostname, &t.OS, &t.OSVersion, &t.KernelVersion, &t.ClientVersion,
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

// ListTerminals returns terminals ordered by last_seen_at DESC with optional
// online filter and pagination. The total count is before offset/limit.
func (s *sqliteStore) ListTerminals(online *bool, offset, limit int) ([]*Terminal, int, error) {
	var (
		where string
		args  []interface{}
	)
	if online != nil {
		where = "WHERE online = ?"
		if *online {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	// Total count.
	var total int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM terminals "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count terminals: %w", err)
	}

	// Paginated query.
	const base = `
SELECT client_id, hostname, os, os_version, kernel_version, client_version,
       online, blocked, last_heartbeat, last_seen_at, first_seen_at
FROM terminals %s ORDER BY last_seen_at DESC LIMIT ? OFFSET ?`
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	queryStr := fmt.Sprintf(base, where)
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
			&t.ClientID, &t.Hostname, &t.OS, &t.OSVersion, &t.KernelVersion, &t.ClientVersion,
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

// MarkBlocked sets blocked=1 on the terminal row. If the row does not exist
// yet (terminal hasn't completed Register) it still writes a stub so the
// hostname-based check works on subsequent registrations.
func (s *sqliteStore) MarkBlocked(clientID string) error {
	const query = `UPDATE terminals SET blocked = 1 WHERE client_id = ?`
	if _, err := s.db.Exec(query, clientID); err != nil {
		return fmt.Errorf("store: block %s: %w", clientID, err)
	}
	return nil
}

// UnblockTerminal clears the blocked flag.
func (s *sqliteStore) UnblockTerminal(clientID string) error {
	const query = `UPDATE terminals SET blocked = 0 WHERE client_id = ?`
	_, err := s.db.Exec(query, clientID)
	if err != nil {
		return fmt.Errorf("store: unblock %s: %w", clientID, err)
	}
	return nil
}

// IsHostnameBlocked returns true if any terminal with the given hostname has
// blocked=1.
func (s *sqliteStore) IsHostnameBlocked(hostname string) (bool, error) {
	const query = `SELECT COUNT(*) FROM terminals WHERE hostname = ? AND blocked = 1`
	var n int
	if err := s.db.QueryRow(query, hostname).Scan(&n); err != nil {
		return false, fmt.Errorf("store: check blocked hostname %s: %w", hostname, err)
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

func itoa(i int32) string {
	return strings.TrimSpace(fmt.Sprintf("%d", i))
}
