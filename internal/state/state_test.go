package state

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ibrahimraimi/envil/internal/crypto"
	"github.com/ibrahimraimi/envil/internal/errs"
)

const (
	mnemonicAccountA = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	mnemonicAccountB = "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo vote"
)

func deriveTestStateKey(t *testing.T, phrase string) []byte {
	t.Helper()
	keys, err := crypto.KeysFromPhrase(phrase)
	if err != nil {
		t.Fatalf("derive keys: %v", err)
	}
	stateKey, err := keys.StateKey()
	if err != nil {
		t.Fatalf("derive state key: %v", err)
	}
	return stateKey
}

func TestCheckVerdicts(t *testing.T) {
	hashA := ComputeBlobHash([]byte("ciphertext-a"))
	hashB := ComputeBlobHash([]byte("ciphertext-b"))

	cases := []struct {
		name         string
		prev         *Entry
		serverLatest int
		serverHash   string
		expected     Verdict
	}{
		{
			name:         "New project",
			prev:         nil,
			serverLatest: 3,
			serverHash:   "",
			expected:     FirstSeen,
		},
		{
			name:         "Normal progress",
			prev:         &Entry{LatestVersion: 3, BlobSHA256: hashA},
			serverLatest: 4,
			serverHash:   "",
			expected:     Advanced,
		},
		{
			name:         "No change without server hash",
			prev:         &Entry{LatestVersion: 3, BlobSHA256: hashA},
			serverLatest: 3,
			serverHash:   "",
			expected:     OK,
		},
		{
			name:         "No change with matching hash",
			prev:         &Entry{LatestVersion: 3, BlobSHA256: hashA},
			serverLatest: 3,
			serverHash:   hashA,
			expected:     OK,
		},
		{
			name:         "Rollback",
			prev:         &Entry{LatestVersion: 5, BlobSHA256: hashA},
			serverLatest: 3,
			serverHash:   hashA,
			expected:     Rollback,
		},
		{
			name:         "Fork",
			prev:         &Entry{LatestVersion: 5, BlobSHA256: hashA},
			serverLatest: 5,
			serverHash:   hashB,
			expected:     Fork,
		},
		{
			name:         "Hash unknown locally",
			prev:         &Entry{LatestVersion: 5, BlobSHA256: ""},
			serverLatest: 5,
			serverHash:   hashA,
			expected:     OK,
		},
		{
			name:         "Zero values",
			prev:         &Entry{LatestVersion: 0, BlobSHA256: ""},
			serverLatest: 0,
			serverHash:   "",
			expected:     OK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Check(tc.prev, tc.serverLatest, tc.serverHash)
			if got != tc.expected {
				t.Fatalf("Check() = %v (%s), want %v (%s)", got, got.String(), tc.expected, tc.expected.String())
			}
		})
	}
}

func TestStoreRoundTripAndMissingFile(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "envil", "state.json")
	stateKey := deriveTestStateKey(t, mnemonicAccountA)
	store := NewStore(statePath, stateKey)

	// 1. Missing file loads as empty without error
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load on missing file failed: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0 entries for missing file, got %d", len(loaded))
	}

	// 2. Round trip: save then load returns identical data
	seenTime := time.Date(2026, 10, 28, 10, 15, 0, 0, time.UTC)
	projID := "3f2c9a6e-0000-0000-0000-000000000000"
	expectedEntry := Entry{
		LatestVersion: 5,
		BlobSHA256:    ComputeBlobHash([]byte("blob-v5")),
		SeenAt:        seenTime,
	}

	if err := store.Put(projID, expectedEntry); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	gotEntry, err := store.Get(projID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if gotEntry == nil {
		t.Fatal("expected non-nil entry after Put")
	}
	if gotEntry.LatestVersion != expectedEntry.LatestVersion ||
		gotEntry.BlobSHA256 != expectedEntry.BlobSHA256 ||
		!gotEntry.SeenAt.Equal(expectedEntry.SeenAt) {
		t.Fatalf("entry mismatch: got %+v, want %+v", *gotEntry, expectedEntry)
	}

	// 3. Verify permissions on Unix (file 0600, dir 0700)
	if runtime.GOOS != "windows" {
		fileInfo, err := os.Stat(statePath)
		if err != nil {
			t.Fatalf("stat state file: %v", err)
		}
		if fileInfo.Mode().Perm() != 0600 {
			t.Fatalf("expected file mode 0600, got %o", fileInfo.Mode().Perm())
		}

		dirInfo, err := os.Stat(filepath.Dir(statePath))
		if err != nil {
			t.Fatalf("stat state dir: %v", err)
		}
		if dirInfo.Mode().Perm() != 0700 {
			t.Fatalf("expected dir mode 0700, got %o", dirInfo.Mode().Perm())
		}
	}
}

