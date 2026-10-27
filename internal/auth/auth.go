package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	HEADER_KEY          = "X-Envil-Key"
	HEADER_TIMESTAMP    = "X-Envil-Timestamp"
	HEADER_NONCE        = "X-Envil-Nonce"
	HEADER_SIGNATURE    = "X-Envil-Signature"
	MAX_TIMESTAMP_DRIFT = 5 * time.Minute
	NONCE_CACHE_TTL     = 10 * time.Minute
	MAX_BODY_BYTES      = 2 * 1024 * 1024
	NONCE_BYTE_LENGTH   = 16
)

var (
	ErrMissingHeader     = errors.New("missing authentication headers")
	ErrInvalidHeader     = errors.New("invalid authentication header format")
	ErrTimestampSkew     = errors.New("request timestamp outside acceptable window")
	ErrReplayedNonce     = errors.New("nonce has already been used")
	ErrInvalidSignature  = errors.New("signature verification failed")
	ErrUnregisteredKey   = errors.New("public key is not registered")
	ErrRequestBodyTooBig = errors.New("request body exceeds size limit")
)

type Identity struct {
	PublicKey ed25519.PublicKey
	AccountID [32]byte
}

type contextKey struct{}

var identityContextKey = contextKey{}

func SetIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, identity)
}

func GetIdentity(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey).(Identity)
	return identity, ok
}

type AccountChecker interface {
	HasAccount(ctx context.Context, accountID [32]byte) (bool, error)
}

type NonceTracker interface {
	CheckAndStoreNonce(accountID [32]byte, nonce string, now time.Time) bool
}

type MemoryNonceTracker struct {
	lock    sync.Mutex
	entries map[string]time.Time
}

func NewMemoryNonceTracker() *MemoryNonceTracker {
	return &MemoryNonceTracker{
		entries: make(map[string]time.Time),
	}
}

func (tracker *MemoryNonceTracker) CheckAndStoreNonce(accountID [32]byte, nonce string, now time.Time) bool {
	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	tracker.purgeExpiredEntries(now)

	key := hex.EncodeToString(accountID[:]) + ":" + nonce
	if _, exists := tracker.entries[key]; exists {
		return false
	}

	tracker.entries[key] = now.Add(NONCE_CACHE_TTL)
	return true
}

func (tracker *MemoryNonceTracker) purgeExpiredEntries(now time.Time) {
	for cacheKey, expiration := range tracker.entries {
		if now.After(expiration) {
			delete(tracker.entries, cacheKey)
		}
	}
}

func GenerateNonce() (string, error) {
	nonceBytes := make([]byte, NONCE_BYTE_LENGTH)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.StdEncoding.EncodeToString(nonceBytes), nil
}

func ComputeBodyHash(bodyBytes []byte) string {
	hashBytes := sha256.Sum256(bodyBytes)
	return hex.EncodeToString(hashBytes[:])
}

func BuildCanonicalString(method, target string, timestamp int64, nonce, bodyHashHex string) string {
	return fmt.Sprintf("%s\n%s\n%d\n%s\n%s", method, target, timestamp, nonce, bodyHashHex)
}

func GetRequestTarget(req *http.Request) string {
	target := req.URL.RequestURI()
	if target == "" {
		return req.URL.Path
	}
	return target
}

func SignRequest(req *http.Request, privateKey ed25519.PrivateKey) error {
	bodyBytes, err := readAndRestoreBody(req)
	if err != nil {
		return fmt.Errorf("read body for signing: %w", err)
	}

	nonce, err := GenerateNonce()
	if err != nil {
		return fmt.Errorf("create nonce: %w", err)
	}

	nowSeconds := time.Now().Unix()
	bodyHashHex := ComputeBodyHash(bodyBytes)
	target := GetRequestTarget(req)
	canonicalString := BuildCanonicalString(req.Method, target, nowSeconds, nonce, bodyHashHex)

	signature := ed25519.Sign(privateKey, []byte(canonicalString))
	publicKey := privateKey.Public().(ed25519.PublicKey)

	req.Header.Set(HEADER_KEY, base64.StdEncoding.EncodeToString(publicKey))
	req.Header.Set(HEADER_TIMESTAMP, strconv.FormatInt(nowSeconds, 10))
	req.Header.Set(HEADER_NONCE, nonce)
	req.Header.Set(HEADER_SIGNATURE, base64.StdEncoding.EncodeToString(signature))

	return nil
}

