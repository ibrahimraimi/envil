package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/localfiles"
	"github.com/ibrahimraimi/envil/internal/state"
)

func (s *DefaultService) Doctor(ctx context.Context) (DoctorReport, error) {
	var checks []Check

	checks = append(checks, s.checkVersion())
	checks = append(checks, s.checkConfigDirectory())
	checks = append(checks, s.checkKeychain())

	keys, keysCheck := s.checkKeysPresent()
	checks = append(checks, keysCheck)

	serverReachable, dateHeader, serverCheck := s.checkServerReachable(ctx)
	checks = append(checks, serverCheck)

	checks = append(checks, s.checkAccountRegistered(ctx, keys, serverReachable))
	checks = append(checks, s.checkClockSkew(serverReachable, dateHeader))
	checks = append(checks, s.checkStateFile(keys))
	checks = append(checks, s.checkRollbackStatus(ctx, keys, serverReachable))
	checks = append(checks, s.checkGitSafety(ctx))
	checks = append(checks, s.checkBackupCopies())

	return DoctorReport{Checks: checks}, nil
}

func (s *DefaultService) checkVersion() Check {
	detail := fmt.Sprintf("v%s (%s/%s)", config.CLI_VERSION, runtime.GOOS, runtime.GOARCH)
	return NewCheck("Version", StatusOK, detail, "")
}

func (s *DefaultService) checkConfigDirectory() Check {
	if runtime.GOOS == "windows" {
		return NewCheck("Config directory", StatusSkipped, "POSIX file permission check skipped on Windows", "")
	}

	configDir := s.resolveConfigDir()
	if configDir == "" {
		return NewCheck("Config directory", StatusWarn, "could not resolve configuration directory", "")
	}

	dirInfo, err := os.Stat(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return NewCheck("Config directory", StatusOK, "not created yet (will be created with mode 0700)", "")
		}
		return NewCheck("Config directory", StatusFail, err.Error(), "Verify permissions on your home and config directory.")
	}

	if dirPerm := dirInfo.Mode().Perm(); dirPerm&0077 != 0 {
		return NewCheck(
			"Config directory",
			StatusFail,
			fmt.Sprintf("directory has insecure permissions %04o (expected 0700)", dirPerm),
			fmt.Sprintf("Run: chmod 0700 %s", configDir),
		)
	}

	entries, err := os.ReadDir(configDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr == nil && info.Mode().Perm()&0077 != 0 {
				return NewCheck(
					"Config directory",
					StatusFail,
					fmt.Sprintf("file %s has insecure permissions %04o (expected 0600)", entry.Name(), info.Mode().Perm()),
					"Ensure all files in the Envil config directory have mode 0600.",
				)
			}
		}
	}

	return NewCheck("Config directory", StatusOK, "private (dir 0700, files 0600)", "")
}

func (s *DefaultService) checkKeychain() Check {
	if _, isFileStore := s.keystore.(*keystore.FileStore); isFileStore {
		return NewCheck(
			"Keychain",
			StatusWarn,
			"using local 0600 file fallback",
			"Install and unlock a system keychain provider for hardware-backed secret storage.",
		)
	}

	if err := keystore.ProbeKeyring(); err != nil {
		return NewCheck(
			"Keychain",
			StatusWarn,
			"system keychain unavailable; using 0600 file fallback",
			"Ensure Secret Service / OS keychain is unlocked, or rely on the 0600 file fallback.",
		)
	}

	return NewCheck("Keychain", StatusOK, "system keychain read/write verified", "")
}

func (s *DefaultService) checkKeysPresent() (*crypto.Keys, Check) {
	keys, err := s.keystore.GetKeys()
	if err != nil {
		if errors.Is(err, keystore.ErrKeysNotFound) {
			return nil, NewCheck(
				"Keys present",
				StatusFail,
				"no keys found locally",
				"Run `envil init` to create a new account or `envil recover` on an existing account.",
			)
		}
		return nil, NewCheck(
			"Keys present",
			StatusFail,
			err.Error(),
			"Check your keystore permissions or run `envil recover`.",
		)
	}

	return keys, NewCheck("Keys present", StatusOK, "local signing and encryption keys loaded", "")
}

