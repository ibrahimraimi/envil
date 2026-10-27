package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/keystore"
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

	rootCmd := &cobra.Command{
		Use:   "envil",
		Short: "Zero-knowledge environment variable backup and recovery tool",
		Long: `Envil is a secure, cloud-backed environment variable backup and recovery tool.
It encrypts .env files client-side using XChaCha20-Poly1305 and recovers everything
from a 24-word recovery phrase with zero server knowledge.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVar(&serverURLFlag, "server", "", "Envil API server URL (or via ENVIL_SERVER_URL)")

	rootCmd.AddCommand(
		newInitCommand(ks, getServerURL),
		newBackupCommand(ks, getServerURL),
		newRestoreCommand(ks, getServerURL),
		newListCommand(ks, getServerURL),
		newHistoryCommand(ks, getServerURL),
		newDiffCommand(ks, getServerURL),
		newRecoverCommand(ks, getServerURL),
		newDoctorCommand(ks, getServerURL),
		newDeleteCommand(ks, getServerURL),
		newVersionCommand(),
	)

	return rootCmd
}