func TestStoreByteFlipTamperDetection(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	stateKey := deriveTestStateKey(t, mnemonicAccountA)
	store := NewStore(statePath, stateKey)

	if err := store.Put("3f2c9a6e-0000-0000-0000-000000000000", Entry{
		LatestVersion: 5,
		BlobSHA256:    ComputeBlobHash([]byte("blob-v5")),
		SeenAt:        time.Date(2026, 10, 28, 10, 15, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	originalBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	for idx := range originalBytes {
		tampered := bytes.Clone(originalBytes)
		tampered[idx] ^= 0x01

		if err := os.WriteFile(statePath, tampered, 0600); err != nil {
			t.Fatalf("write tampered state file: %v", err)
		}

		_, loadErr := store.Load()
		if loadErr == nil {
			t.Fatalf("expected error when flipping byte at index %d (%q), got nil", idx, originalBytes[idx])
		}
		if !errs.IsCode(loadErr, errs.StateTampered) {
			t.Fatalf("expected E_STATE_TAMPERED at byte index %d, got: %v", idx, loadErr)
		}
	}
}

func TestStoreCrossAccountVerificationFails(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	keyA := deriveTestStateKey(t, mnemonicAccountA)
	keyB := deriveTestStateKey(t, mnemonicAccountB)

	storeA := NewStore(statePath, keyA)
	if err := storeA.Put("proj-1", Entry{
		LatestVersion: 2,
		BlobSHA256:    ComputeBlobHash([]byte("cipher")),
		SeenAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("storeA Put: %v", err)
	}

	storeB := NewStore(statePath, keyB)
	_, err := storeB.Load()
	if err == nil {
		t.Fatal("expected storeB Load to fail verification on file written by storeA")
	}
	if !errs.IsCode(err, errs.StateTampered) {
		t.Fatalf("expected E_STATE_TAMPERED for cross-account load, got: %v", err)
	}
}

func TestStoreAtomicWriteCrashSimulation(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	stateKey := deriveTestStateKey(t, mnemonicAccountA)
	store := NewStore(statePath, stateKey)

	if err := store.Put("proj-1", Entry{
		LatestVersion: 3,
		BlobSHA256:    "hash-v3",
		SeenAt:        time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("initial Put: %v", err)
	}

	// Simulate crash between temporary file write and rename
	simulatedCrash := errors.New("simulated process crash before rename")
	store.beforeRename = func(tmpPath string) error {
		return simulatedCrash
	}

	err := store.Put("proj-1", Entry{
		LatestVersion: 4,
		BlobSHA256:    "hash-v4",
		SeenAt:        time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, simulatedCrash) {
		t.Fatalf("expected simulatedCrash error, got: %v", err)
	}

	store.beforeRename = nil
	entry, err := store.Get("proj-1")
	if err != nil {
		t.Fatalf("Get after simulated crash failed: %v", err)
	}
	if entry == nil || entry.LatestVersion != 3 || entry.BlobSHA256 != "hash-v3" {
		t.Fatalf("old state file was not intact after crash: got %+v", entry)
	}
}

func TestStoreConcurrentSaves(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	stateKey := deriveTestStateKey(t, mnemonicAccountA)

	const workers = 12
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			store := NewStore(statePath, stateKey)
			projID := fmt.Sprintf("proj-%02d", workerID)
			err := store.Put(projID, Entry{
				LatestVersion: workerID + 1,
				BlobSHA256:    ComputeBlobHash([]byte(projID)),
				SeenAt:        time.Now().UTC().Truncate(time.Second),
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent Put failed: %v", err)
	}

	verifier := NewStore(statePath, stateKey)
	projects, err := verifier.Load()
	if err != nil {
		t.Fatalf("Load after concurrent writes failed: %v", err)
	}
	if len(projects) != workers {
		t.Fatalf("expected %d projects after concurrent writes, got %d", workers, len(projects))
	}
}

func TestStoreUnknownSchemaVersion(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	stateKey := deriveTestStateKey(t, mnemonicAccountA)

	futureSchema := 99
	projects := map[string]Entry{}
	macBytes, err := computeMAC(stateKey, futureSchema, projects)
	if err != nil {
		t.Fatalf("computeMAC: %v", err)
	}
	rawBytes, err := marshalStateFile(futureSchema, projects, base64.StdEncoding.EncodeToString(macBytes))
	if err != nil {
		t.Fatalf("marshalStateFile: %v", err)
	}
	if err := os.WriteFile(statePath, rawBytes, 0600); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	store := NewStore(statePath, stateKey)
	_, err = store.Load()
	if err == nil {
		t.Fatal("expected error for unknown schema version, got nil")
	}
	if !errors.Is(err, ErrUnknownSchema) {
		t.Fatalf("expected ErrUnknownSchema, got: %v", err)
	}
}

func TestStoreReset(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	stateKey := deriveTestStateKey(t, mnemonicAccountA)
	store := NewStore(statePath, stateKey)

	_ = store.Put("proj-1", Entry{LatestVersion: 2, SeenAt: time.Now().UTC()})
	_ = store.Put("proj-2", Entry{LatestVersion: 4, SeenAt: time.Now().UTC()})

	// Reset single project
	if err := store.Reset("proj-1"); err != nil {
		t.Fatalf("Reset proj-1: %v", err)
	}
	e1, err := store.Get("proj-1")
	if err != nil || e1 != nil {
		t.Fatalf("expected proj-1 to be removed, got %+v (err=%v)", e1, err)
	}
	e2, err := store.Get("proj-2")
	if err != nil || e2 == nil || e2.LatestVersion != 4 {
		t.Fatalf("expected proj-2 to remain intact, got %+v (err=%v)", e2, err)
	}

	// Reset all projects
	if err := store.Reset(""); err != nil {
		t.Fatalf("Reset all: %v", err)
	}
	all, err := store.Load()
	if err != nil || len(all) != 0 {
		t.Fatalf("expected empty state after Reset all, got %+v (err=%v)", all, err)
	}
}

