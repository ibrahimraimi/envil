package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	FORMAT_VERSION byte   = 1
	HKDF_SALT             = "envil-v1"
	KEY_SIZE       int    = 32
	ENTROPY_BITS   int    = 256
	NONCE_SIZE     int    = 24
)

var (
	ErrInvalidPhrase  = errors.New("invalid recovery phrase")
	ErrDecrypt        = errors.New("decryption failed")
	ErrInvalidKeySize = errors.New("key must be exactly 32 bytes")
)

type Keys struct {
	Signing ed25519.PrivateKey
	encRoot [32]byte
}

func NewPhrase() (string, error) {
	entropy, err := bip39.NewEntropy(ENTROPY_BITS)
	if err != nil {
		return "", fmt.Errorf("generate entropy: %w", err)
	}
	defer clear(entropy)

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("create mnemonic: %w", err)
	}
	return mnemonic, nil
}

func ValidatePhrase(phrase string) error {
	if !bip39.IsMnemonicValid(phrase) {
		return ErrInvalidPhrase
	}
	return nil
}

func KeysFromPhrase(phrase string) (*Keys, error) {
	if err := ValidatePhrase(phrase); err != nil {
		return nil, err
	}

	entropy, err := bip39.EntropyFromMnemonic(phrase)
	if err != nil {
		return nil, ErrInvalidPhrase
	}
	defer clear(entropy)

	signingSeed, err := deriveKey(entropy, "envil/v1/signing")
	if err != nil {
		return nil, fmt.Errorf("derive signing seed: %w", err)
	}
	defer clear(signingSeed)

	encryptionRoot, err := deriveKey(entropy, "envil/v1/encryption")
	if err != nil {
		return nil, fmt.Errorf("derive encryption root: %w", err)
	}
	defer clear(encryptionRoot)

	keys := &Keys{
		Signing: ed25519.NewKeyFromSeed(signingSeed),
	}
	copy(keys.encRoot[:], encryptionRoot)

	return keys, nil
}

func KeysFromSeeds(signingSeed, encryptionRoot []byte) (*Keys, error) {
	if len(signingSeed) != ed25519.SeedSize {
		return nil, errors.New("signing seed must be 32 bytes")
	}
	if len(encryptionRoot) != KEY_SIZE {
		return nil, errors.New("encryption root must be 32 bytes")
	}

	keys := &Keys{
		Signing: ed25519.NewKeyFromSeed(signingSeed),
	}
	copy(keys.encRoot[:], encryptionRoot)
	return keys, nil
}

func (k *Keys) SigningSeed() []byte {
	return k.Signing.Seed()
}

func (k *Keys) EncryptionRoot() []byte {
	copied := make([]byte, KEY_SIZE)
	copy(copied, k.encRoot[:])
	return copied
}

func (k *Keys) AccountID() [32]byte {
	publicKey := k.Signing.Public().(ed25519.PublicKey)
	return sha256.Sum256(publicKey)
}

func (k *Keys) PublicKey() ed25519.PublicKey {
	return k.Signing.Public().(ed25519.PublicKey)
}

func (k *Keys) ProjectKey(projectID string) ([]byte, error) {
	return deriveKey(k.encRoot[:], "envil/v1/project/"+projectID)
}

func (k *Keys) Destroy() {
	clear(k.Signing)
	clear(k.encRoot[:])
}

func deriveKey(secret []byte, info string) ([]byte, error) {
	reader := hkdf.New(sha256.New, secret, []byte(HKDF_SALT), []byte(info))
	derivedKey := make([]byte, KEY_SIZE)
	if _, err := io.ReadFull(reader, derivedKey); err != nil {
		return nil, fmt.Errorf("read hkdf output: %w", err)
	}
	return derivedKey, nil
}

func BuildAAD(accountID [32]byte, projectID string, version uint32) []byte {
	aad := make([]byte, 0, len(accountID)+len(projectID)+4)
	aad = append(aad, accountID[:]...)
	aad = append(aad, []byte(projectID)...)
	return binary.BigEndian.AppendUint32(aad, version)
}

func Seal(key, plaintext, aad []byte) ([]byte, error) {
	if len(key) != KEY_SIZE {
		return nil, ErrInvalidKeySize
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}

	headerLength := 1 + aead.NonceSize()
	ciphertextLength := len(plaintext) + aead.Overhead()
	sealedBlob := make([]byte, headerLength, headerLength+ciphertextLength)
	sealedBlob[0] = FORMAT_VERSION

	nonceSlice := sealedBlob[1:headerLength]
	if _, err := rand.Read(nonceSlice); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return aead.Seal(sealedBlob, nonceSlice, plaintext, aad), nil
}

func SealWithNonce(key, nonce, plaintext, aad []byte) ([]byte, error) {
	if len(key) != KEY_SIZE {
		return nil, ErrInvalidKeySize
	}
	if len(nonce) != NONCE_SIZE {
		return nil, errors.New("nonce must be exactly 24 bytes")
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}

	headerLength := 1 + len(nonce)
	ciphertextLength := len(plaintext) + aead.Overhead()
	sealedBlob := make([]byte, headerLength, headerLength+ciphertextLength)
	sealedBlob[0] = FORMAT_VERSION
	copy(sealedBlob[1:headerLength], nonce)

	return aead.Seal(sealedBlob, nonce, plaintext, aad), nil
}

func Open(key, blob, aad []byte) ([]byte, error) {
	if len(key) != KEY_SIZE {
		return nil, ErrInvalidKeySize
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}

	headerLength := 1 + aead.NonceSize()
	if len(blob) < headerLength+aead.Overhead() {
		return nil, ErrDecrypt
	}

	if blob[0] != FORMAT_VERSION {
		return nil, fmt.Errorf("unsupported blob version: %d", blob[0])
	}

	nonce := blob[1:headerLength]
	ciphertextWithTag := blob[headerLength:]

	plaintext, err := aead.Open(nil, nonce, ciphertextWithTag, aad)
	if err != nil {
		return nil, ErrDecrypt
	}

	return plaintext, nil
}
