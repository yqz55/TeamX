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
		online  string
		page    int32
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
		Use:   "show <client-id>",
		Short: "Show terminal detail + latest hardware",
		Long:  "Show terminal summary metadata and the most recent hardware report.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.GetTerminal(cmd.Context(), &proto.GetTerminalRequest{
				ClientId: args[0],
			})
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
		Use:   "history <client-id>",
		Short: "Show hardware report history",
		Long:  "List hardware snapshots for a terminal within an optional time range.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.GetTerminalHistory(cmd.Context(), &proto.GetTerminalHistoryRequest{
				ClientId: args[0],
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
		Use:   "kick <client-id>",
		Short: "Disconnect a terminal",
		Long:  "Forcefully disconnect an online terminal.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.DisconnectTerminal(cmd.Context(), &proto.DisconnectTerminalRequest{
				ClientId: args[0],
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
		Use:   "block <client-id>",
		Short: "Block a terminal",
		Long:  "Add a terminal to the blocklist and kick it if online.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.BlockTerminal(cmd.Context(), &proto.BlockTerminalRequest{
				ClientId: args[0],
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
		Use:   "unblock <client-id>",
		Short: "Unblock a terminal",
		Long:  "Remove a terminal from the blocklist.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := ctx.dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.UnblockTerminal(cmd.Context(), &proto.UnblockTerminalRequest{
				ClientId: args[0],
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
