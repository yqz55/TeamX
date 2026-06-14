package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"teamx/internal/proto"
)

func safePrefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func main() {
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	client := proto.NewTeamXClient(conn)
	ctx := context.Background()

	// 1. ListTerminals — all terminals
	fmt.Println("=== ListTerminals (all) ===")
	r1, err := client.ListTerminals(ctx, &proto.ListTerminalsRequest{PageSize: 10, Page: 1})
	if err != nil {
		log.Fatalf("ListTerminals: %v", err)
	}
	fmt.Printf("total=%d terminals=%d\n", r1.GetTotalCount(), len(r1.GetTerminals()))
	for _, t := range r1.GetTerminals() {
		fmt.Printf("  id=%s host=%s os=%s online=%v hb=%s\n",
			safePrefix(t.GetClientId(), 8), t.GetHostname(), t.GetOs(), t.GetOnline(), safePrefix(t.GetLastHeartbeat(), 19))
	}

	if len(r1.GetTerminals()) == 0 {
		log.Fatal("NO TERMINALS — cannot continue")
	}
	cid := r1.GetTerminals()[0].GetClientId()

	// 2. ListTerminals — online only
	fmt.Println("\n=== ListTerminals (online only) ===")
	online := true
	r1b, _ := client.ListTerminals(ctx, &proto.ListTerminalsRequest{OnlineFilter: &online, PageSize: 10, Page: 1})
	fmt.Printf("total=%d\n", r1b.GetTotalCount())

	// 3. GetTerminal
	fmt.Println("\n=== GetTerminal ===")
	r2, err := client.GetTerminal(ctx, &proto.GetTerminalRequest{ClientId: cid})
	if err != nil {
		log.Fatalf("GetTerminal: %v", err)
	}
	fmt.Printf("summary: host=%s os=%s\n", r2.GetSummary().GetHostname(), r2.GetSummary().GetOs())
	hw := r2.GetLatestHardware()
	if hw != nil {
		fmt.Printf("hardware: cpu=%s cores=%d threads=%d arch=%s mem=%dMB\n",
			hw.GetCpu().GetModel(), hw.GetCpu().GetCores(), hw.GetCpu().GetThreads(),
			hw.GetCpu().GetArchitecture(), hw.GetMemory().GetTotalBytes()/(1024*1024))
	} else {
		fmt.Println("hardware: nil")
	}

	// 4. GetTerminalHistory
	fmt.Println("\n=== GetTerminalHistory ===")
	r3, err := client.GetTerminalHistory(ctx, &proto.GetTerminalHistoryRequest{
		ClientId: cid,
		Limit:    5,
	})
	if err != nil {
		log.Fatalf("GetTerminalHistory: %v", err)
	}
	fmt.Printf("snapshots=%d\n", len(r3.GetSnapshots()))
	for i, s := range r3.GetSnapshots() {
		fmt.Printf("  [%d] rid=%s created=%s\n", i, safePrefix(s.GetReportId(), 8), safePrefix(s.GetCreatedAt(), 19))
	}

	fmt.Println("\n✅ All 3 query RPCs work correctly.")
}
