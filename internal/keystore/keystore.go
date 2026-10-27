package keystore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
	"github.com/ibrahimraimi/envil/internal/crypto"
)

const (
	KEYRING_SERVICE = "envil"
	KEYRING_USER    = "keys"
	CONFIG_DIR_NAME = "envil"
	KEYS_FILE_NAME  = "keys.json"
)

var (
	ErrKeysNotFound = errors.New("no encryption keys found locally, run 'envil init' or 'envil recover'")
)

type Keystore interface {
	GetKeys() (*crypto.Keys, error)
	SaveKeys(keys *crypto.Keys) error
	DeleteKeys() error
}

type StoredPayload struct {
	SigningSeed    string `json:"signing_seed"`
	EncryptionRoot string `json:"encryption_root"`
}

type KeyringStore struct {
	service string
	user    string
}

func NewKeyringStore() *KeyringStore {
	return &KeyringStore{
		service: KEYRING_SERVICE,
		user:    KEYRING_USER,
	}
}

func (s *KeyringStore) GetKeys() (*crypto.Keys, error) {
	serializedPayload, err := keyring.Get(s.service, s.user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, ErrKeysNotFound
		}
		return nil, fmt.Errorf("read from keyring: %w", err)
	}

	return deserializeKeys(serializedPayload)
}

func (s *KeyringStore) SaveKeys(keys *crypto.Keys) error {
	serializedPayload, err := serializeKeys(keys)
	if err != nil {
		return err
	}

	if err := keyring.Set(s.service, s.user, serializedPayload); err != nil {
		return fmt.Errorf("write to keyring: %w", err)
	}
	return nil
}

func (s *KeyringStore) DeleteKeys() error {
	if err := keyring.Delete(s.service, s.user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete from keyring: %w", err)
	}
	return nil
}

type FileStore struct {
	filePath string
}

func NewFileStore(filePath string) *FileStore {
	return &FileStore{filePath: filePath}
}

func GetDefaultKeysFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	return filepath.Join(configDir, CONFIG_DIR_NAME, KEYS_FILE_NAME), nil
}

func (s *FileStore) GetKeys() (*crypto.Keys, error) {
	fileBytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrKeysNotFound
		}
		return nil, fmt.Errorf("read keys file: %w", err)
	}

	return deserializeKeys(string(fileBytes))
}

func (s *FileStore) SaveKeys(keys *crypto.Keys) error {
	serializedPayload, err := serializeKeys(keys)
	if err != nil {
		return err
	}

	parentDirectory := filepath.Dir(s.filePath)
	if err := os.MkdirAll(parentDirectory, 0700); err != nil {
		return fmt.Errorf("create keys directory: %w", err)
	}

	if err := os.WriteFile(s.filePath, []byte(serializedPayload), 0600); err != nil {
		return fmt.Errorf("write keys file: %w", err)
	}
	return nil
}

func (s *FileStore) DeleteKeys() error {
	if err := os.Remove(s.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete keys file: %w", err)
	}
	return nil
}

type AutoStore struct {
	primary  Keystore
	fallback Keystore
}

func NewDefaultKeystore() Keystore {
	keysPath, err := GetDefaultKeysFilePath()
	var fallback Keystore
	if err == nil {
		fallback = NewFileStore(keysPath)
	}

	return &AutoStore{
		primary:  NewKeyringStore(),
		fallback: fallback,
	}
}

func (s *AutoStore) GetKeys() (*crypto.Keys, error) {
	keys, err := s.primary.GetKeys()
	if err == nil {
		return keys, nil
	}

	if s.fallback != nil {
		fallbackKeys, fallbackErr := s.fallback.GetKeys()
		if fallbackErr == nil {
			return fallbackKeys, nil
		}
		if errors.Is(fallbackErr, ErrKeysNotFound) {
			return nil, ErrKeysNotFound
		}
	}

	return nil, err
}

func (s *AutoStore) SaveKeys(keys *crypto.Keys) error {
	err := s.primary.SaveKeys(keys)
	if err == nil {
		return nil
	}

	if s.fallback != nil {
		return s.fallback.SaveKeys(keys)
	}

	return err
}

func (s *AutoStore) DeleteKeys() error {
	primaryErr := s.primary.DeleteKeys()
	if s.fallback != nil {
		_ = s.fallback.DeleteKeys()
	}
	return primaryErr
}

func serializeKeys(keys *crypto.Keys) (string, error) {
	payload := StoredPayload{
		SigningSeed:    base64.StdEncoding.EncodeToString(keys.SigningSeed()),
		EncryptionRoot: base64.StdEncoding.EncodeToString(keys.EncryptionRoot()),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal keys: %w", err)
	}
	return string(payloadBytes), nil
}

func deserializeKeys(serializedPayload string) (*crypto.Keys, error) {
	var payload StoredPayload
	if err := json.Unmarshal([]byte(serializedPayload), &payload); err != nil {
		return nil, fmt.Errorf("unmarshal keys: %w", err)
	}

	seed, err := base64.StdEncoding.DecodeString(payload.SigningSeed)
	if err != nil {
		return nil, fmt.Errorf("decode signing seed: %w", err)
	}

	root, err := base64.StdEncoding.DecodeString(payload.EncryptionRoot)
	if err != nil {
		return nil, fmt.Errorf("decode encryption root: %w", err)
	}

	return crypto.KeysFromSeeds(seed, root)
}
