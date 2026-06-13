package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"teamx/internal/client"
)

func main() {
	serverAddr := flag.String("server", "localhost:50051", "TeamX Server address (host:port)")
	heartbeatInterval := flag.Duration("heartbeat", 10*time.Second, "Heartbeat interval")
	reconnectInitial := flag.Duration("reconnect-initial", 1*time.Second, "Initial reconnect delay")
	reconnectMax := flag.Duration("reconnect-max", 60*time.Second, "Maximum reconnect delay")
	clientVersion := flag.String("version", "0.2.0", "Client version string")
	flag.Parse()

	cfg := client.Config{
		ServerAddr:        *serverAddr,
		HeartbeatInterval: *heartbeatInterval,
		ReconnectInitial:  *reconnectInitial,
		ReconnectMax:      *reconnectMax,
		ClientVersion:     *clientVersion,
	}

	c := client.NewClient(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
	}()

	info := c.SysInfo()
	log.Printf("TeamX Client v%s starting — server=%s", cfg.ClientVersion, cfg.ServerAddr)
	log.Printf("  hostname=%s os=%s", info.Hostname, info.OS)

	if err := c.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("client exited: %v", err)
	}
	log.Println("client stopped")
}
