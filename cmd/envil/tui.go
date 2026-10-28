package main

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/tui"
)

func newTUICommand(svc service.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive terminal dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isInteractiveTerminal() {
				return errors.New("envil tui requires an interactive terminal (both stdin and stdout must be TTYs)")
			}
			return tui.Run(svc, nil)
		},
	}
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
