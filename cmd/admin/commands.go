package main

import (
	"fmt"

	"teamx/internal/proto"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ---- shared state ------------------------------------------------------------

type cmdCtx struct {
	serverAddr string
	jsonMode   bool
}

// dial connects to the server and returns a client + closer.
// Connection is non-blocking; the first RPC will establish the transport.
func (c *cmdCtx) dial() (proto.TeamXClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(c.serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", c.serverAddr, err)
	}
	return proto.NewTeamXClient(conn), conn, nil
}

// ---- list --------------------------------------------------------------------

func newListCmd(ctx *cmdCtx) *cobra.Command {
	var (
		online   string
		page     int32
		pageSize int32
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List terminals",
		Long:  "List all registered terminals with optional online/offline filter and pagination.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &proto.ListTerminalsRequest{
				Page:     page,
				PageSize: pageSize,
			}
			switch online {
			case "online":
				v := true
				req.OnlineFilter = &v
			case "offline":
				v := false
				req.OnlineFilter = &v
			}

			resp, err := client.ListTerminals(cmd.Context(), req)
			if err != nil {
				return err
			}

			printTerminalList(resp.Terminals, resp.TotalCount, ctx.jsonMode)
			return nil
		},
	}

	cmd.Flags().StringVar(&online, "status", "", "Filter by status: online, offline")
	cmd.Flags().Int32Var(&page, "page", 1, "Page number (1-based)")
	cmd.Flags().Int32Var(&pageSize, "page-size", 50, "Page size (max 500)")
	return cmd
}

// ---- show --------------------------------------------------------------------

func newShowCmd(ctx *cmdCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <session-id|device-id>",
		Short: "Show terminal detail + latest hardware",
		Long:  "Show terminal summary metadata and the most recent hardware report.\nAccepts session_id or device_id (auto-detect by length: 64-char = device_id).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &proto.GetTerminalRequest{}
			// Auto-detect: 64-char hex = device_id, otherwise session_id.
			if len(args[0]) == 64 {
				req.DeviceId = args[0]
			} else {
				req.SessionId = args[0]
			}

			resp, err := client.GetTerminal(cmd.Context(), req)
			if err != nil {
				return err
			}

			printTerminalDetail(resp, ctx.jsonMode)
			return nil
		},
	}
	return cmd
}

// ---- history -----------------------------------------------------------------

func newHistoryCmd(ctx *cmdCtx) *cobra.Command {
	var since, until string
	var limit int32

	cmd := &cobra.Command{
		Use:   "history <device-id>",
		Short: "Show hardware report history",
		Long:  "List hardware snapshots for a device within an optional time range.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.GetTerminalHistory(cmd.Context(), &proto.GetTerminalHistoryRequest{
				DeviceId: args[0],
				Since:    since,
				Until:    until,
				Limit:    limit,
			})
			if err != nil {
				return err
			}

			printTerminalHistory(resp, ctx.jsonMode)
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "Start time (RFC3339)")
	cmd.Flags().StringVar(&until, "until", "", "End time (RFC3339)")
	cmd.Flags().Int32Var(&limit, "limit", 100, "Max snapshots (max 500)")
	return cmd
}

// ---- kick --------------------------------------------------------------------

func newKickCmd(ctx *cmdCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kick <session-id>",
		Short: "Disconnect a session",
		Long:  "Forcefully disconnect an online session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.DisconnectTerminal(cmd.Context(), &proto.DisconnectTerminalRequest{
				SessionId: args[0],
			})
			if err != nil {
				return err
			}

			printResult(resp.Ok, resp.Message, ctx.jsonMode)
			return nil
		},
	}
	return cmd
}

// ---- block -------------------------------------------------------------------

func newBlockCmd(ctx *cmdCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "block <device-id>",
		Short: "Block a device",
		Long:  "Add a device to the blocklist. All its sessions will be kicked.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.BlockTerminal(cmd.Context(), &proto.BlockTerminalRequest{
				DeviceId: args[0],
			})
			if err != nil {
				return err
			}

			printResult(resp.Ok, resp.Message, ctx.jsonMode)
			return nil
		},
	}
	return cmd
}

// ---- unblock -----------------------------------------------------------------

func newUnblockCmd(ctx *cmdCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unblock <device-id>",
		Short: "Unblock a device",
		Long:  "Remove a device from the blocklist.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.UnblockTerminal(cmd.Context(), &proto.UnblockTerminalRequest{
				DeviceId: args[0],
			})
			if err != nil {
				return err
			}

			printResult(resp.Ok, resp.Message, ctx.jsonMode)
			return nil
		},
	}
	return cmd
}
