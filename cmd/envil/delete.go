package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/keystore"
)

func newDeleteCommand(ks keystore.Keystore, getServerURL func() string) *cobra.Command {
	var shouldDeleteAccount bool
	var bypassConfirm bool

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete current project or entire account",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()

			if shouldDeleteAccount {
				return deleteAccount(ctx, ks, serverURL, bypassConfirm)
			}
			return deleteCurrentProject(ctx, ks, serverURL, bypassConfirm)
		},
	}

	cmd.Flags().BoolVar(&shouldDeleteAccount, "account", false, "Delete the entire account, all projects, and all backups")
	cmd.Flags().BoolVarP(&bypassConfirm, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func deleteCurrentProject(ctx context.Context, ks keystore.Keystore, serverURL string, bypassConfirm bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		return err
	}

	if !bypassConfirm {
		fmt.Printf("Are you sure you want to delete project '%s'? (y/N): ", projectCfg.ProjectID)
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(input)) != "y" {
			fmt.Println("Deletion aborted.")
			return nil
		}
	}

	apiClient, _, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return err
	}

	if err := apiClient.DeleteProject(ctx, projectCfg.ProjectID); err != nil {
		return fmt.Errorf("delete project on server: %w", err)
	}

	configPath := filepath.Join(cwd, PROJECT_CONFIG_FILE)
	_ = os.Remove(configPath)

	fmt.Printf("✓ Project '%s' deleted successfully and unlinked.\n", projectCfg.ProjectID)
	return nil
}

func deleteAccount(ctx context.Context, ks keystore.Keystore, serverURL string, bypassConfirm bool) error {
	if !bypassConfirm {
		fmt.Println("⚠️  WARNING: This will permanently delete your account and all backups on the server.")
		fmt.Print("Type 'delete my account' to confirm: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.TrimSpace(input) != "delete my account" {
			return errors.New("confirmation phrase mismatch, account deletion aborted")
		}
	}

	apiClient, _, err := ResolveClientAndKeys(ks, serverURL)
	if err != nil {
		return err
	}

	if err := apiClient.DeleteAccount(ctx); err != nil {
		return fmt.Errorf("delete account on server: %w", err)
	}

	if err := ks.DeleteKeys(); err != nil {
		return fmt.Errorf("delete local keys: %w", err)
	}

	fmt.Println("✓ Account and all backups permanently deleted. Local keys removed.")
	return nil
}
