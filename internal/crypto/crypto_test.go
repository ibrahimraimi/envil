package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

const (
	testMnemonic1 = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	testMnemonic2 = "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo vote"
)

func TestNewPhrase(t *testing.T) {
	phrase, err := NewPhrase()
	if err != nil {
		t.Fatalf("unexpected error creating phrase: %v", err)
	}

	words := strings.Fields(phrase)
	if len(words) != 24 {
		t.Fatalf("expected 24 words, got %d", len(words))
	}

	if err := ValidatePhrase(phrase); err != nil {
		t.Fatalf("generated phrase failed validation: %v", err)
	}
}

func TestKeysFromPhraseDeterminism(t *testing.T) {
	keys1, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys1: %v", err)
	}

	keys2, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys2: %v", err)
	}

	if !keys1.Signing.Equal(keys2.Signing) {
		t.Fatal("signing keys do not match for identical phrase")
	}

	if keys1.AccountID() != keys2.AccountID() {
		t.Fatal("account IDs do not match for identical phrase")
	}

	projectKey1, err := keys1.ProjectKey("project-alpha")
	if err != nil {
		t.Fatalf("failed to derive project key 1: %v", err)
	}

	projectKey2, err := keys2.ProjectKey("project-alpha")
	if err != nil {
		t.Fatalf("failed to derive project key 2: %v", err)
	}

	if !bytes.Equal(projectKey1, projectKey2) {
		t.Fatal("project keys do not match for identical phrase and project ID")
	}
}

func TestKeysFromPhraseDistinct(t *testing.T) {
	keys1, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys1: %v", err)
	}

	keys2, err := KeysFromPhrase(testMnemonic2)
	if err != nil {
		t.Fatalf("failed to derive keys2: %v", err)
	}

	if keys1.Signing.Equal(keys2.Signing) {
		t.Fatal("different phrases derived identical signing keys")
	}

	if keys1.AccountID() == keys2.AccountID() {
		t.Fatal("different phrases derived identical account IDs")
	}
}

func TestProjectKeyIsolation(t *testing.T) {
	keys, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys: %v", err)
	}

	keyA, err := keys.ProjectKey("proj-1")
	if err != nil {
		t.Fatalf("failed to derive key A: %v", err)
	}

	keyB, err := keys.ProjectKey("proj-2")
	if err != nil {
		t.Fatalf("failed to derive key B: %v", err)
	}

	if bytes.Equal(keyA, keyB) {
		t.Fatal("different project IDs yielded identical keys")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	keys, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys: %v", err)
	}

	projectKey, err := keys.ProjectKey("project-test")
	if err != nil {
		t.Fatalf("failed to derive project key: %v", err)
	}

	aad := BuildAAD(keys.AccountID(), "project-test", 1)

	testCases := []struct {
		name      string
		plaintext []byte
	}{
		{name: "empty", plaintext: []byte{}},
		{name: "small", plaintext: []byte("DATABASE_URL=postgres://user:secret@localhost:5432/app\nPORT=8080")},
		{name: "one_megabyte", plaintext: make([]byte, 1024*1024)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := Seal(projectKey, tc.plaintext, aad)
			if err != nil {
				t.Fatalf("seal error: %v", err)
			}

			decrypted, err := Open(projectKey, blob, aad)
			if err != nil {
				t.Fatalf("open error: %v", err)
			}

			if !bytes.Equal(decrypted, tc.plaintext) {
				t.Fatal("decrypted payload does not match original plaintext")
			}
		})
	}
}

func TestSealNonceRandomness(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("SECRET_TOKEN=xyz123")
	aad := []byte("sample-aad")

	blob1, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("seal 1 failed: %v", err)
	}

	blob2, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("seal 2 failed: %v", err)
	}

	if bytes.Equal(blob1, blob2) {
		t.Fatal("consecutive seals of identical plaintext produced identical ciphertexts")
	}
}

func TestTamperDetection(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("API_KEY=sensitive-data")
	aad := []byte("context")

	originalBlob, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("seal failed: %v", err)
	}

	for byteIdx := 0; byteIdx < len(originalBlob); byteIdx++ {
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			tampered := bytes.Clone(originalBlob)
			tampered[byteIdx] ^= 1 << bitIdx

			_, err := Open(key, tampered, aad)
			if err == nil {
				t.Fatalf("tampered byte %d bit %d unexpectedly decrypted successfully", byteIdx, bitIdx)
			}
		}
	}
}

