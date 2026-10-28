package state

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ibrahimraimi/envil/internal/errs"
)

const (
	CurrentSchema = 1
	ConfigDirName = "envil"
	StateFileName = "state.json"
)

var (
	ErrUnknownSchema = errors.New("unsupported state schema version")
)

type Entry struct {
	LatestVersion int       `json:"latest_version"`
	BlobSHA256    string    `json:"blob_sha256,omitempty"`
	SeenAt        time.Time `json:"seen_at"`
}

type Verdict int

const (
	OK Verdict = iota
	FirstSeen
	Advanced
	Rollback
	Fork
)

func (v Verdict) String() string {
	switch v {
	case OK:
		return "OK"
	case FirstSeen:
		return "FirstSeen"
	case Advanced:
		return "Advanced"
	case Rollback:
		return "Rollback"
	case Fork:
		return "Fork"
	default:
		return "Unknown"
	}
}

func Check(prev *Entry, serverLatest int, serverHash string) Verdict {
	switch {
	case prev == nil:
		return FirstSeen
	case serverLatest < prev.LatestVersion:
		return Rollback
	case serverLatest > prev.LatestVersion:
		return Advanced
	case serverHash != "" && prev.BlobSHA256 != "" && serverHash != prev.BlobSHA256:
		return Fork
	default:
		return OK
	}
}

func ComputeBlobHash(blob []byte) string {
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}

func GetDefaultStateFilePath() (string, error) {
	if customDir := os.Getenv("ENVIL_CONFIG_DIR"); customDir != "" {
		return filepath.Join(customDir, StateFileName), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configDir, ConfigDirName, StateFileName), nil
}

type canonicalPayload struct {
	Schema   int              `json:"schema"`
	Projects map[string]Entry `json:"projects"`
}

type stateFile struct {
	Schema   int              `json:"schema"`
	Projects map[string]Entry `json:"projects"`
	MAC      string           `json:"mac"`
}

type Store struct {
	mu           sync.Mutex
	filePath     string
	stateKey     []byte
	beforeRename func(tmpPath string) error
}

func NewStore(filePath string, stateKey []byte) *Store {
	copiedKey := make([]byte, len(stateKey))
	copy(copiedKey, stateKey)
	return &Store{
		filePath: filePath,
		stateKey: copiedKey,
	}
}

func (s *Store) FilePath() string {
	return s.filePath
}

func (s *Store) Exists() bool {
	_, err := os.Stat(s.filePath)
	return err == nil
}

func (s *Store) Load() (map[string]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadUnlocked()
}

func (s *Store) Save(projects map[string]Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(func() error {
		return s.saveUnlocked(projects)
	})
}

func (s *Store) Get(projectID string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	projects, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	entry, ok := projects[projectID]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

func (s *Store) Put(projectID string, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(func() error {
		projects, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		projects[projectID] = entry
		return s.saveUnlocked(projects)
	})
}

func (s *Store) Reset(projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(func() error {
		if projectID == "" {
			return s.saveUnlocked(make(map[string]Entry))
		}
		projects, err := s.loadUnlocked()
		if err != nil {
			if errs.IsCode(err, errs.StateTampered) {
				return s.saveUnlocked(make(map[string]Entry))
			}
			return err
		}
		delete(projects, projectID)
		return s.saveUnlocked(projects)
	})
}

func (s *Store) loadUnlocked() (map[string]Entry, error) {
	rawBytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]Entry), nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	return ParseStateBytes(rawBytes, s.stateKey)
}

