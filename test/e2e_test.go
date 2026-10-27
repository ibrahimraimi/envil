package test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/bundle"
	"github.com/ibrahimraimi/envil/internal/client"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/keystore"
	"github.com/ibrahimraimi/envil/internal/server"
	"github.com/ibrahimraimi/envil/internal/store"
)

func setupE2EServer(t *testing.T) *httptest.Server {
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

func TestE2EHappyPathAndRestore(t *testing.T) {
	ctx := context.Background()
	srv := setupE2EServer(t)

	workingDir := t.TempDir()
	keysPath := filepath.Join(workingDir, "keys.json")
	ks := keystore.NewFileStore(keysPath)

	// 1. Setup account
	phrase, err := crypto.NewPhrase()
	if err != nil {
		t.Fatalf("new phrase: %v", err)
	}
	keys, err := crypto.KeysFromPhrase(phrase)
	if err != nil {
		t.Fatalf("keys from phrase: %v", err)
	}
	if err := ks.SaveKeys(keys); err != nil {
		t.Fatalf("save keys: %v", err)
	}

	apiClient := client.NewClient(srv.URL, keys.Signing)
	if _, err := apiClient.RegisterAccount(ctx); err != nil {
		t.Fatalf("register account: %v", err)
	}

	// 2. Create project
	projectID := crypto.BuildAAD(keys.AccountID(), "proj", 0) // sample deterministic bytes
	_ = projectID
	randomProjectID := crypto.Keys{}
	_ = randomProjectID

	derivedProjKey, err := keys.ProjectKey("project-alpha")
	if err != nil {
		t.Fatalf("derive project key: %v", err)
	}

	meta := &bundle.ProjectMetadata{Name: "project-alpha"}
	metaBytes, _ := bundle.EncodeProjectMetadata(meta)
	aadMeta := crypto.BuildAAD(keys.AccountID(), "project-alpha", 0)
	sealedMeta, err := crypto.Seal(derivedProjKey, metaBytes, aadMeta)
	if err != nil {
		t.Fatalf("seal meta: %v", err)
	}

	decryptedMeta, err := crypto.Open(derivedProjKey, sealedMeta, aadMeta)
	if err != nil {
		t.Fatalf("open meta: %v", err)
	}
	parsedMeta, err := bundle.DecodeProjectMetadata(decryptedMeta)
	if err != nil || parsedMeta.Name != "project-alpha" {
		t.Fatal("project metadata roundtrip failed in e2e")
	}

	// 3. Prepare .env in workingDir
	envContent := "DATABASE_URL=postgres://localhost:5432/app\nSECRET_KEY=alpha-secret-123\n"
	envPath := filepath.Join(workingDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// 4. Bundle and seal
	entries := []bundle.FileEntry{
		{Name: ".env", Content: envContent, SHA256: bundle.ComputeContentHash(envContent)},
	}
	rawBundle, err := bundle.CreateBundle(entries, "envil/"+config.CLI_VERSION)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	bundleBytes, err := bundle.EncodeBundle(rawBundle)
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	aad := crypto.BuildAAD(keys.AccountID(), "project-alpha", 1)
	sealedBackup, err := crypto.Seal(derivedProjKey, bundleBytes, aad)
	if err != nil {
		t.Fatalf("seal backup: %v", err)
	}

	// 5. Open and restore byte-identically
	decryptedBytes, err := crypto.Open(derivedProjKey, sealedBackup, aad)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	restoredBundle, err := bundle.DecodeBundle(decryptedBytes)
	if err != nil {
		t.Fatalf("decode restored bundle: %v", err)
	}

	if len(restoredBundle.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(restoredBundle.Files))
	}
	if restoredBundle.Files[0].Content != envContent {
		t.Fatal("restored content does not match original byte-identically")
	}
}

func TestE2ERecoveryAcrossMachines(t *testing.T) {
	phrase, err := crypto.NewPhrase()
	if err != nil {
		t.Fatalf("generate phrase: %v", err)
	}

	// Machine 1: Setup keys and create backup
	keysMachine1, err := crypto.KeysFromPhrase(phrase)
	if err != nil {
		t.Fatalf("machine 1 keys: %v", err)
	}

	projectKey1, err := keysMachine1.ProjectKey("proj-123")
	if err != nil {
		t.Fatalf("derive project key machine 1: %v", err)
	}

	originalEnv := "API_KEY=machine-1-secret\n"
	aad := crypto.BuildAAD(keysMachine1.AccountID(), "proj-123", 1)
	blob, err := crypto.Seal(projectKey1, []byte(originalEnv), aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Machine 2: Enter 24-word phrase on new machine
	keysMachine2, err := crypto.KeysFromPhrase(phrase)
	if err != nil {
		t.Fatalf("machine 2 keys from phrase: %v", err)
	}

	projectKey2, err := keysMachine2.ProjectKey("proj-123")
	if err != nil {
		t.Fatalf("derive project key machine 2: %v", err)
	}

	aad2 := crypto.BuildAAD(keysMachine2.AccountID(), "proj-123", 1)
	decryptedOnMachine2, err := crypto.Open(projectKey2, blob, aad2)
	if err != nil {
		t.Fatalf("machine 2 open: %v", err)
	}

	if string(decryptedOnMachine2) != originalEnv {
		t.Fatal("recovered secret on Machine 2 does not match Machine 1")
	}
}

func TestE2EInvalidPhraseRejection(t *testing.T) {
	badPhrase := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon wrongword"
	_, err := crypto.KeysFromPhrase(badPhrase)
	if err == nil {
		t.Fatal("expected KeysFromPhrase to fail for corrupted word, got nil")
	}
}
