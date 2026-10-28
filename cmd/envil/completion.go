package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/spf13/cobra"
)

func newCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(out, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(out)
			case "fish":
				return cmd.Root().GenFishCompletion(out, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(out)
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
	return cmd
}

func completeVersionNumbers(svc service.Service) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		versions, err := svc.History(ctx, "")
		if err != nil || len(versions) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		suggestions := make([]string, 0, len(versions))
		for _, v := range versions {
			suggestions = append(suggestions, fmt.Sprintf("%s\tbackup from %s (%d B)",
				strconv.Itoa(v.Number),
				v.CreatedAt.Format("2006-01-02 15:04"),
				v.SizeBytes,
			))
		}
		return suggestions, cobra.ShellCompDirectiveNoFileComp
	}
}
