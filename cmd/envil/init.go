package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/tui"
)

func newInitCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	var skipConfirmation bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize envil on this machine or link a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()

			keys, err := ks.GetKeys()
			if err != nil {
				if errors.Is(err, keystore.ErrKeysNotFound) {
					shouldDisableTUI := noTUIFlag || os.Getenv("ENVIL_NO_TUI") == "1"
					if !shouldDisableTUI && !skipConfirmation && isInteractiveTerminal() {
						if err := tui.Run(svc, tui.NewSetupScreen(svc)); err != nil {
							return err
						}
						keys, err = ks.GetKeys()
						if err != nil {
							return err
						}
					} else {
						keys, err = executeFirstRunSetup(ctx, ks, serverURL, skipConfirmation)
						if err != nil {
							return err
						}
					}
				} else {
					return fmt.Errorf("read keystore: %w", err)
				}
			}

			return linkCurrentDirectory(ctx, ks, keys, serverURL)
		},
	}

	cmd.Flags().BoolVarP(&skipConfirmation, "yes", "y", false, "Skip recovery phrase word verification (for automated setups)")
	return cmd
}

func executeFirstRunSetup(ctx context.Context, ks keystore.Keystore, serverURL string, skipConfirmation bool) (*crypto.Keys, error) {
	fmt.Println("No existing Envil account found. Creating a new recovery phrase...")

	phrase, err := crypto.NewPhrase()
	if err != nil {
		return nil, fmt.Errorf("generate phrase: %w", err)
	}

	displayRecoveryPhrase(phrase)

	if !skipConfirmation && term.IsTerminal(int(os.Stdin.Fd())) {
		if err := verifyPhraseWords(phrase); err != nil {
			return nil, err
		}
	}

	keys, err := crypto.KeysFromPhrase(phrase)
	if err != nil {
		return nil, fmt.Errorf("derive keys: %w", err)
	}

	if err := ks.SaveKeys(keys); err != nil {
		return nil, fmt.Errorf("save keys locally: %w", err)
	}
	fmt.Println("✓ Encryption keys derived and saved securely.")

	apiClient := client.NewClient(serverURL, keys.Signing)
	if _, err := apiClient.RegisterAccount(ctx); err != nil {
		return nil, fmt.Errorf("register account on server: %w", err)
	}
	fmt.Println("✓ Account registered with Envil server.")

	return keys, nil
}

func displayRecoveryPhrase(phrase string) {
	fmt.Println("\n========================= YOUR RECOVERY PHRASE =========================")
	fmt.Println("Write these 24 words down in order and store them in a secure location.")
	fmt.Println("If you lose both this machine and your phrase, YOUR DATA IS UNRECOVERABLE.")
	fmt.Println("-------------------------------------------------------------------------")

	words := strings.Fields(phrase)
	for i, word := range words {
		fmt.Printf("%2d. %-14s ", i+1, word)
		if (i+1)%4 == 0 {
			fmt.Println()
		}
	}
	fmt.Println("=========================================================================")
	fmt.Println()
}

func verifyPhraseWords(phrase string) error {
	words := strings.Fields(phrase)
	reader := bufio.NewReader(os.Stdin)

	indicesToVerify := pickVerificationIndices(len(words), 3)
	for _, wordIndex := range indicesToVerify {
		fmt.Printf("Enter word #%d: ", wordIndex+1)
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read word input: %w", err)
		}

		entered := strings.TrimSpace(input)
		expected := words[wordIndex]
		if entered != expected {
			return fmt.Errorf("verification failed for word #%d, setup aborted", wordIndex+1)
		}
	}
	fmt.Println("✓ Recovery phrase verified successfully.")
	return nil
}

func pickVerificationIndices(totalWords, count int) []int {
	perm := rand.Perm(totalWords)
	return perm[:count]
}

func linkCurrentDirectory(ctx context.Context, ks keystore.Keystore, keys *crypto.Keys, serverURL string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}

	existingConfig, err := LoadProjectConfig(cwd)
	if err == nil {
		fmt.Printf("Project already linked with ID: %s\n", existingConfig.ProjectID)
		return nil
	}

	projectID := uuid.New()
	projectName := filepath.Base(cwd)
	gitRemote := DiscoverGitRemote(cwd)

	projectKey, err := keys.ProjectKey(projectID.String())
	if err != nil {
		return fmt.Errorf("derive project key: %w", err)
	}

	metadata := &bundle.ProjectMetadata{
		Name:      projectName,
		GitRemote: gitRemote,
	}
	metaBytes, err := bundle.EncodeProjectMetadata(metadata)
	if err != nil {
		return fmt.Errorf("encode project metadata: %w", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), projectID.String(), 0)
	sealedMeta, err := crypto.Seal(projectKey, metaBytes, aad)
	if err != nil {
		return fmt.Errorf("encrypt project metadata: %w", err)
	}

	apiClient := client.NewClient(serverURL, keys.Signing)
	if _, err := apiClient.UpsertProject(ctx, projectID, sealedMeta); err != nil {
		return fmt.Errorf("register project on server: %w", err)
	}

	if err := SaveProjectConfig(cwd, &ProjectConfig{ProjectID: projectID}); err != nil {
		return fmt.Errorf("save %s: %w", PROJECT_CONFIG_FILE, err)
	}

	fmt.Printf("✓ Linked directory '%s' to new project ID: %s\n", projectName, projectID)

	if !CheckGitIgnoreProtectsEnv(cwd) {
		fmt.Println("⚠️  WARNING: '.env' is not in your .gitignore file. Ensure secrets are not committed!")
	}

	return nil
}