func (s *DefaultService) checkServerReachable(ctx context.Context) (bool, string, Check) {
	serverURL := s.getServerURL()
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	startTime := time.Now()
	request, err := http.NewRequestWithContext(reqCtx, http.MethodGet, serverURL+"/v1/healthz", nil)
	if err != nil {
		return false, "", NewCheck(
			"Server reachable",
			StatusFail,
			fmt.Sprintf("invalid server URL: %v", err),
			"Check your `ENVIL_SERVER` or `--server` setting.",
		)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		return false, "", NewCheck(
			"Server reachable",
			StatusFail,
			"could not reach server",
			"Check your network connection and `ENVIL_SERVER` setting.",
		)
	}
	defer response.Body.Close()

	duration := time.Since(startTime).Round(time.Millisecond)
	if response.StatusCode != http.StatusOK {
		return false, "", NewCheck(
			"Server reachable",
			StatusFail,
			fmt.Sprintf("HTTP %d from /v1/healthz", response.StatusCode),
			"Verify that the Envil server is healthy.",
		)
	}

	return true, response.Header.Get("Date"), NewCheck(
		"Server reachable",
		StatusOK,
		fmt.Sprintf("%s (latency %v)", serverURL, duration),
		"",
	)
}

func (s *DefaultService) checkAccountRegistered(ctx context.Context, keys *crypto.Keys, serverReachable bool) Check {
	if keys == nil || !serverReachable {
		return NewCheck("Account registered", StatusSkipped, "skipped (requires local keys and reachable server)", "")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	apiClient := client.NewClient(s.getServerURL(), keys.Signing)
	if _, err := apiClient.ListProjects(reqCtx); err != nil {
		return NewCheck(
			"Account registered",
			StatusFail,
			"authenticated request rejected by server",
			"Run `envil init` or `envil recover` to register your account credentials.",
		)
	}

	return NewCheck("Account registered", StatusOK, "authenticated request succeeded", "")
}

func (s *DefaultService) checkClockSkew(serverReachable bool, dateHeader string) Check {
	if !serverReachable {
		return NewCheck("Clock skew", StatusSkipped, "skipped (server unreachable)", "")
	}
	if dateHeader == "" {
		return NewCheck("Clock skew", StatusWarn, "server did not send a Date header", "")
	}

	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		return NewCheck("Clock skew", StatusWarn, "could not parse server Date header", "")
	}

	skewSeconds := math.Abs(time.Since(serverTime).Seconds())
	switch {
	case skewSeconds > 300:
		return NewCheck(
			"Clock skew",
			StatusFail,
			fmt.Sprintf("%.1fs skew (> 5 minutes)", skewSeconds),
			"Enable automatic NTP time synchronization, then retry.",
		)
	case skewSeconds > 120:
		return NewCheck(
			"Clock skew",
			StatusWarn,
			fmt.Sprintf("%.1fs skew (> 2 minutes)", skewSeconds),
			"Synchronize your system clock to avoid authentication failures.",
		)
	default:
		return NewCheck("Clock skew", StatusOK, fmt.Sprintf("%.1fs", skewSeconds), "")
	}
}

func (s *DefaultService) checkStateFile(keys *crypto.Keys) Check {
	if keys == nil {
		return NewCheck("State file", StatusSkipped, "skipped (no local keys to verify MAC)", "")
	}

	store, err := s.openStateStore(keys)
	if err != nil {
		return NewCheck("State file", StatusFail, err.Error(), "Run `envil state reset` to rebuild the state file.")
	}

	if !store.Exists() {
		return NewCheck("State file", StatusOK, "not created yet (will be initialized on first use)", "")
	}

	projects, err := store.Load()
	if err != nil {
		if errs.IsCode(err, errs.StateTampered) {
			return NewCheck(
				"State file",
				StatusFail,
				"state file MAC verification failed",
				"Run `envil state reset` to rebuild it.",
			)
		}
		return NewCheck("State file", StatusFail, err.Error(), "Run `envil state reset` to rebuild it.")
	}

	return NewCheck(
		"State file",
		StatusOK,
		fmt.Sprintf("valid MAC (%d project(s) tracked)", len(projects)),
		"",
	)
}

