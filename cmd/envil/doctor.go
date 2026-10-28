package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/spf13/cobra"
)

func newDoctorCommand(ks keystore.Keystore, getServerURL func() string, svc service.Service) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose local keystore, server connectivity, clock skew, state, and git safety",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return executeDoctor(ctx, svc, cmd.OutOrStdout(), jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output diagnostic report as JSON")
	return cmd
}

func executeDoctor(ctx context.Context, svc service.Service, out io.Writer, jsonOutput bool) error {
	report, err := svc.Doctor(ctx)
	if err != nil {
		return err
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal doctor report: %w", err)
		}
		fmt.Fprintln(out, string(encoded))
		if report.HasFailures() {
			return errors.New("one or more diagnostic checks failed")
		}
		return nil
	}

	tabWriter := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tabWriter, "STATUS\tCHECK\tDETAIL")

	for _, check := range report.Checks {
		label := check.Label
		if label == "" {
			label = check.Status.String()
		}
		detail := check.Detail
		if detail == "" {
			detail = check.Message
		}
		fmt.Fprintf(tabWriter, "%s\t%s\t%s\n", label, check.Name, detail)
		if check.Hint != "" && (check.Status == service.StatusWarn || check.Status == service.StatusFail) {
			fmt.Fprintf(tabWriter, " \t \tHint: %s\n", check.Hint)
		}
	}

	if err := tabWriter.Flush(); err != nil {
		return err
	}

	if report.HasFailures() {
		return errors.New("one or more diagnostic checks failed")
	}
	return nil
}
