package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/tui"
)

func newRecoverCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Recover an existing Envil account using your 24-word recovery phrase",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			serverURL := getServerURL()

			shouldDisableTUI := noTUIFlag || os.Getenv("ENVIL_NO_TUI") == "1"
			if !shouldDisableTUI && isInteractiveTerminal() {
				return tui.Run(svc, tui.NewRecoverScreen(svc))
			}

			return executeRecover(ctx, ks, serverURL)
		},
	}
}

func executeRecover(ctx context.Context, ks keystore.Keystore, serverURL string) error {
	phrase, err := readPhraseInput()
	if err != nil {
		return err
	}

	cleanPhrase := strings.Join(strings.Fields(phrase), " ")
	if err := crypto.ValidatePhrase(cleanPhrase); err != nil {
		return errors.New("invalid recovery phrase: check word spelling and order")
	}

	keys, err := crypto.KeysFromPhrase(cleanPhrase)
	if err != nil {
		return fmt.Errorf("derive keys: %w", err)
	}

	if err := ks.SaveKeys(keys); err != nil {
		return fmt.Errorf("save keys to keystore: %w", err)
	}

	fmt.Println("✓ Encryption keys restored successfully to local keystore.")
	fmt.Println("\nRetrieving your projects from the server...")

	if err := executeList(ctx, ks, serverURL); err != nil {
		fmt.Printf("⚠️  Could not list projects automatically: %v\n", err)
	}

	fmt.Println("\nRecovery complete. You can now run 'envil restore' in your project directories.")
	return nil
}

func readPhraseInput() (string, error) {
	fmt.Println("Enter your 24-word recovery phrase (words separated by spaces):")

	if term.IsTerminal(int(os.Stdin.Fd())) {
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return "", fmt.Errorf("read recovery phrase: %w", err)
		}
		fmt.Println()
		return string(passwordBytes), nil
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read recovery phrase from stdin: %w", err)
	}
	return line, nil
}
