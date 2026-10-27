package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

type mockAccountChecker struct {
	registeredAccounts map[[32]byte]bool
}

func (m *mockAccountChecker) HasAccount(ctx context.Context, accountID [32]byte) (bool, error) {
	return m.registeredAccounts[accountID], nil
}

func generateKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	return publicKey, privateKey
}

func TestSignAndVerifyValidRequest(t *testing.T) {
	_, privateKey := generateKeyPair(t)
	tracker := NewMemoryNonceTracker()

	request := httptest.NewRequest(http.MethodPost, "/v1/projects?env=prod", bytes.NewReader([]byte(`{"name":"test"}`)))
	if err := SignRequest(request, privateKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}

	identity, err := VerifyRequestHeaders(request, tracker, time.Now())
	if err != nil {
		t.Fatalf("verify request headers failed: %v", err)
	}

	expectedPub := privateKey.Public().(ed25519.PublicKey)
	if !bytes.Equal(identity.PublicKey, expectedPub) {
		t.Fatal("identity public key does not match signing key")
	}
}

func TestAlteredFieldsFailVerification(t *testing.T) {
	_, privateKey := generateKeyPair(t)

	testCases := []struct {
		name       string
		tamperFunc func(req *http.Request)
	}{
		{
			name: "altered method",
			tamperFunc: func(req *http.Request) {
				req.Method = http.MethodPut
			},
		},
		{
			name: "altered path",
			tamperFunc: func(req *http.Request) {
				req.URL.Path = "/v1/other"
			},
		},
		{
			name: "altered query",
			tamperFunc: func(req *http.Request) {
				req.URL.RawQuery = "tampered=true"
			},
		},
		{
			name: "altered body",
			tamperFunc: func(req *http.Request) {
				req.Body = ioNopBody([]byte(`{"name":"tampered"}`))
			},
		},
		{
			name: "altered nonce",
			tamperFunc: func(req *http.Request) {
				req.Header.Set(HEADER_NONCE, "differentnonce1234")
			},
		},
		{
			name: "altered timestamp header",
			tamperFunc: func(req *http.Request) {
				req.Header.Set(HEADER_TIMESTAMP, strconv.FormatInt(time.Now().Unix()+10, 10))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := NewMemoryNonceTracker()
			request := httptest.NewRequest(http.MethodPost, "/v1/projects", bytes.NewReader([]byte(`{"name":"test"}`)))
			if err := SignRequest(request, privateKey); err != nil {
				t.Fatalf("sign request: %v", err)
			}

			tc.tamperFunc(request)

			_, err := VerifyRequestHeaders(request, tracker, time.Now())
			if err == nil {
				t.Fatalf("verification unexpectedly succeeded for %s", tc.name)
			}
		})
	}
}

func TestTimestampDriftWindow(t *testing.T) {
	_, privateKey := generateKeyPair(t)
	tracker := NewMemoryNonceTracker()

	now := time.Now()

	// 6 minutes in the past
	pastRequest := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	if err := SignRequest(pastRequest, privateKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	pastRequest.Header.Set(HEADER_TIMESTAMP, strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10))

	if _, err := VerifyRequestHeaders(pastRequest, tracker, now); err != ErrTimestampSkew {
		t.Fatalf("expected ErrTimestampSkew, got: %v", err)
	}

	// 6 minutes in the future
	futureRequest := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	if err := SignRequest(futureRequest, privateKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	futureRequest.Header.Set(HEADER_TIMESTAMP, strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10))

	if _, err := VerifyRequestHeaders(futureRequest, tracker, now); err != ErrTimestampSkew {
		t.Fatalf("expected ErrTimestampSkew, got: %v", err)
	}
}

func TestReusedNonceFails(t *testing.T) {
	_, privateKey := generateKeyPair(t)
	tracker := NewMemoryNonceTracker()

	request := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	if err := SignRequest(request, privateKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}

	now := time.Now()
	if _, err := VerifyRequestHeaders(request, tracker, now); err != nil {
		t.Fatalf("first verification failed: %v", err)
	}

	// Second verification with identical nonce must fail
	if _, err := VerifyRequestHeaders(request, tracker, now); err != ErrReplayedNonce {
		t.Fatalf("expected ErrReplayedNonce, got: %v", err)
	}
}

