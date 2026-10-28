package service

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/server"
	"github.com/ibrahimraimi/envil/internal/store"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := server.Config{
		RateLimitRPS:   1000,
		RateLimitBurst: 2000,
		MaxBlobBytes:   1024 * 1024,
	}
	st := store.NewMemoryStore()
	nonces := auth.NewMemoryNonceTracker()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := server.BuildRouter(cfg, st, nonces, logger)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func TestServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	srv := setupTestServer(t)

	tempDir := t.TempDir()
	keysPath := filepath.Join(tempDir, "keys.json")
	ks := keystore.NewFileStore(keysPath)

	svc := NewDefaultService(ks, func() string { return srv.URL })

	hasKeys, err := svc.HasKeys(ctx)
	if err != nil || hasKeys {
		t.Fatalf("expected hasKeys=false, got %v (err: %v)", hasKeys, err)
	}

	phrase, err := svc.NewPhrase(ctx)
	if err != nil {
		t.Fatalf("new phrase: %v", err)
	}

	if err := svc.CompleteSetup(ctx, phrase); err != nil {
		t.Fatalf("complete setup: %v", err)
	}

	hasKeys, err = svc.HasKeys(ctx)
	if err != nil || !hasKeys {
		t.Fatalf("expected hasKeys=true, got %v (err: %v)", hasKeys, err)
	}

	fingerprint, err := svc.AccountFingerprint(ctx)
	if err != nil || len(fingerprint) == 0 {
		t.Fatalf("expected valid fingerprint, got %q (err: %v)", fingerprint, err)
	}

	projectDir := filepath.Join(tempDir, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	projectID, err := svc.LinkCurrentDirectory(ctx, projectDir, "my-app")
	if err != nil {
		t.Fatalf("link project: %v", err)
	}

	projects, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "my-app" {
		t.Fatalf("unexpected projects list: %+v", projects)
	}

	// Backup
	envPath := filepath.Join(projectDir, ".env")
	if err := os.WriteFile(envPath, []byte("API_KEY=secret123\nPORT=3000\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	_ = os.Chdir(projectDir)

	version, err := svc.Backup(ctx, projectID, []string{".env"})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}

	history, err := svc.History(ctx, projectID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || history[0].Number != 1 {
		t.Fatalf("unexpected history: %+v", history)
	}

	// Diff before modification
	diffs, err := svc.Diff(ctx, projectID, 1)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf("expected 0 diffs for identical files, got %+v", diffs)
	}

	// Modify local file
	if err := os.WriteFile(envPath, []byte("API_KEY=secret999\nNEW_VAR=hello\n"), 0600); err != nil {
		t.Fatalf("modify .env: %v", err)
	}

	diffs, err = svc.Diff(ctx, projectID, 1)
	if err != nil {
		t.Fatalf("diff after modification: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatalf("expected diff entries, got none")
	}

	// Test reveal
	localVal, remoteVal, err := svc.RevealDiffValue(ctx, projectID, 1, ".env", "API_KEY")
	if err != nil {
		t.Fatalf("reveal diff value: %v", err)
	}
	if localVal != "secret999" || remoteVal != "secret123" {
		t.Fatalf("expected local=secret999, remote=secret123; got local=%s, remote=%s", localVal, remoteVal)
	}

	// Restore with force
	if err := svc.Restore(ctx, projectID, 1, true); err != nil {
		t.Fatalf("restore: %v", err)
	}

	restoredContent, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read restored .env: %v", err)
	}
	if string(restoredContent) != "API_KEY=secret123\nPORT=3000\n" {
		t.Fatalf("unexpected restored content: %s", string(restoredContent))
	}

	// Doctor report
	report, err := svc.Doctor(ctx)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(report.Checks) == 0 {
		t.Fatalf("expected doctor checks, got 0")
	}

	// Delete project
	if err := svc.DeleteProject(ctx, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	projects, err = svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects after delete: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected 0 projects after deletion, got %d", len(projects))
	}
}

func TestServiceRecovery(t *testing.T) {
	ctx := context.Background()
	srv := setupTestServer(t)

	// Machine A
	dirA := t.TempDir()
	ksA := keystore.NewFileStore(filepath.Join(dirA, "keys.json"))
	svcA := NewDefaultService(ksA, func() string { return srv.URL })

	phrase, err := svcA.NewPhrase(ctx)
	if err != nil {
		t.Fatalf("new phrase: %v", err)
	}
	if err := svcA.CompleteSetup(ctx, phrase); err != nil {
		t.Fatalf("setup A: %v", err)
	}

	appDir := filepath.Join(dirA, "backend")
	_ = os.MkdirAll(appDir, 0755)
	projectID, err := svcA.LinkCurrentDirectory(ctx, appDir, "backend")
	if err != nil {
		t.Fatalf("link A: %v", err)
	}

	// Machine B
	dirB := t.TempDir()
	ksB := keystore.NewFileStore(filepath.Join(dirB, "keys.json"))
	svcB := NewDefaultService(ksB, func() string { return srv.URL })

	if err := svcB.Recover(ctx, phrase); err != nil {
		t.Fatalf("recover B: %v", err)
	}

	projectsB, err := svcB.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(projectsB) != 1 || projectsB[0].ID != projectID || projectsB[0].Name != "backend" {
		t.Fatalf("machine B did not find project: %+v", projectsB)
	}
}

func TestServiceValidationErrors(t *testing.T) {
	ctx := context.Background()
	srv := setupTestServer(t)

	dir := t.TempDir()
	ks := keystore.NewFileStore(filepath.Join(dir, "keys.json"))
	svc := NewDefaultService(ks, func() string { return srv.URL })

	if err := svc.CompleteSetup(ctx, "invalid phrase with not enough words"); err == nil {
		t.Fatalf("expected error for invalid phrase on setup, got nil")
	}

	if err := svc.Recover(ctx, "invalid phrase"); err == nil {
		t.Fatalf("expected error for invalid phrase on recover, got nil")
	}
}

