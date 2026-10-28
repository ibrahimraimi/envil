package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/state"
	"github.com/spf13/cobra"
)

func newDiffCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	var versionNumber int
	var revealSecrets bool

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show variable-level diff between local and remote backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()
			return executeDiff(ctx, ks, serverURL, versionNumber, revealSecrets)
		},
	}

	cmd.Flags().IntVarP(&versionNumber, "version", "v", 0, "Specific backup version to compare against (defaults to latest)")
	cmd.Flags().BoolVar(&revealSecrets, "reveal", false, "Reveal secret values instead of masking them")
	_ = cmd.RegisterFlagCompletionFunc("version", completeVersionNumbers(svc))
	return cmd
}

func executeDiff(ctx context.Context, ks keystore.Keystore, serverURL string, versionNumber int, revealSecrets bool) error {
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

	remoteBundle, err := bundle.DecodeBundle(decryptedBytes)
	if err != nil {
		return err
	}

	if isLatestBlob || service.ShouldAcceptServerState(ctx) {
		_ = stateStore.Put(projectCfg.ProjectID.String(), state.Entry{
			LatestVersion: serverLatest,
			BlobSHA256:    blobHash,
			SeenAt:        time.Now().UTC(),
		})
	}

	for _, remoteFile := range remoteBundle.Files {
		localPath := filepath.Join(cwd, remoteFile.Name)
		localBytes, _ := os.ReadFile(localPath)
		localContent := string(localBytes)

		fileDiff := bundle.DiffEnvContent(remoteFile.Name, localContent, remoteFile.Content)
		printFileDiff(fileDiff, revealSecrets)
	}

	return nil
}

func printFileDiff(fileDiff bundle.FileDiff, revealSecrets bool) {
	fmt.Printf("\n--- %s (local vs remote) ---\n", fileDiff.Filename)

	hasChanges := false
	for _, entry := range fileDiff.Entries {
		if entry.Change == bundle.ChangeEqual {
			continue
		}
		hasChanges = true
		printDiffEntry(entry, revealSecrets)
	}

	if !hasChanges {
		fmt.Println("No differences found. Local file is in sync with backup.")
	}
}

func printDiffEntry(entry bundle.VariableDiff, revealSecrets bool) {
	localVal := bundle.MaskSecret(entry.LocalValue, revealSecrets)
	remoteVal := bundle.MaskSecret(entry.RemoteValue, revealSecrets)

	switch entry.Change {
	case bundle.ChangeAdded:
		fmt.Printf("+ %s=%s (only in remote)\n", entry.Key, remoteVal)
	case bundle.ChangeRemoved:
		fmt.Printf("- %s=%s (only in local)\n", entry.Key, localVal)
	case bundle.ChangeModified:
		fmt.Printf("~ %s: local=%s -> remote=%s\n", entry.Key, localVal, remoteVal)
	}
}