func ParseStateBytes(rawBytes []byte, stateKey []byte) (map[string]Entry, error) {
	if len(rawBytes) == 0 {
		return nil, errs.New(errs.StateTampered, errors.New("empty state file"))
	}

	var parsed stateFile
	decoder := json.NewDecoder(bytes.NewReader(rawBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return nil, errs.New(errs.StateTampered, fmt.Errorf("parse state json: %w", err))
	}
	if decoder.More() {
		return nil, errs.New(errs.StateTampered, errors.New("trailing data in state file"))
	}

	if parsed.Projects == nil {
		return nil, errs.New(errs.StateTampered, errors.New("missing projects map"))
	}

	providedMAC, err := base64.StdEncoding.DecodeString(parsed.MAC)
	if err != nil || len(providedMAC) != sha256.Size {
		if parsed.Schema != CurrentSchema && parsed.Schema != 0 {
			return nil, fmt.Errorf("%w: got %d, expected %d", ErrUnknownSchema, parsed.Schema, CurrentSchema)
		}
		return nil, errs.New(errs.StateTampered, errors.New("invalid state mac encoding"))
	}

	if parsed.Schema != CurrentSchema {
		expectedV1MAC, err := computeMAC(stateKey, CurrentSchema, parsed.Projects)
		if err == nil && hmac.Equal(providedMAC, expectedV1MAC) {
			return nil, errs.New(errs.StateTampered, errors.New("state schema field tampered"))
		}
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrUnknownSchema, parsed.Schema, CurrentSchema)
	}

	expectedMAC, err := computeMAC(stateKey, parsed.Schema, parsed.Projects)
	if err != nil {
		return nil, fmt.Errorf("compute state mac: %w", err)
	}

	if !hmac.Equal(providedMAC, expectedMAC) {
		return nil, errs.New(errs.StateTampered, errors.New("state mac verification failed"))
	}

	// Ensure canonical roundtrip matches rawBytes to detect whitespace/duplicate-key tampering
	expectedRaw, err := marshalStateFile(parsed.Schema, parsed.Projects, parsed.MAC)
	if err != nil || string(expectedRaw) != string(rawBytes) {
		return nil, errs.New(errs.StateTampered, errors.New("state file non-canonical content tampered"))
	}

	return parsed.Projects, nil
}

func (s *Store) saveUnlocked(projects map[string]Entry) error {
	if projects == nil {
		projects = make(map[string]Entry)
	}

	parentDir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(parentDir, 0700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	_ = os.Chmod(parentDir, 0700)

	macBytes, err := computeMAC(s.stateKey, CurrentSchema, projects)
	if err != nil {
		return fmt.Errorf("compute state mac: %w", err)
	}
	macString := base64.StdEncoding.EncodeToString(macBytes)

	contentBytes, err := marshalStateFile(CurrentSchema, projects, macString)
	if err != nil {
		return fmt.Errorf("marshal state file: %w", err)
	}

	tmpFile, err := os.CreateTemp(parentDir, StateFileName+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temporary state file: %w", err)
	}

	if _, err := tmpFile.Write(contentBytes); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temporary state file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("fsync temporary state file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary state file: %w", err)
	}

	if s.beforeRename != nil {
		if err := s.beforeRename(tmpPath); err != nil {
			return err
		}
	}

	if err := os.Rename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("rename state file: %w", err)
	}
	_ = os.Chmod(s.filePath, 0600)
	cleanupTmp = false
	return nil
}

func (s *Store) withFileLock(fn func() error) error {
	parentDir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(parentDir, 0700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	_ = os.Chmod(parentDir, 0700)

	lockPath := s.filePath + ".lock"
	lockHandle, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open state lock file: %w", err)
	}
	defer lockHandle.Close()

	if err := lockFile(lockHandle); err != nil {
		return fmt.Errorf("lock state file: %w", err)
	}
	defer func() {
		_ = unlockFile(lockHandle)
	}()

	return fn()
}

func computeMAC(stateKey []byte, schema int, projects map[string]Entry) ([]byte, error) {
	canonicalBytes, err := json.Marshal(canonicalPayload{
		Schema:   schema,
		Projects: projects,
	})
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, stateKey)
	_, _ = mac.Write(canonicalBytes)
	return mac.Sum(nil), nil
}

func marshalStateFile(schema int, projects map[string]Entry, mac string) ([]byte, error) {
	fileData := stateFile{
		Schema:   schema,
		Projects: projects,
		MAC:      mac,
	}
	encoded, err := json.MarshalIndent(fileData, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
