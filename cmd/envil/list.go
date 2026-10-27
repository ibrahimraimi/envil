package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/server"
)

func newListCommand(ks keystore.Keystore, getServerURL func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects stored on Envil",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()
			return executeList(ctx, ks, serverURL)
		},
	}
}

func executeList(ctx context.Context, ks keystore.Keystore, serverURL string) error {
	apiClient, keys, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return err
	}

	projects, err := apiClient.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("fetch projects list: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found on this account.")
		return nil
	}

	tabWriter := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tabWriter, "PROJECT ID\tNAME\tGIT REMOTE\tCREATED AT")

	for _, project := range projects {
		name, gitRemote := decryptProjectMeta(keys, project)
		fmt.Fprintf(tabWriter, "%s\t%s\t%s\t%s\n",
			project.ID.String(),
			name,
			gitRemote,
			project.CreatedAt.Format("2006-01-02 15:04"),
		)
	}

	return tabWriter.Flush()
}

func decryptProjectMeta(keys *crypto.Keys, project server.ProjectResponse) (string, string) {
	projectKey, err := keys.ProjectKey(project.ID.String())
	if err != nil {
		return "[decrypt error]", ""
	}

	metaCiphertext, err := base64.StdEncoding.DecodeString(project.MetaBlob)
	if err != nil {
		return "[invalid blob]", ""
	}

	aad := crypto.BuildAAD(keys.AccountID(), project.ID.String(), 0)
	plaintextBytes, err := crypto.Open(projectKey, metaCiphertext, aad)
	if err != nil {
		return "[decrypt error]", ""
	}

	meta, err := bundle.DecodeProjectMetadata(plaintextBytes)
	if err != nil {
		return "[unknown]", ""
	}

	return meta.Name, meta.GitRemote
}
