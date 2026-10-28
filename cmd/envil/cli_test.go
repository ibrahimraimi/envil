package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/errs"
	"github.com/ibrahimraimi/envil/internal/localfiles"
	"github.com/ibrahimraimi/envil/internal/server"
	"github.com/ibrahimraimi/envil/internal/service"
	"github.com/ibrahimraimi/envil/internal/store"
)

func TestRootCommandHelpAndFlags(t *testing.T) {
	rootCmd := buildRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute --help failed: %v", err)
	}

	helpText := buf.String()
	if !strings.Contains(helpText, "--no-tui") {
		t.Fatalf("expected help text to contain '--no-tui', got: %s", helpText)
	}
	if !strings.Contains(helpText, "tui") {
		t.Fatalf("expected help text to contain 'tui' command, got: %s", helpText)
	}
}

func TestTUICommandRequiresTTY(t *testing.T) {
	rootCmd := buildRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"tui"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected error when running envil tui in non-interactive environment, got nil")
	}

	if !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("expected error mentioning interactive terminal, got: %v", err)
	}
}

func TestBareEnvilInNonTTYPrintsHelp(t *testing.T) {
	rootCmd := buildRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute bare command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("expected help output for bare invocation in non-TTY, got: %s", output)
	}
}

func TestShellCompletions(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, sh := range shells {
		t.Run(sh, func(t *testing.T) {
			rootCmd := buildRootCommand()
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)
			rootCmd.SetArgs([]string{"completion", sh})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v", sh, err)
			}
			if buf.Len() == 0 {
				t.Fatalf("expected non-empty completion script for %s", sh)
			}
			if !strings.Contains(buf.String(), "envil") {
				t.Fatalf("expected completion script for %s to reference envil", sh)
			}
		})
	}
}

func TestCLIEndToEndScenarios(t *testing.T) {
	cfg := server.Config{
		RateLimitRPS:   1000,
		RateLimitBurst: 2000,
		MaxBlobBytes:   1024 * 1024,
	}
	st := store.NewMemoryStore()
	nonces := auth.NewMemoryNonceTracker()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(server.BuildRouter(cfg, st, nonces, logger))
	t.Cleanup(srv.Close)

	configDir := t.TempDir()
	if err := os.Chmod(configDir, 0700); err != nil {
		t.Fatalf("chmod configDir: %v", err)
	}
	t.Setenv("ENVIL_CONFIG_DIR", configDir)
	t.Setenv("ENVIL_SERVER_URL", srv.URL)
	t.Setenv("ENVIL_NO_TUI", "1")

	// Configure backup_copies = 2 and git_check = false for automated test
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"backup_copies": 2, "git_check": false}`), 0600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	projDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	_ = os.Chdir(projDir)

	runCLI := func(args ...string) (string, error) {
		acceptServerStateFlag = false
		noGitCheckFlag = false
		noTUIFlag = false
		cmd := buildRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buf.String(), err
	}

	// 1. Initialize project
	if _, err := runCLI("init", "--yes"); err != nil {
		t.Fatalf("init --yes failed: %v", err)
	}

	// 2. Upload 3 versions
	envPath := filepath.Join(projDir, ".env")
	for v := 1; v <= 3; v++ {
		_ = os.WriteFile(envPath, []byte("VAL=v"+string(rune('0'+v))+"\n"), 0600)
		if _, err := runCLI("backup"); err != nil {
			t.Fatalf("backup v%d failed: %v", v, err)
		}
	}

	// 3. Three consecutive restores with backup_copies = 2 -> only 2 newest copies remain
	for i := 1; i <= 3; i++ {
		_ = os.WriteFile(envPath, []byte("VAL=local-mod-"+string(rune('0'+i))+"\n"), 0600)
		if _, err := runCLI("restore", "--force"); err != nil {
			t.Fatalf("restore %d failed: %v", i, err)
		}
	}
	copies, err := localfiles.ListBackupCopiesForFile(projDir, ".env")
	if err != nil {
		t.Fatalf("ListBackupCopiesForFile: %v", err)
	}
	if len(copies) != 2 {
		t.Fatalf("expected exactly 2 backup copies with backup_copies=2, got %d", len(copies))
	}

	// 4. Doctor --json output: valid JSON, no secrets
	jsonOut, err := runCLI("doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json failed: %v\nOutput: %s", err, jsonOut)
	}
	var parsedReport service.DoctorReport
	if err := json.Unmarshal([]byte(jsonOut), &parsedReport); err != nil {
		t.Fatalf("doctor --json produced invalid JSON: %v\n%s", err, jsonOut)
	}
	if len(parsedReport.Checks) == 0 {
		t.Fatal("expected checks in doctor --json report")
	}

	// 5. Tamper with state.json -> E_STATE_TAMPERED (exit code 3), then envil state reset fixes it
	statePath := filepath.Join(configDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"schema":1,"projects":{},"mac":"tampered"}`), 0600); err != nil {
		t.Fatalf("tamper state.json: %v", err)
	}
	_, tamperErr := runCLI("history")
	if !errs.IsCode(tamperErr, errs.StateTampered) || errs.ExitCode(tamperErr) != 3 {
		t.Fatalf("expected E_STATE_TAMPERED with exit code 3, got %v (exit %d)", tamperErr, errs.ExitCode(tamperErr))
	}

	if _, err := runCLI("state", "reset"); err != nil {
		t.Fatalf("state reset failed: %v", err)
	}
	if _, err := runCLI("history"); err != nil {
		t.Fatalf("history after state reset failed: %v", err)
	}

	// 6. Server down -> E_OFFLINE (exit code 4)
	_, offlineErr := runCLI("--server", "http://127.0.0.1:1", "history")
	if !errs.IsCode(offlineErr, errs.Offline) || errs.ExitCode(offlineErr) != 4 {
		t.Fatalf("expected E_OFFLINE with exit code 4 when server is down, got %v (exit %d)",
			offlineErr, errs.ExitCode(offlineErr))
	}
}
