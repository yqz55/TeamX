package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	ctx := &cmdCtx{}

	root := &cobra.Command{
		Use:   "admin",
		Short: "TeamX Admin CLI — manage terminals from the command line",
		Long: `TeamX Admin CLI connects to a TeamX gRPC server and provides
subcommands for querying and managing remote terminals.

All commands require a running TeamX server (default localhost:50051).`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&ctx.serverAddr, "server", "localhost:50051", "TeamX gRPC server address")
	root.PersistentFlags().BoolVar(&ctx.jsonMode, "json", false, "Output in JSON format")

	root.AddCommand(
		newListCmd(ctx),
		newShowCmd(ctx),
		newHistoryCmd(ctx),
		newKickCmd(ctx),
		newBlockCmd(ctx),
		newUnblockCmd(ctx),
		newCmdCmd(ctx),
		newCmdLogCmd(ctx),
		newServeCmd(ctx),
	)

	if err := root.Execute(); err != nil {
		printError(err, ctx.jsonMode)
		os.Exit(1)
	}
}
