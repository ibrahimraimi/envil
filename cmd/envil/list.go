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

func newListCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects stored on Envil",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return executeListWithService(ctx, svc)
		},
	}
}

func executeList(ctx context.Context, ks keystore.Keystore, serverURL string) error {
	svc := service.NewDefaultService(ks, func() string { return serverURL })
	return executeListWithService(ctx, svc)
}

func executeListWithService(ctx context.Context, svc service.Service) error {
	projects, err := svc.ListProjects(ctx)
	if err != nil {
		return err
	}

	if len(projects) == 0 {
		fmt.Println("No projects found on this account.")
		return nil
	}

	tabWriter := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tabWriter, "PROJECT ID\tNAME\tGIT REMOTE\tCREATED AT")

	for _, project := range projects {
		fmt.Fprintf(tabWriter, "%s\t%s\t%s\t%s\n",
			project.ID,
			project.Name,
			project.GitRemote,
			project.CreatedAt.Format("2006-01-02 15:04"),
		)
	}

	return tabWriter.Flush()
}
