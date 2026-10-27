package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/server"
	"github.com/ibrahimraimi/envil/internal/store"
)

func setupTestServer(t *testing.T) (*httptest.Server, store.Store) {
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

	return srv, st
}

func TestClientEndToEnd(t *testing.T) {
	ctx := context.Background()
	srv, _ := setupTestServer(t)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	c := NewClient(srv.URL, privateKey)

	// 1. Health check
	if err := c.HealthCheck(ctx); err != nil {
		t.Fatalf("health check: %v", err)
	}

	// 2. Register account
	account, err := c.RegisterAccount(ctx)
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	if account.ID == "" {
		t.Fatal("empty account id")
	}

	// 3. Upsert project
	projectID := uuid.New()
	meta := []byte(`{"name":"my-project"}`)
	project, err := c.UpsertProject(ctx, projectID, meta)
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	if project.ID != projectID {
		t.Fatal("project ID mismatch")
	}

	// 4. List projects
	projects, err := c.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	// 5. Create backup version 1 (expected_version = 0)
	blob := []byte("encrypted-payload-v1")
	backup, err := c.CreateBackup(ctx, projectID, 0, blob)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if backup.Version != 1 {
		t.Fatalf("expected version 1, got %d", backup.Version)
	}

	// 6. Conflicting backup (expected_version = 0 while version 1 exists)
	_, err = c.CreateBackup(ctx, projectID, 0, blob)
	if err != ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict, got: %v", err)
	}

	// 7. Get backup latest
	latest, err := c.GetBackup(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("get latest backup: %v", err)
	}
	if latest.Version != 1 {
		t.Fatalf("expected latest version 1, got %d", latest.Version)
	}

	// 8. Delete project
	if err := c.DeleteProject(ctx, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
}
