package keystore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahimraimi/envil/internal/crypto"
)

func TestFileStoreLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	keysPath := filepath.Join(tempDir, "keys.json")
	store := NewFileStore(keysPath)

	// 1. Initial state: keys not found
	_, err := store.GetKeys()
	if err != ErrKeysNotFound {
		t.Fatalf("expected ErrKeysNotFound, got: %v", err)
	}

	// 2. Generate and save keys
	phrase, err := crypto.NewPhrase()
	if err != nil {
		t.Fatalf("new phrase: %v", err)
	}
	originalKeys, err := crypto.KeysFromPhrase(phrase)
	if err != nil {
		t.Fatalf("keys from phrase: %v", err)
	}

	if err := store.SaveKeys(originalKeys); err != nil {
		t.Fatalf("save keys: %v", err)
	}

	// 3. Verify file permissions are 0600
	info, err := os.Stat(keysPath)
	if err != nil {
		t.Fatalf("stat keys file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected file permission 0600, got: %o", info.Mode().Perm())
	}

	// 4. Retrieve and verify keys match exactly
	loadedKeys, err := store.GetKeys()
	if err != nil {
		t.Fatalf("get keys: %v", err)
	}

	if !bytes.Equal(loadedKeys.SigningSeed(), originalKeys.SigningSeed()) {
		t.Fatal("signing seed mismatch after retrieval")
	}
	if !bytes.Equal(loadedKeys.EncryptionRoot(), originalKeys.EncryptionRoot()) {
		t.Fatal("encryption root mismatch after retrieval")
	}
	if loadedKeys.AccountID() != originalKeys.AccountID() {
		t.Fatal("account ID mismatch after retrieval")
	}

	// 5. Delete keys
	if err := store.DeleteKeys(); err != nil {
		t.Fatalf("delete keys: %v", err)
	}

	// 6. Verify deleted
	_, err = store.GetKeys()
	if err != ErrKeysNotFound {
		t.Fatalf("expected ErrKeysNotFound after delete, got: %v", err)
	}
}
