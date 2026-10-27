package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
)

func newBackupCommand(ks keystore.Keystore, getServerURL func() string) *cobra.Command {
	var explicitFiles []string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Encrypt and upload environment files for this project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()
			return executeBackup(ctx, ks, serverURL, explicitFiles)
		},
	}

	cmd.Flags().StringSliceVarP(&explicitFiles, "file", "f", nil, "Specific file(s) to back up (defaults to all standard .env files)")
	return cmd
}

func executeBackup(ctx context.Context, ks keystore.Keystore, serverURL string, explicitFiles []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		return err
	}

	filesToBackup := explicitFiles
	if len(filesToBackup) == 0 {
		filesToBackup = FindDefaultEnvFiles(cwd)
	}

	if len(filesToBackup) == 0 {
		return errors.New("no .env files found in current directory to back up")
	}

	fileEntries, err := readFilesToEntries(cwd, filesToBackup)
	if err != nil {
		return err
	}

	rawBundle, err := bundle.CreateBundle(fileEntries, "envil/"+config.CLI_VERSION)
	if err != nil {
		return fmt.Errorf("create backup bundle: %w", err)
	}

	bundleBytes, err := bundle.EncodeBundle(rawBundle)
	if err != nil {
		return fmt.Errorf("encode backup bundle: %w", err)
	}

	apiClient, keys, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return err
	}

	backupsList, err := apiClient.ListBackups(ctx, projectCfg.ProjectID)
	if err != nil {
		return fmt.Errorf("query project history: %w", err)
	}

	expectedVersion := 0
	if len(backupsList) > 0 {
		expectedVersion = backupsList[0].Version
	}
	nextVersion := expectedVersion + 1

	projectKey, err := keys.ProjectKey(projectCfg.ProjectID.String())
	if err != nil {
		return fmt.Errorf("derive project key: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectCfg.ProjectID.String(), uint32(nextVersion))
	sealedBlob, err := crypto.Seal(projectKey, bundleBytes, aad)
	if err != nil {
		return fmt.Errorf("encrypt backup blob: %w", err)
	}

	backupResponse, err := apiClient.CreateBackup(ctx, projectCfg.ProjectID, expectedVersion, sealedBlob)
	if err != nil {
		if errors.Is(err, client.ErrVersionConflict) {
			return errors.New("backup conflict: remote version has changed. Run 'envil diff' or 'envil restore' first")
		}
		return fmt.Errorf("upload backup: %w", err)
	}

	fmt.Printf("✓ Backed up %d file(s) as version %d (size: %d bytes)\n",
		len(fileEntries), backupResponse.Version, backupResponse.SizeBytes)
	for _, entry := range fileEntries {
		fmt.Printf("  - %s\n", entry.Name)
	}

	return nil
}

func readFilesToEntries(baseDir string, filenames []string) ([]bundle.FileEntry, error) {
	entries := make([]bundle.FileEntry, 0, len(filenames))

	for _, filename := range filenames {
		filePath := filepath.Join(baseDir, filename)
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read file '%s': %w", filename, err)
		}

		entries = append(entries, bundle.FileEntry{
			Name:    filepath.Base(filename),
			Content: string(contentBytes),
			SHA256:  bundle.ComputeContentHash(string(contentBytes)),
		})
	}

	return entries, nil
}