func VerifyRequestHeaders(req *http.Request, nonces NonceTracker, now time.Time) (*Identity, error) {
	rawKey := req.Header.Get(HEADER_KEY)
	rawTimestamp := req.Header.Get(HEADER_TIMESTAMP)
	nonce := req.Header.Get(HEADER_NONCE)
	rawSignature := req.Header.Get(HEADER_SIGNATURE)

	if rawKey == "" || rawTimestamp == "" || nonce == "" || rawSignature == "" {
		return nil, ErrMissingHeader
	}

	publicKey, err := parsePublicKey(rawKey)
	if err != nil {
		return nil, err
	}

	timestampSeconds, err := strconv.ParseInt(rawTimestamp, 10, 64)
	if err != nil {
		return nil, ErrInvalidHeader
	}

	if err := checkTimestampDrift(timestampSeconds, now); err != nil {
		return nil, err
	}

	signatureBytes, err := base64.StdEncoding.DecodeString(rawSignature)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return nil, ErrInvalidHeader
	}

	bodyBytes, err := readAndRestoreBody(req)
	if err != nil {
		return nil, err
	}

	bodyHashHex := ComputeBodyHash(bodyBytes)
	target := GetRequestTarget(req)
	canonicalString := BuildCanonicalString(req.Method, target, timestampSeconds, nonce, bodyHashHex)

	if !ed25519.Verify(publicKey, []byte(canonicalString), signatureBytes) {
		return nil, ErrInvalidSignature
	}

	identity := &Identity{
		PublicKey: publicKey,
		AccountID: sha256.Sum256(publicKey),
	}

	if !nonces.CheckAndStoreNonce(identity.AccountID, nonce, now) {
		return nil, ErrReplayedNonce
	}

	return identity, nil
}

func parsePublicKey(encodedKey string) (ed25519.PublicKey, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(keyBytes) != ed25519.PublicKeySize {
		return nil, ErrInvalidHeader
	}
	return ed25519.PublicKey(keyBytes), nil
}

func checkTimestampDrift(timestampSeconds int64, now time.Time) error {
	requestTime := time.Unix(timestampSeconds, 0)
	difference := now.Sub(requestTime)
	if difference < -MAX_TIMESTAMP_DRIFT || difference > MAX_TIMESTAMP_DRIFT {
		return ErrTimestampSkew
	}
	return nil
}

func readAndRestoreBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}

	limitedReader := io.LimitReader(req.Body, MAX_BODY_BYTES+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if len(bodyBytes) > MAX_BODY_BYTES {
		return nil, ErrRequestBodyTooBig
	}

	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	return bodyBytes, nil
}

func RequireAuth(checker AccountChecker, nonces NonceTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, err := VerifyRequestHeaders(r, nonces, time.Now())
			if err != nil {
				respondUnauthorized(w, err.Error())
				return
			}

			isRegistered, err := checker.HasAccount(r.Context(), identity.AccountID)
			if err != nil {
				respondInternalError(w, "account verification failed")
				return
			}
			if !isRegistered {
				respondUnauthorized(w, ErrUnregisteredKey.Error())
				return
			}

			next.ServeHTTP(w, r.WithContext(SetIdentity(r.Context(), *identity)))
		})
	}
}

func RequireSignatureOnly(nonces NonceTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, err := VerifyRequestHeaders(r, nonces, time.Now())
			if err != nil {
				respondUnauthorized(w, err.Error())
				return
			}

			next.ServeHTTP(w, r.WithContext(SetIdentity(r.Context(), *identity)))
		})
	}
}

func respondUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"error":{"code":"unauthorized","message":%q}}`+"\n", message)
}

func respondInternalError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = fmt.Fprintf(w, `{"error":{"code":"internal_error","message":%q}}`+"\n", message)
}
