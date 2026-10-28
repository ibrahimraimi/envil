package main

import (
	"fmt"

	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/spf13/cobra"
)

func newStateCommand(svc service.Service) *cobra.Command {
	stateCmd := &cobra.Command{
		Use:   "state",
		Short: "Manage local rollback-detection state",
	}

	var projectID string
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset local version tracking state for a project or rebuild state.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := svc.ResetState(ctx, projectID); err != nil {
				return err
			}
			if projectID != "" {
				fmt.Printf("✓ Reset local state for project %s.\n", projectID)
			} else {
				fmt.Println("✓ Local state file reset successfully.")
			}
			return nil
		},
	}

	resetCmd.Flags().StringVar(&projectID, "project", "", "Project ID to reset (omit to reset all local state)")
	stateCmd.AddCommand(resetCmd)
	return stateCmd
}