func TestSignatureFromDifferentKeyFails(t *testing.T) {
	publicKeyA, _ := generateKeyPair(t)
	_, privateKeyB := generateKeyPair(t)
	tracker := NewMemoryNonceTracker()

	request := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	if err := SignRequest(request, privateKeyB); err != nil {
		t.Fatalf("sign request: %v", err)
	}

	// Replace public key header with Key A while signature is from Key B
	request.Header.Set(HEADER_KEY, base64.StdEncoding.EncodeToString(publicKeyA))

	if _, err := VerifyRequestHeaders(request, tracker, time.Now()); err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestMissingAndMalformedHeadersFail(t *testing.T) {
	tracker := NewMemoryNonceTracker()

	testCases := []struct {
		name         string
		setupHeaders func(req *http.Request)
	}{
		{
			name: "missing all headers",
			setupHeaders: func(req *http.Request) {},
		},
		{
			name: "missing key",
			setupHeaders: func(req *http.Request) {
				req.Header.Set(HEADER_TIMESTAMP, "123456789")
				req.Header.Set(HEADER_NONCE, "nonce")
				req.Header.Set(HEADER_SIGNATURE, "sig")
			},
		},
		{
			name: "malformed base64 key",
			setupHeaders: func(req *http.Request) {
				req.Header.Set(HEADER_KEY, "not-valid-base64!!!")
				req.Header.Set(HEADER_TIMESTAMP, "123456789")
				req.Header.Set(HEADER_NONCE, "nonce")
				req.Header.Set(HEADER_SIGNATURE, "sig")
			},
		},
		{
			name: "malformed timestamp",
			setupHeaders: func(req *http.Request) {
				req.Header.Set(HEADER_KEY, base64.StdEncoding.EncodeToString(make([]byte, 32)))
				req.Header.Set(HEADER_TIMESTAMP, "not-a-number")
				req.Header.Set(HEADER_NONCE, "nonce")
				req.Header.Set(HEADER_SIGNATURE, "sig")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
			tc.setupHeaders(req)
			_, err := VerifyRequestHeaders(req, tracker, time.Now())
			if err == nil {
				t.Fatal("expected header verification failure, got nil")
			}
		})
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	_, privateKey := generateKeyPair(t)
	tracker := NewMemoryNonceTracker()

	identity := Identity{
		PublicKey: privateKey.Public().(ed25519.PublicKey),
	}
	identity.AccountID = computeAccountID(identity.PublicKey)

	checker := &mockAccountChecker{
		registeredAccounts: map[[32]byte]bool{
			identity.AccountID: true,
		},
	}

	handlerWasCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqIdentity, ok := GetIdentity(r.Context())
		if !ok {
			t.Fatal("identity not found in context")
		}
		if !bytes.Equal(reqIdentity.PublicKey, identity.PublicKey) {
			t.Fatal("identity public key mismatch")
		}
		handlerWasCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireAuth(checker, tracker)
	wrappedHandler := middleware(testHandler)

	// Test 1: Registered account succeeds
	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	if err := SignRequest(req, privateKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", recorder.Code)
	}
	if !handlerWasCalled {
		t.Fatal("expected handler to be invoked")
	}

	// Test 2: Unregistered account returns 401
	_, unregisteredPrivateKey := generateKeyPair(t)
	unregisteredReq := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	if err := SignRequest(unregisteredReq, unregisteredPrivateKey); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	unregisteredRecorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(unregisteredRecorder, unregisteredReq)

	if unregisteredRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unregistered key, got: %d", unregisteredRecorder.Code)
	}
}

func computeAccountID(publicKey ed25519.PublicKey) [32]byte {
	return sha256.Sum256(publicKey)
}

func ioNopBody(bodyBytes []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(bodyBytes))
}
