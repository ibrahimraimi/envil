package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/spf13/cobra"
)

func newHistoryCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "List backup versions for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return executeHistory(ctx, svc)
		},
	}
}

func executeHistory(ctx context.Context, svc service.Service) error {
	versions, err := svc.History(ctx, "")
	if err != nil {
		return err
	}

	if len(versions) == 0 {
		fmt.Println("No backups found for this project.")
		return nil
	}

	tabWriter := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tabWriter, "VERSION\tSIZE\tCREATED AT")

	for _, version := range versions {
		fmt.Fprintf(tabWriter, "v%-6d\t%d B\t%s\n",
			version.Number,
			version.SizeBytes,
			version.CreatedAt.Format("2006-01-02 15:04:05"),
		)
	}

	return tabWriter.Flush()
}
