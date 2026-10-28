package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/localfiles"
)

func checkAndWarnGitIgnore(ctx context.Context, dir string, files []string, patternToOffer string) {
	if noGitCheckFlag {
		return
	}
	cfg := config.LoadClientConfig("")
	if !cfg.GitCheck {
		return
	}

	var nonIgnoredFile string
	for _, file := range files {
		res := localfiles.CheckGitStatus(ctx, dir, file)
		switch res.Status {
		case localfiles.GitTracked:
			fmt.Fprintln(os.Stderr, localfiles.FormatGitWarning(file, res.Status))
		case localfiles.GitNotIgnored:
			fmt.Fprintln(os.Stderr, localfiles.FormatGitWarning(file, res.Status))
			if nonIgnoredFile == "" {
				nonIgnoredFile = file
			}
		}
	}

	if nonIgnoredFile == "" || !isInteractiveTerminal() {
		return
	}

	pattern := patternToOffer
	if pattern == "" {
		pattern = nonIgnoredFile
	}

	fmt.Fprintf(os.Stderr, "Add %q to .gitignore? [y/N] ", pattern)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		if err := localfiles.AppendGitIgnorePattern(dir, pattern); err == nil {
			fmt.Fprintf(os.Stderr, "✓ Added %q to .gitignore\n", pattern)
		}
	}
}

