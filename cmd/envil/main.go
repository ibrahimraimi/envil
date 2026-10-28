package main

import (
	"fmt"
	"os"

	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/tui"
	"github.com/spf13/cobra"
)

var (
	noTUIFlag             bool
	acceptServerStateFlag bool
	noGitCheckFlag        bool
)

func main() {
	rootCmd := buildRootCommand()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", errs.Format(err))
		os.Exit(errs.ExitCode(err))
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
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if acceptServerStateFlag {
				fmt.Fprintln(cmd.OutOrStdout(), "Audit: accepting server state override (--accept-server-state).")
				cmd.SetContext(service.WithAcceptServerState(cmd.Context(), true))
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			shouldDisableTUI := noTUIFlag || os.Getenv("ENVIL_NO_TUI") == "1"
			if !shouldDisableTUI && isInteractiveTerminal() {
				return tui.Run(svc, nil)
			}
			return cmd.Help()
		},
	}

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &errs.UsageError{Err: err}
	})

	rootCmd.PersistentFlags().StringVar(&serverURLFlag, "server", "", "Envil API server URL (or via ENVIL_SERVER_URL)")
	rootCmd.PersistentFlags().BoolVar(&noTUIFlag, "no-tui", false, "Disable interactive terminal UI (or via ENVIL_NO_TUI=1)")
	rootCmd.PersistentFlags().BoolVar(&acceptServerStateFlag, "accept-server-state", false, "Accept server version state on rollback or fork")
	rootCmd.PersistentFlags().BoolVar(&noGitCheckFlag, "no-git-check", false, "Skip Git ignore safety check")

	rootCmd.AddCommand(
		newInitCommand(ks, getServerURL, svc),
		newBackupCommand(ks, getServerURL, svc),
		newRestoreCommand(ks, getServerURL, svc),
		newListCommand(ks, getServerURL, svc),
		newHistoryCommand(ks, getServerURL, svc),
		newDiffCommand(ks, getServerURL, svc),
		newRecoverCommand(ks, getServerURL, svc),
		newDoctorCommand(ks, getServerURL, svc),
		newDeleteCommand(ks, getServerURL),
		newStateCommand(svc),
		newCompletionCommand(),
		newTUICommand(svc),
		newVersionCommand(),
	)

	return rootCmd
}
