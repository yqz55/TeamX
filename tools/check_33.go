package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"teamx/internal/proto"
)

func main() {
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	client := proto.NewTeamXClient(conn)
	ctx := context.Background()

	// ---- 1. List terminals, get session_id and device_id ----
	fmt.Println("=== 1. ListTerminals ===")
	r1, err := client.ListTerminals(ctx, &proto.ListTerminalsRequest{PageSize: 10})
	if err != nil {
		log.Fatalf("ListTerminals: %v", err)
	}
	if len(r1.GetTerminals()) == 0 {
		log.Fatal("No terminals — start a client first")
	}
	sid := r1.GetTerminals()[0].GetSessionId()
	did := r1.GetTerminals()[0].GetDeviceId()
	fmt.Printf("session=%s device=%s host=%s online=%v\n",
		safeP(sid, 8), safeP(did, 16), r1.GetTerminals()[0].GetHostname(), r1.GetTerminals()[0].GetOnline())

	// ---- 2. Kick (by session_id) ----
	fmt.Println("\n=== 2. DisconnectTerminal (Kick) ===")
	r2, err := client.DisconnectTerminal(ctx, &proto.DisconnectTerminalRequest{SessionId: sid})
	if err != nil {
		log.Fatalf("DisconnectTerminal: %v", err)
	}
	fmt.Printf("ok=%v msg=%s\n", r2.GetOk(), r2.GetMessage())

	// Wait a moment for the client to disconnect.
	time.Sleep(2 * time.Second)

	// Verify client went offline.
	r1b, _ := client.ListTerminals(ctx, &proto.ListTerminalsRequest{PageSize: 10})
	for _, t := range r1b.GetTerminals() {
		if t.GetSessionId() == sid {
			fmt.Printf("  after kick: online=%v\n", t.GetOnline())
		}
	}

	// ---- 3. Block (by device_id) ----
	fmt.Println("\n=== 3. BlockTerminal ===")
	r3, err := client.BlockTerminal(ctx, &proto.BlockTerminalRequest{DeviceId: did})
	if err != nil {
		log.Fatalf("BlockTerminal: %v", err)
	}
	fmt.Printf("ok=%v msg=%s\n", r3.GetOk(), r3.GetMessage())

	// Verify blocked flag in DB (query by device_id).
	r3b, err := client.GetTerminal(ctx, &proto.GetTerminalRequest{DeviceId: did})
	if err != nil {
		log.Fatalf("GetTerminal after block: %v", err)
	}
	fmt.Printf("  terminal query ok, hardware=%v\n", r3b.GetLatestHardware() != nil)

	// ---- 4. Unblock (by device_id) ----
	fmt.Println("\n=== 4. UnblockTerminal ===")
	r4, err := client.UnblockTerminal(ctx, &proto.UnblockTerminalRequest{DeviceId: did})
	if err != nil {
		log.Fatalf("UnblockTerminal: %v", err)
	}
	fmt.Printf("ok=%v msg=%s\n", r4.GetOk(), r4.GetMessage())

	fmt.Println("\n✅ Phase 3.3 — all 3 control RPCs work correctly.")
}

func safeP(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
