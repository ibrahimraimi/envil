package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/ibrahimraimi/envil/internal/config"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number of Envil CLI",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("envil version %s (%s/%s)\n", config.CLI_VERSION, runtime.GOOS, runtime.GOARCH)
		},
	}
}
