package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/localfiles"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/state"
	"github.com/spf13/cobra"
)

func newRestoreCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
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
	cmd.Flags().BoolVarP(&forceOverwrite, "force", "f", false, "Overwrite existing files without prompting (creates timestamped .bak copies)")
	_ = cmd.RegisterFlagCompletionFunc("version", completeVersionNumbers(svc))
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

	backups, err := apiClient.ListBackups(ctx, projectCfg.ProjectID)
	if err != nil {
		return err
	}
	serverLatest := 0
	if len(backups) > 0 {
		serverLatest = backups[0].Version
	}

	backupResp, err := apiClient.GetBackup(ctx, projectCfg.ProjectID, versionNumber)
	if err != nil {
		return err
	}
	if serverLatest == 0 || (versionNumber == 0 && backupResp.Version > 0) {
		serverLatest = backupResp.Version
	}

	rawCiphertext, err := base64.StdEncoding.DecodeString(backupResp.Blob)
	if err != nil {
		return fmt.Errorf("decode backup blob: %w", err)
	}

	stateKey, err := keys.StateKey()
	if err != nil {
		return err
	}
	statePath, err := state.GetDefaultStateFilePath()
	if err != nil {
		return err
	}
	stateStore := state.NewStore(statePath, stateKey)
	prevEntry, err := stateStore.Get(projectCfg.ProjectID.String())
	if err != nil {
		return err
	}

	isLatestBlob := backupResp.Version == serverLatest
	blobHash := ""
	if isLatestBlob {
		blobHash = state.ComputeBlobHash(rawCiphertext)
	}

	verdict := state.Check(prevEntry, serverLatest, blobHash)
	if verdict == state.Rollback || verdict == state.Fork {
		if !service.ShouldAcceptServerState(ctx) {
			if verdict == state.Rollback {
				return errs.NewRollback(serverLatest, prevEntry.LatestVersion)
			}
			return errs.NewFork(serverLatest)
		}
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
		return err
	}

	if verdict == state.FirstSeen {
		fmt.Printf("Note: first restore for this project on this machine (recording version %d).\n", serverLatest)
	}

	if isLatestBlob || service.ShouldAcceptServerState(ctx) {
		if err := stateStore.Put(projectCfg.ProjectID.String(), state.Entry{
			LatestVersion: serverLatest,
			BlobSHA256:    blobHash,
			SeenAt:        time.Now().UTC(),
		}); err != nil {
			return err
		}
	}

	cfg := config.LoadClientConfig(serverURL)
	now := time.Now().UTC()
	var restoredNames []string
	for _, entry := range restoredBundle.Files {
		if err := restoreSingleFile(ctx, cwd, entry, forceOverwrite, cfg.BackupCopies, now); err != nil {
			return err
		}
		restoredNames = append(restoredNames, entry.Name)
	}

	checkAndWarnGitIgnore(ctx, cwd, restoredNames, ".env")

	fmt.Printf("✓ Restored %d file(s) from version %d (mode 0600)\n", len(restoredBundle.Files), backupResp.Version)
	return nil
}

func restoreSingleFile(ctx context.Context, baseDir string, entry bundle.FileEntry, forceOverwrite bool, backupCopies int, now time.Time) error {
	targetPath := filepath.Join(baseDir, entry.Name)

	if existingBytes, err := os.ReadFile(targetPath); err == nil {
		if string(existingBytes) == entry.Content {
			fmt.Printf("  - %s (identical, skipped)\n", entry.Name)
			return nil
		}

		if !forceOverwrite {
			return fmt.Errorf("local file '%s' exists and has differing contents. Use --force to overwrite", entry.Name)
		}

		if backupCopies > 0 {
			nextCopyName := localfiles.NextBackupCopyName(baseDir, entry.Name, now)
			checkAndWarnGitIgnore(ctx, baseDir, []string{nextCopyName}, ".env.bak*")

			createdCopy, err := localfiles.CreateBackupCopy(baseDir, entry.Name, existingBytes, now)
			if err != nil {
				return err
			}
			_ = localfiles.PruneBackupCopies(baseDir, entry.Name, backupCopies)
			fmt.Printf("  - Created backup copy: %s\n", createdCopy)
		}
	}

	if err := os.WriteFile(targetPath, []byte(entry.Content), 0600); err != nil {
		return fmt.Errorf("write file '%s': %w", entry.Name, err)
	}
	_ = os.Chmod(targetPath, 0600)

	fmt.Printf("  - %s (restored)\n", entry.Name)
	return nil
}
