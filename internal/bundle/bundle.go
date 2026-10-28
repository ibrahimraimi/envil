package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ibrahimraimi/envil/internal/errs"
)

const (
	CURRENT_SCHEMA_VERSION = 1
	MAX_BUNDLE_BYTES       = 1024 * 1024 // 1 MiB cap
)

var (
	ErrInvalidSchema          = errors.New("unsupported bundle schema version")
	ErrEmptyBundle            = errors.New("bundle contains no files")
	ErrBundleTooLarge   error = errs.New(errs.TooLarge, nil)
	ErrFileHashMismatch       = errors.New("file checksum does not match content")
)

type FileEntry struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type Bundle struct {
	Schema       int         `json:"schema"`
	Files        []FileEntry `json:"files"`
	CreatedBy    string      `json:"created_by"`
	HostnameHash string      `json:"hostname_hash"`
}

type ProjectMetadata struct {
	Name      string `json:"name"`
	GitRemote string `json:"git_remote,omitempty"`
}

func CreateBundle(entries []FileEntry, clientVersion string) (*Bundle, error) {
	if len(entries) == 0 {
		return nil, ErrEmptyBundle
	}

	validatedEntries := make([]FileEntry, len(entries))
	for index, entry := range entries {
		expectedHash := ComputeContentHash(entry.Content)
		if entry.SHA256 != "" && entry.SHA256 != expectedHash {
			return nil, fmt.Errorf("%w for %s", ErrFileHashMismatch, entry.Name)
		}
		validatedEntries[index] = FileEntry{
			Name:    entry.Name,
			Content: entry.Content,
			SHA256:  expectedHash,
		}
	}

	return &Bundle{
		Schema:       CURRENT_SCHEMA_VERSION,
		Files:        validatedEntries,
		CreatedBy:    clientVersion,
		HostnameHash: ComputeHostnameHash(),
	}, nil
}

func EncodeBundle(bundle *Bundle) ([]byte, error) {
	bundleBytes, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("marshal bundle: %w", err)
	}

	if len(bundleBytes) > MAX_BUNDLE_BYTES {
		return nil, ErrBundleTooLarge
	}

	return bundleBytes, nil
}

func DecodeBundle(rawBytes []byte) (*Bundle, error) {
	if len(rawBytes) > MAX_BUNDLE_BYTES {
		return nil, ErrBundleTooLarge
	}

	var parsedBundle Bundle
	if err := json.Unmarshal(rawBytes, &parsedBundle); err != nil {
		return nil, fmt.Errorf("unmarshal bundle: %w", err)
	}

	if parsedBundle.Schema != CURRENT_SCHEMA_VERSION {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrInvalidSchema, parsedBundle.Schema, CURRENT_SCHEMA_VERSION)
	}

	for _, entry := range parsedBundle.Files {
		computedHash := ComputeContentHash(entry.Content)
		if entry.SHA256 != computedHash {
			return nil, fmt.Errorf("%w for file %s", ErrFileHashMismatch, entry.Name)
		}
	}

	return &parsedBundle, nil
}

func EncodeProjectMetadata(meta *ProjectMetadata) ([]byte, error) {
	encodedBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal project metadata: %w", err)
	}
	return encodedBytes, nil
}

func DecodeProjectMetadata(rawBytes []byte) (*ProjectMetadata, error) {
	var meta ProjectMetadata
	if err := json.Unmarshal(rawBytes, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal project metadata: %w", err)
	}
	return &meta, nil
}

func ComputeContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func ComputeHostnameHash() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	sum := sha256.Sum256([]byte(hostname))
	return hex.EncodeToString(sum[:])
}
