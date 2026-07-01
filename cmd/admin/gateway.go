package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	connect "connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"teamx/internal/proto"
	"teamx/internal/proto/protoconnect"
)

// Gateway proxies Connect HTTP requests to the gRPC backend and broadcasts
// terminal online/offline changes over WebSocket.
type Gateway struct {
	protoconnect.UnimplementedTeamXHandler
	grpcConn *grpc.ClientConn
	wsHub    *wsHub
}

// GatewayConfig holds the knobs for creating a Gateway.
type GatewayConfig struct {
	GRPCAddr     string
	PollInterval time.Duration
}

// NewGateway dials the gRPC backend and starts the WebSocket poller.
func NewGateway(cfg GatewayConfig) (*Gateway, error) {
	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	g := &Gateway{grpcConn: conn}
	g.wsHub = newWSHub(g, cfg.PollInterval)
	return g, nil
}

// handler builds the full HTTP handler stack: Connect routes + WebSocket + CORS.
func (g *Gateway) handler(corsOrigin string) http.Handler {
	mux := http.NewServeMux()

	// ---- ConnectRPC handler (6 admin RPCs) ---------------------------------
	path, connectHandler := protoconnect.NewTeamXHandler(g)
	mux.Handle(path, connectHandler)

	// ---- WebSocket ---------------------------------------------------------
	mux.HandleFunc("/ws", g.wsHub.serveWS)

	// ---- CORS wrap ---------------------------------------------------------
	return corsMiddleware(mux, corsOrigin)
}

// Close shuts down the gateway: WebSocket poller first, then the gRPC connection.
func (g *Gateway) Close() error {
	g.wsHub.stop()
	return g.grpcConn.Close()
}

// ---- RPC proxy methods -------------------------------------------------------
// Each method creates a gRPC client, calls the backend, and returns the result.

func (g *Gateway) ListTerminals(ctx context.Context, req *connect.Request[proto.ListTerminalsRequest]) (*connect.Response[proto.ListTerminalsResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).ListTerminals(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) GetTerminal(ctx context.Context, req *connect.Request[proto.GetTerminalRequest]) (*connect.Response[proto.GetTerminalResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).GetTerminal(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) GetTerminalHistory(ctx context.Context, req *connect.Request[proto.GetTerminalHistoryRequest]) (*connect.Response[proto.GetTerminalHistoryResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).GetTerminalHistory(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) DisconnectTerminal(ctx context.Context, req *connect.Request[proto.DisconnectTerminalRequest]) (*connect.Response[proto.DisconnectTerminalResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).DisconnectTerminal(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) BlockTerminal(ctx context.Context, req *connect.Request[proto.BlockTerminalRequest]) (*connect.Response[proto.BlockTerminalResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).BlockTerminal(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) UnblockTerminal(ctx context.Context, req *connect.Request[proto.UnblockTerminalRequest]) (*connect.Response[proto.UnblockTerminalResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).UnblockTerminal(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) SendCommand(ctx context.Context, req *connect.Request[proto.SendCommandRequest]) (*connect.Response[proto.SendCommandResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).SendCommand(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (g *Gateway) GetCommandLog(ctx context.Context, req *connect.Request[proto.GetCommandLogRequest]) (*connect.Response[proto.GetCommandLogResponse], error) {
	resp, err := proto.NewTeamXClient(g.grpcConn).GetCommandLog(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

// =============================================================================
// WebSocket Hub
// =============================================================================

// onlineSnapshot maps session_id → online status at the last poll.
type onlineSnapshot map[string]bool

// wsEvent is broadcast to WebSocket clients on state change.
type wsEvent struct {
	Type      string `json:"type"`      // "online" or "offline"
	SessionID string `json:"session_id"`
	Hostname  string `json:"hostname"`
	Timestamp string `json:"timestamp"`
}

type wsHub struct {
	gateway      *Gateway
	pollInterval time.Duration
	mu           sync.Mutex
	conns        map[*wsConn]struct{}
	last         onlineSnapshot
	stopCh       chan struct{}
}

type wsConn struct {
	conn *websocket.Conn
	ctx  context.Context
}

func newWSHub(g *Gateway, pollInterval time.Duration) *wsHub {
	h := &wsHub{
		gateway:      g,
		pollInterval: pollInterval,
		conns:        make(map[*wsConn]struct{}),
		last:         make(onlineSnapshot),
		stopCh:       make(chan struct{}),
	}
	go h.pollLoop()
	return h
}

func (h *wsHub) stop() {
	close(h.stopCh)
}

// pollLoop periodically calls ListTerminals and broadcasts state changes.
func (h *wsHub) pollLoop() {
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	// Do an immediate poll to seed the baseline (no broadcast).
	h.poll(false)

	for {
		select {
		case <-ticker.C:
			h.poll(true)
		case <-h.stopCh:
			return
		}
	}
}

func (h *wsHub) poll(broadcast bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := proto.NewTeamXClient(h.gateway.grpcConn).ListTerminals(ctx, &proto.ListTerminalsRequest{})
	if err != nil {
		log.Printf("[ws] poll failed: %v", err)
		return
	}

	current := make(onlineSnapshot)
	for _, t := range resp.Terminals {
		current[t.SessionId] = t.Online
	}

	if !broadcast {
		h.last = current
		return
	}

	// Detect changes.
	var events []wsEvent
	now := time.Now().UTC().Format(time.RFC3339)

	for id, online := range current {
		was, known := h.last[id]
		if known && was != online {
			evt := wsEvent{SessionID: id, Timestamp: now}
			if online {
				evt.Type = "online"
				evt.Hostname = h.findHostname(resp.Terminals, id)
			} else {
				evt.Type = "offline"
				evt.Hostname = h.findHostname(resp.Terminals, id)
			}
			events = append(events, evt)
		}
	}

	h.last = current

	if len(events) > 0 {
		h.broadcast(events)
	}
}

func (h *wsHub) findHostname(terminals []*proto.TerminalSummary, sessionID string) string {
	for _, t := range terminals {
		if t.SessionId == sessionID {
			return t.Hostname
		}
	}
	return ""
}

func (h *wsHub) broadcast(events []wsEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for wc := range h.conns {
		for _, evt := range events {
			if err := wsjson.Write(wc.ctx, wc.conn, evt); err != nil {
				log.Printf("[ws] write error: %v", err)
				wc.conn.Close(websocket.StatusInternalError, "write error")
				delete(h.conns, wc)
				break
			}
		}
	}
}

// serveWS handles a new WebSocket connection.
func (h *wsHub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		log.Printf("[ws] accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	wc := &wsConn{conn: conn, ctx: r.Context()}

	h.mu.Lock()
	h.conns[wc] = struct{}{}
	h.mu.Unlock()

	// Block until the client disconnects.
	<-r.Context().Done()

	h.mu.Lock()
	delete(h.conns, wc)
	h.mu.Unlock()
}

// =============================================================================
// CORS Middleware
// =============================================================================

func corsMiddleware(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Connect-Protocol-Version, X-User-Agent")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
