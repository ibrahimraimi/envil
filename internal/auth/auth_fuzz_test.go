package auth

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"
)

func FuzzVerifyHeadersGarbage(f *testing.F) {
	f.Add("invalid-key", "12345678", "nonce", "sig", []byte("body"))
	f.Fuzz(func(t *testing.T, key, timestamp, nonce, signature string, bodyBytes []byte) {
		tracker := NewMemoryNonceTracker()
		request := httptest.NewRequest("POST", "/v1/test", bytes.NewReader(bodyBytes))
		request.Header.Set(HEADER_KEY, key)
		request.Header.Set(HEADER_TIMESTAMP, timestamp)
		request.Header.Set(HEADER_NONCE, nonce)
		request.Header.Set(HEADER_SIGNATURE, signature)

		// Must never panic on arbitrary input
		_, _ = VerifyRequestHeaders(request, tracker, time.Now())
	})
}
