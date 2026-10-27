package store

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/google/uuid"
)

func generateTestAccountID(t *testing.T) [32]byte {
	t.Helper()
	var accountID [32]byte
	if _, err := rand.Read(accountID[:]); err != nil {
		t.Fatalf("generate account ID: %v", err)
	}
	return accountID
}

func TestStoreAccountLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	accountID := generateTestAccountID(t)
	publicKey := []byte("test-public-key-32-bytes-long!!")

	// Register account
	account, err := store.RegisterAccount(ctx, accountID, publicKey)
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	if account.ID != accountID {
		t.Fatal("account ID mismatch")
	}

	// Idempotent registration returns existing
	sameAccount, err := store.RegisterAccount(ctx, accountID, publicKey)
	if err != nil {
		t.Fatalf("idempotent register: %v", err)
	}
	if sameAccount.CreatedAt != account.CreatedAt {
		t.Fatal("expected identical account from idempotent register")
	}

	// Check existence
	hasAccount, err := store.HasAccount(ctx, accountID)
	if err != nil || !hasAccount {
		t.Fatalf("expected account to exist, got: %v", err)
	}

	// Delete account
	if err := store.DeleteAccount(ctx, accountID); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	hasDeleted, err := store.HasAccount(ctx, accountID)
	if err != nil || hasDeleted {
		t.Fatal("expected deleted account to not exist")
	}
}

func TestStoreProjectIsolationAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	accountA := generateTestAccountID(t)
	accountB := generateTestAccountID(t)

	_, _ = store.RegisterAccount(ctx, accountA, []byte("pub-a"))
	_, _ = store.RegisterAccount(ctx, accountB, []byte("pub-b"))

	projectID := uuid.New()
	meta := []byte("encrypted-meta")

	// Create project for Account A
	project, err := store.UpsertProject(ctx, projectID, accountA, meta)
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	if project.ID != projectID {
		t.Fatal("project ID mismatch")
	}

	// Account B cannot fetch Account A's project
	_, err = store.GetProject(ctx, projectID, accountB)
	if err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got: %v", err)
	}

	// Account B cannot upsert Account A's existing project ID
	_, err = store.UpsertProject(ctx, projectID, accountB, []byte("hijack"))
	if err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized on hijack attempt, got: %v", err)
	}

	// Account A lists project
	projects, err := store.ListProjects(ctx, accountA)
	if err != nil || len(projects) != 1 {
		t.Fatalf("expected 1 project for account A, got %d", len(projects))
	}

	// Soft delete project
	if err := store.SoftDeleteProject(ctx, projectID, accountA); err != nil {
		t.Fatalf("soft delete project: %v", err)
	}

	// Project no longer listed
	projectsAfterDelete, err := store.ListProjects(ctx, accountA)
	if err != nil || len(projectsAfterDelete) != 0 {
		t.Fatalf("expected 0 projects after delete, got %d", len(projectsAfterDelete))
	}
}

func TestStoreBackupVersioningAndConcurrency(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	accountID := generateTestAccountID(t)
	_, _ = store.RegisterAccount(ctx, accountID, []byte("pub"))

	projectID := uuid.New()
	_, _ = store.UpsertProject(ctx, projectID, accountID, []byte("meta"))

	// Version 1 backup: expectedVersion = 0
	blob1 := []byte("encrypted-env-v1")
	backup1, err := store.CreateBackup(ctx, projectID, accountID, 0, blob1)
	if err != nil {
		t.Fatalf("create backup 1: %v", err)
	}
	if backup1.Version != 1 {
		t.Fatalf("expected version 1, got %d", backup1.Version)
	}

	// Conflicting backup: expects 0 but current is 1
	_, err = store.CreateBackup(ctx, projectID, accountID, 0, []byte("conflict-blob"))
	if err != ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict, got: %v", err)
	}

	// Valid version 2 backup: expects 1
	blob2 := []byte("encrypted-env-v2")
	backup2, err := store.CreateBackup(ctx, projectID, accountID, 1, blob2)
	if err != nil {
		t.Fatalf("create backup 2: %v", err)
	}
	if backup2.Version != 2 {
		t.Fatalf("expected version 2, got %d", backup2.Version)
	}

	// Fetch latest (version 0)
	latest, err := store.GetBackup(ctx, projectID, accountID, 0)
	if err != nil {
		t.Fatalf("get latest backup: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("expected latest version 2, got %d", latest.Version)
	}

	// Fetch specific version 1
	v1, err := store.GetBackup(ctx, projectID, accountID, 1)
	if err != nil {
		t.Fatalf("get version 1 backup: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}
}

func TestStoreQuotaExceeded(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	accountID := generateTestAccountID(t)

	account, _ := store.RegisterAccount(ctx, accountID, []byte("pub"))
	account.QuotaBytes = 100 // Set small quota for test

	projectID := uuid.New()
	_, _ = store.UpsertProject(ctx, projectID, accountID, []byte("meta"))

	// Create backup exceeding quota
	oversizedBlob := make([]byte, 101)
	_, err := store.CreateBackup(ctx, projectID, accountID, 0, oversizedBlob)
	if err != ErrQuotaExceeded {
		t.Fatalf("expected ErrQuotaExceeded, got: %v", err)
	}
}

func TestStoreRetentionPrunesOldVersions(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	accountID := generateTestAccountID(t)
	account, _ := store.RegisterAccount(ctx, accountID, []byte("pub"))
	account.QuotaBytes = 50 * 1024 * 1024 // large quota for 25 backups

	projectID := uuid.New()
	_, _ = store.UpsertProject(ctx, projectID, accountID, []byte("meta"))

	// Create 25 backups
	for v := 0; v < 25; v++ {
		_, err := store.CreateBackup(ctx, projectID, accountID, v, []byte("blob"))
		if err != nil {
			t.Fatalf("create backup version %d: %v", v+1, err)
		}
	}

	headers, err := store.ListBackups(ctx, projectID, accountID)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(headers) != DEFAULT_RETENTION {
		t.Fatalf("expected %d retained versions, got %d", DEFAULT_RETENTION, len(headers))
	}

	// Oldest retained should be version 6, latest should be version 25
	if headers[0].Version != 25 {
		t.Fatalf("expected latest version 25, got %d", headers[0].Version)
	}
	if headers[len(headers)-1].Version != 6 {
		t.Fatalf("expected oldest retained version 6, got %d", headers[len(headers)-1].Version)
	}
}
