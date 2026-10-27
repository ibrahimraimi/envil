package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/keystore"
)

func newHistoryCommand(ks keystore.Keystore, getServerURL func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "List backup versions for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()
			return executeHistory(ctx, ks, serverURL)
		},
	}
}

func executeHistory(ctx context.Context, ks keystore.Keystore, serverURL string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		return err
	}

	apiClient, _, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return err
	}

	backups, err := apiClient.ListBackups(ctx, projectCfg.ProjectID)
	if err != nil {
		return fmt.Errorf("query backup history: %w", err)
	}

	if len(backups) == 0 {
		fmt.Println("No backups found for this project.")
		return nil
	}

	tabWriter := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tabWriter, "VERSION\tSIZE\tCREATED AT")

	for _, backup := range backups {
		fmt.Fprintf(tabWriter, "v%-6d\t%d B\t%s\n",
			backup.Version,
			backup.SizeBytes,
			backup.CreatedAt.Format("2006-01-02 15:04:05"),
		)
	}

	return tabWriter.Flush()
}
