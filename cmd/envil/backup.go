package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/spf13/cobra"
)

func newBackupCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	var explicitFiles []string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Encrypt and upload environment files for this project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return executeBackup(ctx, svc, explicitFiles)
		},
	}

	cmd.Flags().StringSliceVarP(&explicitFiles, "file", "f", nil, "Specific file(s) to back up (defaults to all standard .env files)")
	return cmd
}

func executeBackup(ctx context.Context, svc service.Service, explicitFiles []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}

	filesToBackup := explicitFiles
	if len(filesToBackup) == 0 {
		filesToBackup = FindDefaultEnvFiles(cwd)
	}
	if len(filesToBackup) == 0 {
		return errors.New("no .env files found in current directory to back up")
	}

	checkAndWarnGitIgnore(ctx, cwd, filesToBackup, "")

	fileEntries, err := readFilesToEntries(cwd, filesToBackup)
	if err != nil {
		return err
	}

	version, err := svc.Backup(ctx, "", filesToBackup)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Backed up %d file(s) as version %d\n", len(fileEntries), version)
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
