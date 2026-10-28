package localfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	gitCommandTimeout = 3 * time.Second
)

type GitStatus int

const (
	GitIgnored GitStatus = iota
	GitNotIgnored
	GitTracked
	GitSkipped
)

type GitCheckResult struct {
	Status     GitStatus
	GitMissing bool
}

func IsGitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func CheckGitStatus(ctx context.Context, dir, relPath string) GitCheckResult {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return GitCheckResult{
			Status:     GitSkipped,
			GitMissing: true,
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	checkIgnoreCmd := exec.CommandContext(cmdCtx, gitBin, "check-ignore", "-q", "--", relPath)
	checkIgnoreCmd.Dir = dir
	ignoreErr := checkIgnoreCmd.Run()

	if ignoreErr == nil {
		return GitCheckResult{Status: GitIgnored}
	}

	var exitErr *exec.ExitError
	if !errors.As(ignoreErr, &exitErr) {
		return GitCheckResult{Status: GitSkipped}
	}

	if exitErr.ExitCode() != 1 {
		// Exit code 128 (not a git repository) or other git error: skip silently
		return GitCheckResult{Status: GitSkipped}
	}

	// Exit code 1 means not ignored. Check if the file is already tracked in Git index.
	trackCtx, trackCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer trackCancel()

	lsFilesCmd := exec.CommandContext(trackCtx, gitBin, "ls-files", "--error-unmatch", "--", relPath)
	lsFilesCmd.Dir = dir
	if err := lsFilesCmd.Run(); err == nil {
		return GitCheckResult{Status: GitTracked}
	}

	return GitCheckResult{Status: GitNotIgnored}
}

func FormatGitWarning(relPath string, status GitStatus) string {
	switch status {
	case GitTracked:
		return fmt.Sprintf(
			"Warning: %s is already tracked by Git in this repository. Ignoring it will not remove it from Git history.",
			relPath,
		)
	case GitNotIgnored:
		return fmt.Sprintf(
			"Warning: %s is not ignored by Git in this repository. Secrets in this file could be committed by accident.",
			relPath,
		)
	default:
		return ""
	}
}

func AppendGitIgnorePattern(dir, pattern string) error {
	gitignorePath := filepath.Join(dir, ".gitignore")
	existingBytes, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .gitignore: %w", err)
	}

	existing := string(existingBytes)
	for _, line := range strings.Split(existing, "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	var updated strings.Builder
	updated.WriteString(existing)
	if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
		updated.WriteByte('\n')
	}
	updated.WriteString(pattern)
	updated.WriteByte('\n')

	if err := os.WriteFile(gitignorePath, []byte(updated.String()), 0644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}

