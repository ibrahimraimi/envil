package crypto

import (
	"bytes"
	"testing"
)

func FuzzSealOpen(f *testing.F) {
	f.Add([]byte("DATABASE_URL=postgres://localhost:5432/app"), []byte("project-alpha/v1"))
	f.Fuzz(func(t *testing.T, plaintext, aad []byte) {
		encryptionKey := make([]byte, KEY_SIZE)
		blob, err := Seal(encryptionKey, plaintext, aad)
		if err != nil {
			t.Fatalf("seal failed: %v", err)
		}

		decrypted, err := Open(encryptionKey, blob, aad)
		if err != nil {
			t.Fatalf("open failed: %v", err)
		}

		if !bytes.Equal(decrypted, plaintext) {
			t.Fatal("round trip decrypted plaintext does not match input")
		}
	})
}

func FuzzOpenGarbage(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, arbitraryBlob []byte) {
		arbitraryKey := make([]byte, KEY_SIZE)
		// Open must handle arbitrary corrupt input cleanly without panicking.
		_, _ = Open(arbitraryKey, arbitraryBlob, nil)
	})
}
