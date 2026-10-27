package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
)

func newRestoreCommand(ks keystore.Keystore, getServerURL func() string) *cobra.Command {
	var versionNumber int
	var forceOverwrite bool

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore environment files from backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()
			return executeRestore(ctx, ks, serverURL, versionNumber, forceOverwrite)
		},
	}

	cmd.Flags().IntVarP(&versionNumber, "version", "v", 0, "Specific backup version to restore (0 for latest)")
	cmd.Flags().BoolVarP(&forceOverwrite, "force", "f", false, "Overwrite existing files without prompting (creates .bak backups)")
	return cmd
}

func executeRestore(ctx context.Context, ks keystore.Keystore, serverURL string, versionNumber int, forceOverwrite bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		return err
	}

	apiClient, keys, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return err
	}

	backupResp, err := apiClient.GetBackup(ctx, projectCfg.ProjectID, versionNumber)
	if err != nil {
		return fmt.Errorf("fetch backup from server: %w", err)
	}

	rawCiphertext, err := base64.StdEncoding.DecodeString(backupResp.Blob)
	if err != nil {
		return fmt.Errorf("decode backup blob: %w", err)
	}

	projectKey, err := keys.ProjectKey(projectCfg.ProjectID.String())
	if err != nil {
		return fmt.Errorf("derive project key: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectCfg.ProjectID.String(), uint32(backupResp.Version))
	decryptedBytes, err := crypto.Open(projectKey, rawCiphertext, aad)
	if err != nil {
		return fmt.Errorf("decrypt backup: %w", err)
	}

	restoredBundle, err := bundle.DecodeBundle(decryptedBytes)
	if err != nil {
		return fmt.Errorf("decode bundle payload: %w", err)
	}

	for _, entry := range restoredBundle.Files {
		if err := restoreSingleFile(cwd, entry, forceOverwrite); err != nil {
			return err
		}
	}

	fmt.Printf("✓ Restored %d file(s) from version %d (mode 0600)\n", len(restoredBundle.Files), backupResp.Version)
	return nil
}

func restoreSingleFile(baseDir string, entry bundle.FileEntry, forceOverwrite bool) error {
	targetPath := filepath.Join(baseDir, entry.Name)

	if existingBytes, err := os.ReadFile(targetPath); err == nil {
		if string(existingBytes) == entry.Content {
			fmt.Printf("  - %s (identical, skipped)\n", entry.Name)
			return nil
		}

		if !forceOverwrite {
			return fmt.Errorf("local file '%s' exists and has differing contents. Use --force to overwrite", entry.Name)
		}

		backupPath := targetPath + ".bak"
		if err := os.WriteFile(backupPath, existingBytes, 0600); err != nil {
			return fmt.Errorf("create backup copy '%s': %w", backupPath, err)
		}
		fmt.Printf("  - Created backup copy: %s\n", filepath.Base(backupPath))
	}

	if err := os.WriteFile(targetPath, []byte(entry.Content), 0600); err != nil {
		return fmt.Errorf("write file '%s': %w", entry.Name, err)
	}

	fmt.Printf("  - %s (restored)\n", entry.Name)
	return nil
}