func (s *DefaultService) checkRollbackStatus(ctx context.Context, keys *crypto.Keys, serverReachable bool) Check {
	cwd, err := os.Getwd()
	if err != nil {
		return NewCheck("Rollback status", StatusSkipped, "could not determine working directory", "")
	}

	projectCfg, err := LoadProjectConfig(cwd)
	if err != nil {
		return NewCheck("Rollback status", StatusSkipped, "current directory is not linked to a project", "Run `envil init` to link this directory.")
	}

	if keys == nil || !serverReachable {
		return NewCheck("Rollback status", StatusSkipped, "skipped (requires local keys and reachable server)", "")
	}

	store, err := s.openStateStore(keys)
	if err != nil {
		return NewCheck("Rollback status", StatusFail, err.Error(), "Run `envil state reset`.")
	}

	prev, err := store.Get(projectCfg.ProjectID.String())
	if err != nil {
		return NewCheck("Rollback status", StatusFail, "state file tampered", "Run `envil state reset` to rebuild it.")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	apiClient := client.NewClient(s.getServerURL(), keys.Signing)
	backups, err := apiClient.ListBackups(reqCtx, projectCfg.ProjectID)
	if err != nil {
		return NewCheck("Rollback status", StatusWarn, "could not query project backups from server", "")
	}

	serverLatest := 0
	if len(backups) > 0 {
		serverLatest = backups[0].Version
	}

	verdict := state.Check(prev, serverLatest, "")
	switch verdict {
	case state.Rollback:
		return NewCheck(
			"Rollback status",
			StatusFail,
			fmt.Sprintf("Rollback (local seen v%d, server reports v%d)", prev.LatestVersion, serverLatest),
			"Use `--accept-server-state` only if you trust the server state, or `envil state reset`.",
		)
	case state.Fork:
		return NewCheck(
			"Rollback status",
			StatusFail,
			fmt.Sprintf("Fork detected at v%d", serverLatest),
			"Use `--accept-server-state` only if you trust the server state.",
		)
	case state.FirstSeen:
		return NewCheck(
			"Rollback status",
			StatusOK,
			fmt.Sprintf("FirstSeen (server latest v%d)", serverLatest),
			"",
		)
	case state.Advanced:
		return NewCheck(
			"Rollback status",
			StatusOK,
			fmt.Sprintf("Advanced (local v%d -> server v%d)", prev.LatestVersion, serverLatest),
			"",
		)
	default:
		return NewCheck(
			"Rollback status",
			StatusOK,
			fmt.Sprintf("OK (v%d)", serverLatest),
			"",
		)
	}
}

func (s *DefaultService) checkGitSafety(ctx context.Context) Check {
	if !localfiles.IsGitAvailable() {
		return NewCheck(
			"Git",
			StatusWarn,
			"git binary not found in PATH",
			"Install Git so Envil can verify that .env files are ignored.",
		)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return NewCheck("Git", StatusSkipped, "could not determine working directory", "")
	}

	candidates := FindDefaultEnvFiles(cwd)
	if len(candidates) == 0 {
		candidates = []string{".env"}
	}

	for _, file := range candidates {
		res := localfiles.CheckGitStatus(ctx, cwd, file)
		switch res.Status {
		case localfiles.GitSkipped:
			return NewCheck("Git", StatusSkipped, "not a Git repository", "")
		case localfiles.GitTracked:
			return NewCheck(
				"Git",
				StatusFail,
				fmt.Sprintf("%s is tracked by Git", file),
				fmt.Sprintf("Remove %s from Git tracking (`git rm --cached -- %s`) and add it to .gitignore.", file, file),
			)
		case localfiles.GitNotIgnored:
			return NewCheck(
				"Git",
				StatusWarn,
				fmt.Sprintf("%s is not ignored by Git", file),
				fmt.Sprintf("Add %q to .gitignore so secrets are not committed.", file),
			)
		}
	}

	return NewCheck("Git", StatusOK, "environment files are ignored by Git", "")
}

func (s *DefaultService) checkBackupCopies() Check {
	cwd, err := os.Getwd()
	if err != nil {
		return NewCheck("Backup copies", StatusSkipped, "could not determine working directory", "")
	}

	copies, err := localfiles.ListAllBackupCopies(cwd)
	if err != nil {
		return NewCheck("Backup copies", StatusWarn, "could not inspect local backup copies", "")
	}

	if len(copies) == 0 {
		return NewCheck("Backup copies", StatusOK, "0 backup copies in current directory", "")
	}

	oldest := copies[len(copies)-1]
	age := time.Since(oldest.Timestamp).Round(time.Minute)
	if age < 0 {
		age = 0
	}

	return NewCheck(
		"Backup copies",
		StatusOK,
		fmt.Sprintf("%d backup copy/copies present (oldest: %v)", len(copies), age),
		"Backup copies contain plaintext secrets; delete them when no longer needed.",
	)
}

func (s *DefaultService) resolveConfigDir() string {
	if s.statePath != "" {
		return filepath.Dir(s.statePath)
	}
	if fs, ok := s.keystore.(*keystore.FileStore); ok {
		return filepath.Dir(fs.FilePath())
	}
	dir, err := config.GetDefaultConfigDir()
	if err != nil {
		return ""
	}
	return dir
}

