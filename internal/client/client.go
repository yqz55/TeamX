package client

import (
	"context"
	"io"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"teamx/internal/proto"
)

// Config holds client parameters.
type Config struct {
	ServerAddr        string
	HeartbeatInterval time.Duration
	ReconnectInitial  time.Duration
	ReconnectMax      time.Duration
	ClientVersion     string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ServerAddr:        "localhost:50051",
		HeartbeatInterval: 10 * time.Second,
		ReconnectInitial:  1 * time.Second,
		ReconnectMax:      60 * time.Second,
		ClientVersion:     "0.2.0",
	}
}

// Client is a TeamX terminal agent. It registers with the server, maintains a
// bidirectional Channel stream, sends periodic heartbeats, and auto-reconnects
// with exponential backoff when the connection is lost.
type Client struct {
	cfg  Config
	info SysInfo

	clientID string
	seq      uint64

	// Protects seq.
	mu sync.Mutex
}

// NewClient creates a Client with the given config.
func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, info: Collect()}
}

// SysInfo returns the client's system information snapshot.
func (c *Client) SysInfo() SysInfo {
	return c.info
}

// Run is the main client loop. It blocks until the context is cancelled.
func (c *Client) Run(ctx context.Context) error {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if attempt > 0 {
			delay := backoff(c.cfg.ReconnectInitial, c.cfg.ReconnectMax, attempt)
			log.Printf("[client] reconnecting in %v (attempt %d)", delay, attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		if err := c.connect(ctx); err != nil {
			log.Printf("[client] connection failed: %v", err)
			attempt++
			continue
		}

		// Connected successfully; reset attempt counter.
		attempt = 0
		log.Printf("[client] disconnected — will retry")
	}
}

// connect performs one full connect → register → channel session. It blocks
// until the session ends (stream error, context cancel, etc.).
func (c *Client) connect(parentCtx context.Context) error {
	// Dial the server with plain TCP (no TLS for Phase 1).
	conn, err := grpc.NewClient(c.cfg.ServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	teamx := proto.NewTeamXClient(conn)

	// ---- Register ----------------------------------------------------------

	regResp, err := teamx.Register(parentCtx, &proto.HandshakeRequest{
		Hostname:      c.info.Hostname,
		Os:            c.info.OS,
		OsVersion:     c.info.OSVersion,
		KernelVersion: c.info.KernelVersion,
		ClientVersion: c.cfg.ClientVersion,
		MacAddrs:      c.info.MacAddrs,
		IpAddrs:       c.info.IPAddrs,
	})
	if err != nil {
		return err
	}
	c.clientID = regResp.GetClientId()
	log.Printf("[client] registered: id=%s server_time=%s", c.clientID, regResp.GetServerTime())

	// ---- Open Channel ------------------------------------------------------

	// Attach client_id via gRPC metadata so the server can bind the stream.
	md := metadata.Pairs("client-id", c.clientID)
	channelCtx := metadata.NewOutgoingContext(parentCtx, md)

	stream, err := teamx.Channel(channelCtx)
	if err != nil {
		return err
	}
	log.Printf("[client] channel opened")

	// Session-scoped context: cancelled when the stream breaks, which
	// signals the heartbeat goroutine to stop.
	sessCtx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// ---- Heartbeat goroutine ------------------------------------------------

	hbDone := make(chan struct{})
	go c.heartbeatLoop(sessCtx, stream, hbDone)

	// ---- Recv loop ----------------------------------------------------------

	err = c.recvLoop(sessCtx, stream)

	// Stream broken — cancel heartbeat and wait for it to exit.
	cancel()
	<-hbDone
	return err
}

// ---- heartbeat ------------------------------------------------------------

func (c *Client) heartbeatLoop(ctx context.Context, stream proto.TeamX_ChannelClient, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := &proto.ClientMessage{
				Seq: c.nextSeq(),
				Payload: &proto.ClientMessage_Heartbeat{
					Heartbeat: &proto.Heartbeat{
						TimestampUnix: time.Now().Unix(),
					},
				},
			}
			if err := stream.Send(msg); err != nil {
				log.Printf("[client] heartbeat send failed: %v", err)
				return
			}
		}
	}
}

// ---- recv loop ------------------------------------------------------------

func (c *Client) recvLoop(ctx context.Context, stream proto.TeamX_ChannelClient) error {
	for {
		// Check context before blocking on Recv.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := stream.Recv()
		if err == io.EOF {
			return nil // server closed stream cleanly
		}
		if err != nil {
			return err
		}

		switch payload := msg.Payload.(type) {
		case *proto.ServerMessage_HeartbeatAck:
			// Silence heartbeat acks in logs unless verbose.
		case *proto.ServerMessage_Command:
			log.Printf("[client] received command: id=%s type=%s",
				payload.Command.GetCommandId(), payload.Command.GetType())
			// Phase 5 will dispatch to command executor.
		default:
			log.Printf("[client] unknown server message seq=%d", msg.Seq)
		}
	}
}

// ---- helpers --------------------------------------------------------------

func (c *Client) nextSeq() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

// backoff computes the exponential backoff duration with ±25% jitter.
func backoff(initial, max time.Duration, attempt int) time.Duration {
	// 2^attempt, clamped.
	f := math.Min(float64(attempt), 63) // avoid overflow
	d := float64(initial) * math.Pow(2, f)
	if d > float64(max) {
		d = float64(max)
	}

	// ±25% jitter.
	jitter := d * 0.25 * (rand.Float64()*2 - 1)
	return time.Duration(d + jitter)
}
