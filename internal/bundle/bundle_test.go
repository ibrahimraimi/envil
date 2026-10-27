package bundle

import (
	"strings"
	"testing"
)

func TestBundleCreateAndDecodeRoundTrip(t *testing.T) {
	entries := []FileEntry{
		{
			Name:    ".env",
			Content: "DATABASE_URL=postgres://localhost:5432/app\nAPI_KEY=secret",
		},
		{
			Name:    ".env.local",
			Content: "DEBUG=true",
		},
	}

	createdBundle, err := CreateBundle(entries, "envil/0.1.0")
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	encodedBytes, err := EncodeBundle(createdBundle)
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	decodedBundle, err := DecodeBundle(encodedBytes)
	if err != nil {
		t.Fatalf("decode bundle: %v", err)
	}

	if decodedBundle.Schema != CURRENT_SCHEMA_VERSION {
		t.Fatalf("expected schema %d, got %d", CURRENT_SCHEMA_VERSION, decodedBundle.Schema)
	}
	if len(decodedBundle.Files) != len(entries) {
		t.Fatalf("expected %d files, got %d", len(entries), len(decodedBundle.Files))
	}
	for i := range entries {
		if decodedBundle.Files[i].Content != entries[i].Content {
			t.Fatalf("file content mismatch for %s", entries[i].Name)
		}
	}
}

func TestBundleFileHashTamperFails(t *testing.T) {
	entries := []FileEntry{
		{
			Name:    ".env",
			Content: "PORT=8080",
			SHA256:  "tampered-hash-that-does-not-match",
		},
	}

	_, err := CreateBundle(entries, "envil/0.1.0")
	if err == nil {
		t.Fatal("expected CreateBundle to fail with tampered hash, got nil")
	}
}

func TestBundleSizeLimit(t *testing.T) {
	largeContent := strings.Repeat("A", MAX_BUNDLE_BYTES+10)
	entries := []FileEntry{
		{
			Name:    ".env",
			Content: largeContent,
		},
	}

	createdBundle, err := CreateBundle(entries, "envil/0.1.0")
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	_, err = EncodeBundle(createdBundle)
	if err != ErrBundleTooLarge {
		t.Fatalf("expected ErrBundleTooLarge, got: %v", err)
	}
}

func TestProjectMetadataRoundTrip(t *testing.T) {
	meta := &ProjectMetadata{
		Name:      "my-service",
		GitRemote: "git@github.com:org/repo.git",
	}

	encodedBytes, err := EncodeProjectMetadata(meta)
	if err != nil {
		t.Fatalf("encode metadata: %v", err)
	}

	decodedMeta, err := DecodeProjectMetadata(encodedBytes)
	if err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	if decodedMeta.Name != meta.Name || decodedMeta.GitRemote != meta.GitRemote {
		t.Fatal("metadata mismatch after decode")
	}
}

func TestDiffEnvContent(t *testing.T) {
	local := `
# Local configuration
PORT=3000
DATABASE_URL="postgres://localhost:5432/dev"
ONLY_LOCAL=active
`
	remote := `
PORT=8080
DATABASE_URL="postgres://localhost:5432/dev"
ONLY_REMOTE=deployed
`
	fileDiff := DiffEnvContent(".env", local, remote)

	changes := make(map[string]DiffChange)
	for _, entry := range fileDiff.Entries {
		changes[entry.Key] = entry.Change
	}

	if changes["DATABASE_URL"] != ChangeEqual {
		t.Fatalf("expected DATABASE_URL to be EQUAL, got %s", changes["DATABASE_URL"])
	}
	if changes["PORT"] != ChangeModified {
		t.Fatalf("expected PORT to be MODIFIED, got %s", changes["PORT"])
	}
	if changes["ONLY_LOCAL"] != ChangeRemoved {
		t.Fatalf("expected ONLY_LOCAL to be REMOVED, got %s", changes["ONLY_LOCAL"])
	}
	if changes["ONLY_REMOTE"] != ChangeAdded {
		t.Fatalf("expected ONLY_REMOTE to be ADDED, got %s", changes["ONLY_REMOTE"])
	}
}

func TestMaskSecret(t *testing.T) {
	secret := "super-sensitive-pass"

	masked := MaskSecret(secret, false)
	if masked != "********" {
		t.Fatalf("expected masked to be ********, got: %s", masked)
	}

	revealed := MaskSecret(secret, true)
	if revealed != secret {
		t.Fatalf("expected revealed to match secret, got: %s", revealed)
	}
}
