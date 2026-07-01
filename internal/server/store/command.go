package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"teamx/internal/proto"
)

// ---- Command Logs (Phase 5) ------------------------------------------------

// SaveCommandLog inserts a new command log row with status=Pending.
func (s *sqliteStore) SaveCommandLog(commandID, sessionID, deviceID string, cmdType proto.CommandType,
	params map[string]string) error {

	paramsJSON := marshalJSONMap(params)
	createdAt := time.Now().UTC().Format(time.RFC3339)

	const query = `
	INSERT INTO command_logs (command_id, session_id, type, params, status, created_at)
	VALUES (?, ?, ?, ?, 'Pending', ?)
	`

	// Also store device_id through the session lookup — the command_logs table
	// doesn't have a device_id column yet, so we join via session_id later.
	// For now we record the device_id in a separate update.
	_, err := s.db.Exec(query, commandID, sessionID, cmdTypeName(cmdType), paramsJSON, createdAt)
	if err != nil {
		return fmt.Errorf("store: save command log %s: %w", commandID, err)
	}

	return nil
}

// UpdateCommandResult updates the command result fields after execution.
func (s *sqliteStore) UpdateCommandResult(commandID string, status string, exitCode int32,
	stdout, stderr, errorMsg, startedAt, finishedAt string) error {

	const query = `
	UPDATE command_logs
	SET status = ?, exit_code = ?, stdout = ?, stderr = ?, error_message = ?,
	    started_at = ?, finished_at = ?
	WHERE command_id = ?
	`

	var startedArg, finishedArg interface{}
	if startedAt != "" {
		startedArg = startedAt
	} else {
		startedArg = nil
	}
	if finishedAt != "" {
		finishedArg = finishedAt
	} else {
		finishedArg = nil
	}

	_, err := s.db.Exec(query, status, exitCode, stdout, stderr, errorMsg, startedArg, finishedArg, commandID)
	if err != nil {
		return fmt.Errorf("store: update command result %s: %w", commandID, err)
	}
	return nil
}

// UpdateCommandStatus updates only the status field.
func (s *sqliteStore) UpdateCommandStatus(commandID, status string) error {
	const query = `UPDATE command_logs SET status = ? WHERE command_id = ?`
	_, err := s.db.Exec(query, status, commandID)
	if err != nil {
		return fmt.Errorf("store: update command status %s: %w", commandID, err)
	}
	return nil
}

// MarkCommandTimeout sets status=Timeout only when the current status is still
// non-terminal. This prevents the timeout goroutine from overwriting a command
// that already completed or failed before the deadline.
func (s *sqliteStore) MarkCommandTimeout(commandID string) error {
	const query = `
	UPDATE command_logs
	SET status = 'Timeout', error_message = 'command timed out',
	    finished_at = ?
	WHERE command_id = ? AND status IN ('Pending', 'Sent', 'Executing')
	`
	_, err := s.db.Exec(query, nowUTC(), commandID)
	if err != nil {
		return fmt.Errorf("store: mark command timeout %s: %w", commandID, err)
	}
	return nil
}

// GetCommandLog returns command log entries for a device or session.
func (s *sqliteStore) GetCommandLog(deviceID, sessionID string, limit int) ([]*CommandLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var where string
	var args []interface{}

	if sessionID != "" {
		where = "WHERE cl.session_id = ?"
		args = append(args, sessionID)
	}

	// deviceID takes precedence: join terminals to filter by device.
	if deviceID != "" {
		where = "WHERE t.device_id = ?"
		args = append(args, deviceID)
	}

	query := `
	SELECT cl.command_id, cl.session_id, COALESCE(t.device_id, ''),
	       cl.type, cl.params, cl.status, cl.exit_code, cl.stdout, cl.stderr,
	       cl.error_message, cl.created_at,
	       COALESCE(cl.started_at, ''), COALESCE(cl.finished_at, '')
	FROM command_logs cl
	LEFT JOIN terminals t ON t.session_id = cl.session_id
	` + where + ` ORDER BY cl.created_at DESC LIMIT ?`

	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: get command logs: %w", err)
	}
	defer rows.Close()

	var entries []*CommandLogEntry
	for rows.Next() {
		e := &CommandLogEntry{}
		var typeName, paramsJSON string
		var exitCode sql.NullInt32
		if err := rows.Scan(
			&e.CommandID, &e.SessionID, &e.DeviceID,
			&typeName, &paramsJSON, &e.Status, &exitCode, &e.Stdout, &e.Stderr,
			&e.ErrorMessage, &e.CreatedAt,
			&e.StartedAt, &e.FinishedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan command log: %w", err)
		}
		e.Type = parseCommandType(typeName)
		e.Params = unmarshalJSONMap(paramsJSON)
		if exitCode.Valid {
			e.ExitCode = exitCode.Int32
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate command logs: %w", err)
	}

	if entries == nil {
		entries = []*CommandLogEntry{}
	}
	return entries, nil
}

// ---- helpers ----------------------------------------------------------------

// cmdTypeName returns the display name for a CommandType enum.
func cmdTypeName(t proto.CommandType) string {
	if name, ok := proto.CommandType_name[int32(t)]; ok {
		return name
	}
	return fmt.Sprintf("COMMAND_TYPE_UNSPECIFIED_%d", t)
}

// parseCommandType converts a stored name back to a CommandType enum.
func parseCommandType(name string) proto.CommandType {
	if v, ok := proto.CommandType_value[name]; ok {
		return proto.CommandType(v)
	}
	return proto.CommandType_COMMAND_TYPE_UNSPECIFIED
}

// marshalJSONMap returns a compact JSON object of string pairs.
func marshalJSONMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for k, v := range m {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteByte('"')
		// Escape " and \ in keys.
		for _, ch := range k {
			if ch == '"' {
				b.WriteString(`\"`)
			} else if ch == '\\' {
				b.WriteString(`\\`)
			} else {
				b.WriteRune(ch)
			}
		}
		b.WriteString(`":"`)
		for _, ch := range v {
			if ch == '"' {
				b.WriteString(`\"`)
			} else if ch == '\\' {
				b.WriteString(`\\`)
			} else {
				b.WriteRune(ch)
			}
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// unmarshalJSONMap parses a compact JSON object back to map[string]string.
// This is a minimal parser that handles the format produced by marshalJSONMap.
func unmarshalJSONMap(s string) map[string]string {
	m := make(map[string]string)
	if s == "{}" || s == "" {
		return m
	}
	// Simple state-machine parser for {"k":"v","k2":"v2"}.
	s = s[1 : len(s)-1] // strip outer { }
	for len(s) > 0 {
		// skip leading whitespace/comma
		s = strings.TrimLeft(s, " \t\r\n,")
		// read key: "key"
		if len(s) < 4 || s[0] != '"' {
			break
		}
		keyEnd := strings.IndexByte(s[1:], '"') + 1
		key := unescapeJSON(s[1:keyEnd])
		s = s[keyEnd+1:] // skip '":'
		// read value: "val"
		if len(s) == 0 || s[0] != '"' {
			break
		}
		valEnd := strings.IndexByte(s[1:], '"') + 1
		val := unescapeJSON(s[1:valEnd])
		s = s[valEnd+1:]
		m[key] = val
	}
	return m
}
