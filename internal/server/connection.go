package server

import (
	"sync"
	"time"

	"teamx/internal/proto"
)

// ClientConn holds the state of a single connected session.
type ClientConn struct {
	SessionID  string
	DeviceID   string
	Hostname   string
	OS         string
	OSVersion  string
	ClientVersion string
	MacAddrs   []string
	IPAddrs    []string

	// Stream is set when the client opens its Channel. It is nil before the
	// Channel is established and after the stream closes.
	Stream proto.TeamX_ChannelServer

	// DisconnectCh is closed when an admin forces a kick. The Channel handler
	// selects on this channel; closing it signals the stream to exit cleanly.
	DisconnectCh chan struct{}

	LastHeartbeat time.Time
	Online        bool
	ConnectedAt   time.Time
}

// ConnectionManager is a thread-safe registry of connected sessions.
type ConnectionManager struct {
	mu       sync.RWMutex
	sessions map[string]*ClientConn // keyed by sessionID
	maxConns int                    // 0 = unlimited
}

// NewConnectionManager returns an initialized ConnectionManager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		sessions: make(map[string]*ClientConn),
	}
}

// SetMaxConns configures the connection limit. 0 means unlimited.
func (cm *ConnectionManager) SetMaxConns(n int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.maxConns = n
}

// Add inserts a newly registered session.
func (cm *ConnectionManager) Add(conn *ClientConn) {
	conn.DisconnectCh = make(chan struct{})
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.sessions[conn.SessionID] = conn
}

// Remove deletes a session from the manager.
func (cm *ConnectionManager) Remove(sessionID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.sessions, sessionID)
}

// Get returns the session by ID, or nil if not found.
func (cm *ConnectionManager) Get(sessionID string) *ClientConn {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.sessions[sessionID]
}

// Count returns the number of connected sessions.
func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.sessions)
}

// IsFull returns true when maxConns > 0 and the current count has reached it.
func (cm *ConnectionManager) IsFull() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.maxConns > 0 && len(cm.sessions) >= cm.maxConns
}

// Kick closes the DisconnectCh of the given session, which signals the Channel
// handler to return (and the stream to close). No-op if not found or offline.
func (cm *ConnectionManager) Kick(sessionID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.sessions[sessionID]
	if !ok || !conn.Online {
		return
	}
	select {
	case <-conn.DisconnectCh:
		// already closed
	default:
		close(conn.DisconnectCh)
	}
}

// RecordHeartbeat updates the last-heartbeat timestamp and marks the session
// online.
func (cm *ConnectionManager) RecordHeartbeat(sessionID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.sessions[sessionID]
	if !ok {
		return
	}
	conn.LastHeartbeat = time.Now()
	conn.Online = true
}

// MarkOffline sets a session's online status to false.
func (cm *ConnectionManager) MarkOffline(sessionID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.sessions[sessionID]
	if !ok {
		return
	}
	conn.Online = false
}

// SetStream binds a Channel stream to a session and marks it online.
func (cm *ConnectionManager) SetStream(sessionID string, stream proto.TeamX_ChannelServer) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.sessions[sessionID]
	if !ok {
		return
	}
	conn.Stream = stream
	conn.Online = true
	conn.LastHeartbeat = time.Now()
}

// ClearStream removes the stream reference when the Channel closes.
func (cm *ConnectionManager) ClearStream(sessionID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.sessions[sessionID]
	if !ok {
		return
	}
	conn.Stream = nil
	conn.Online = false
}

// HeartbeatChecker periodically scans sessions and marks them offline if their
// last heartbeat exceeds timeout. If onOffline is non-nil, it is called with
// each newly-offline session ID (useful for persisting status to a store).
// Run this in a goroutine.
func (cm *ConnectionManager) HeartbeatChecker(interval, timeout time.Duration, onOffline func(sessionID string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		cm.mu.Lock()
		for id, conn := range cm.sessions {
			if conn.Online && now.Sub(conn.LastHeartbeat) > timeout {
				conn.Online = false
				if onOffline != nil {
					onOffline(id)
				}
			}
		}
		cm.mu.Unlock()
	}
}
