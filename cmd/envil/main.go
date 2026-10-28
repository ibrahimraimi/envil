package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/tui"
)

var (
	noTUIFlag bool
)

func main() {
	rootCmd := buildRootCommand()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func buildRootCommand() *cobra.Command {
	var serverURLFlag string
	ks := keystore.NewDefaultKeystore()

	getServerURL := func() string {
		cfg := config.LoadClientConfig(serverURLFlag)
		return cfg.ServerURL
	}

	svc := service.NewDefaultService(ks, getServerURL)

	rootCmd := &cobra.Command{
		Use:   "envil",
		Short: "Zero-knowledge environment variable backup and recovery tool",
		Long: `Envil is a secure, cloud-backed environment variable backup and recovery tool.
It encrypts .env files client-side using XChaCha20-Poly1305 and recovers everything
from a 24-word recovery phrase with zero server knowledge.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			shouldDisableTUI := noTUIFlag || os.Getenv("ENVIL_NO_TUI") == "1"
			if !shouldDisableTUI && isInteractiveTerminal() {
				return tui.Run(svc, nil)
			}
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&serverURLFlag, "server", "", "Envil API server URL (or via ENVIL_SERVER_URL)")
	rootCmd.PersistentFlags().BoolVar(&noTUIFlag, "no-tui", false, "Disable interactive terminal UI (or via ENVIL_NO_TUI=1)")

	rootCmd.AddCommand(
		newInitCommand(ks, getServerURL, svc),
		newBackupCommand(ks, getServerURL),
		newRestoreCommand(ks, getServerURL),
		newListCommand(ks, getServerURL),
		newHistoryCommand(ks, getServerURL),
		newDiffCommand(ks, getServerURL),
		newRecoverCommand(ks, getServerURL, svc),
		newDoctorCommand(ks, getServerURL),
		newDeleteCommand(ks, getServerURL),
		newTUICommand(svc),
		newVersionCommand(),
	)

	return rootCmd
}
