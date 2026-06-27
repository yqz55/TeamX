package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newServeCmd(ctx *cmdCtx) *cobra.Command {
	var (
		httpPort     int
		corsOrigin   string
		pollInterval int
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API gateway",
		Long: `Start an HTTP server that exposes the TeamX admin RPCs via ConnectRPC
(JSON over HTTP) and broadcasts terminal state changes over WebSocket.

The gateway proxies requests to the gRPC backend (--server flag).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := GatewayConfig{
				GRPCAddr:     ctx.serverAddr,
				PollInterval: time.Duration(pollInterval) * time.Second,
			}

			gw, err := NewGateway(cfg)
			if err != nil {
				return fmt.Errorf("gateway: %w", err)
			}
			defer gw.Close()

			addr := fmt.Sprintf(":%d", httpPort)
			srv := &http.Server{
				Addr:         addr,
				Handler:      gw.handler(corsOrigin),
				ReadTimeout:  15 * time.Second,
				WriteTimeout: 15 * time.Second,
				IdleTimeout:  60 * time.Second,
			}

			// Graceful shutdown on SIGINT / SIGTERM.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				sig := <-sigCh
				log.Printf("received signal %v, shutting down...", sig)
				srv.Shutdown(context.Background())
			}()

			log.Printf("TeamX Admin Gateway listening on %s", addr)
			log.Printf("  gRPC backend: %s", ctx.serverAddr)
			log.Printf("  CORS origin:  %s", corsOrigin)
			log.Printf("  WS poll:      every %ds", pollInterval)

			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				return err
			}
			log.Println("gateway stopped")
			return nil
		},
	}

	cmd.Flags().IntVar(&httpPort, "http-port", 8080, "HTTP listen port")
	cmd.Flags().StringVar(&corsOrigin, "cors-origin", "*", "CORS Access-Control-Allow-Origin")
	cmd.Flags().IntVar(&pollInterval, "poll-interval", 5, "WebSocket state poll interval (seconds)")

	return cmd
}