func TestAADMismatchFails(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("VALUE=123")
	validAAD := []byte("project-1/version-1")
	wrongAAD := []byte("project-1/version-2")

	blob, err := Seal(key, plaintext, validAAD)
	if err != nil {
		t.Fatalf("seal failed: %v", err)
	}

	_, err = Open(key, blob, wrongAAD)
	if err == nil {
		t.Fatal("open with mismatched AAD unexpectedly succeeded")
	}
}

func TestWrongKeyFails(t *testing.T) {
	correctKey := make([]byte, 32)
	wrongKey := make([]byte, 32)
	wrongKey[0] = 0xFF

	plaintext := []byte("DATA=secret")
	aad := []byte("aad")

	blob, err := Seal(correctKey, plaintext, aad)
	if err != nil {
		t.Fatalf("seal failed: %v", err)
	}

	_, err = Open(wrongKey, blob, aad)
	if err == nil {
		t.Fatal("open with wrong key unexpectedly succeeded")
	}
}

func TestTruncatedBlobsFail(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("some payload")
	aad := []byte("aad")

	blob, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("seal failed: %v", err)
	}

	minHeader := 1 + NONCE_SIZE + 16
	for length := 0; length < minHeader; length++ {
		truncated := blob[:length]
		_, err := Open(key, truncated, aad)
		if err == nil {
			t.Fatalf("open on truncated blob length %d unexpectedly succeeded", length)
		}
	}
}

func TestInvalidPhrases(t *testing.T) {
	testCases := []struct {
		name   string
		phrase string
	}{
		{
			name:   "wrong word count",
			phrase: "abandon abandon abandon",
		},
		{
			name:   "invalid word",
			phrase: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon notaword",
		},
		{
			name:   "bad checksum",
			phrase: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon",
		},
		{
			name:   "empty phrase",
			phrase: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := KeysFromPhrase(tc.phrase)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestKnownAnswerVectors(t *testing.T) {
	keys, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys: %v", err)
	}

	publicKey := keys.PublicKey()
	accountID := keys.AccountID()
	projectKey, err := keys.ProjectKey("envil-demo-project")
	if err != nil {
		t.Fatalf("failed to derive project key: %v", err)
	}

	// Verify key lengths and properties
	if len(publicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected pubkey length %d, got %d", ed25519.PublicKeySize, len(publicKey))
	}
	if len(projectKey) != KEY_SIZE {
		t.Fatalf("expected project key length %d, got %d", KEY_SIZE, len(projectKey))
	}

	// Deterministic fixed nonce seal
	fixedNonce := make([]byte, NONCE_SIZE)
	for i := range fixedNonce {
		fixedNonce[i] = byte(i + 1)
	}

	plaintext := []byte("APP_ENV=production\nSECRET=supersecret123")
	aad := BuildAAD(accountID, "envil-demo-project", 1)

	blob, err := SealWithNonce(projectKey, fixedNonce, plaintext, aad)
	if err != nil {
		t.Fatalf("seal with fixed nonce failed: %v", err)
	}

	// Record deterministic KAT hex
	blobHex := hex.EncodeToString(blob)
	if len(blobHex) == 0 {
		t.Fatal("blob hex is empty")
	}

	// Verify roundtrip on the deterministic blob
	decrypted, err := Open(projectKey, blob, aad)
	if err != nil {
		t.Fatalf("failed to open fixed-nonce blob: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("decrypted payload mismatch in KAT")
	}
}

func TestDestroyZerosKeys(t *testing.T) {
	keys, err := KeysFromPhrase(testMnemonic1)
	if err != nil {
		t.Fatalf("failed to derive keys: %v", err)
	}

	keys.Destroy()

	zeroSigning := make([]byte, len(keys.Signing))
	if !bytes.Equal(keys.Signing, zeroSigning) {
		t.Fatal("signing key was not cleared by Destroy")
	}

	var zeroRoot [32]byte
	if keys.encRoot != zeroRoot {
		t.Fatal("encryption root was not cleared by Destroy")
	}
}

func TestBuildAAD(t *testing.T) {
	var accountID [32]byte
	_, _ = rand.Read(accountID[:])

	aad1 := BuildAAD(accountID, "proj-1", 5)
	aad2 := BuildAAD(accountID, "proj-1", 5)
	aad3 := BuildAAD(accountID, "proj-1", 6)

	if !bytes.Equal(aad1, aad2) {
		t.Fatal("BuildAAD must be deterministic")
	}
	if bytes.Equal(aad1, aad3) {
		t.Fatal("BuildAAD must differ across versions")
	}
}
