package server

import (
	"sync"
	"time"

	"teamx/internal/proto"
)

// ClientConn holds the state of a single connected client.
type ClientConn struct {
	ID            string
	Hostname      string
	OS            string
	OSVersion     string
	ClientVersion string
	MacAddrs      []string
	IPAddrs       []string

	// Stream is set when the client opens its Channel. It is nil before the
	// Channel is established and after the stream closes.
	Stream proto.TeamX_ChannelServer

	LastHeartbeat time.Time
	Online        bool
	ConnectedAt   time.Time
}

// ConnectionManager is a thread-safe registry of connected clients.
type ConnectionManager struct {
	mu    sync.RWMutex
	conns map[string]*ClientConn
}

// NewConnectionManager returns an initialized ConnectionManager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		conns: make(map[string]*ClientConn),
	}
}

// Add inserts a newly registered client. If a client with the same ID already
// exists it is overwritten.
func (cm *ConnectionManager) Add(conn *ClientConn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.conns[conn.ID] = conn
}

// Remove deletes a client from the manager.
func (cm *ConnectionManager) Remove(clientID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.conns, clientID)
}

// Get returns the client by ID, or nil if not found.
func (cm *ConnectionManager) Get(clientID string) *ClientConn {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.conns[clientID]
}

// List returns a snapshot of all client IDs.
func (cm *ConnectionManager) List() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	ids := make([]string, 0, len(cm.conns))
	for id := range cm.conns {
		ids = append(ids, id)
	}
	return ids
}

// Count returns the number of connected clients.
func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.conns)
}

// RecordHeartbeat updates the last-heartbeat timestamp and marks the client
// online.
func (cm *ConnectionManager) RecordHeartbeat(clientID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.conns[clientID]
	if !ok {
		return
	}
	conn.LastHeartbeat = time.Now()
	conn.Online = true
}

// MarkOffline sets a client's online status to false.
func (cm *ConnectionManager) MarkOffline(clientID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.conns[clientID]
	if !ok {
		return
	}
	conn.Online = false
}

// SetStream binds a Channel stream to a client and marks it online.
func (cm *ConnectionManager) SetStream(clientID string, stream proto.TeamX_ChannelServer) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.conns[clientID]
	if !ok {
		return
	}
	conn.Stream = stream
	conn.Online = true
	conn.LastHeartbeat = time.Now()
}

// ClearStream removes the stream reference when the Channel closes.
func (cm *ConnectionManager) ClearStream(clientID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	conn, ok := cm.conns[clientID]
	if !ok {
		return
	}
	conn.Stream = nil
	conn.Online = false
}

// HeartbeatChecker periodically scans connections and marks clients offline if
// their last heartbeat exceeds timeout. Run this in a goroutine.
func (cm *ConnectionManager) HeartbeatChecker(interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		cm.mu.Lock()
		for id, conn := range cm.conns {
			if conn.Online && now.Sub(conn.LastHeartbeat) > timeout {
				conn.Online = false
				// TODO: log the offline event
				_ = id // placeholder — will log in Phase 3
			}
		}
		cm.mu.Unlock()
	}
}
