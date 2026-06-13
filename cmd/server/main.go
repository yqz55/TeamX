package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"teamx/internal/proto"
	"teamx/internal/server"
)

func main() {
	port := flag.Int("port", 50051, "gRPC listen port")
	heartbeatInterval := flag.Duration("heartbeat-interval", 10*time.Second, "How often to check heartbeat timeout")
	heartbeatTimeout := flag.Duration("heartbeat-timeout", 30*time.Second, "Heartbeat timeout before marking offline")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}

	cm := server.NewConnectionManager()
	srv := server.NewTeamXServer(cm)

	grpcServer := grpc.NewServer()
	proto.RegisterTeamXServer(grpcServer, srv)

	// Enable reflection for debugging with grpcurl.
	reflection.Register(grpcServer)

	// Start heartbeat checker.
	go cm.HeartbeatChecker(*heartbeatInterval, *heartbeatTimeout)

	// Graceful shutdown on SIGINT / SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		grpcServer.GracefulStop()
	}()

	log.Printf("TeamX Server listening on %s", addr)
	log.Printf("  heartbeat check interval: %v, timeout: %v", *heartbeatInterval, *heartbeatTimeout)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("server exited: %v", err)
	}
	log.Println("server stopped")
}
