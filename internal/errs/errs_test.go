package errs

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogCompleteness(t *testing.T) {
	codes := AllCodes()
	if len(codes) == 0 {
		t.Fatal("expected non-empty error code catalog")
	}

	for _, code := range codes {
		entry, ok := Lookup(code)
		if !ok {
			t.Fatalf("code %q missing from catalog", code)
		}
		if strings.TrimSpace(entry.Message) == "" {
			t.Errorf("code %q has empty Message", code)
		}
		if strings.TrimSpace(entry.Hint) == "" {
			t.Errorf("code %q has empty Hint", code)
		}
	}
}

func TestExitCodes(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("expected exit code 0 for nil error, got %d", got)
	}

	if got := ExitCode(errors.New("generic failure")); got != 1 {
		t.Fatalf("expected exit code 1 for non-catalog error, got %d", got)
	}

	cases := []struct {
		code     Code
		expected int
	}{
		{Rollback, 3},
		{StateTampered, 3},
		{Offline, 4},
		{ClockSkew, 4},
		{Auth, 4},
		{Quota, 4},
		{Conflict, 1},
		{TooLarge, 1},
		{Keychain, 1},
		{NotInit, 1},
	}

	for _, tc := range cases {
		err := New(tc.code, nil)
		if got := ExitCode(err); got != tc.expected {
			t.Errorf("ExitCode(%s) = %d, want %d", tc.code, got, tc.expected)
		}
	}
}

func TestErrorsIsAndAsThroughWrappedCauses(t *testing.T) {
	rootCause := errors.New("dial tcp: connection refused")
	catalogErr := New(Offline, rootCause)
	wrapped := errors.Join(errors.New("context"), catalogErr)

	if !errors.Is(wrapped, rootCause) {
		t.Fatal("expected errors.Is to match underlying rootCause")
	}

	if !errors.Is(wrapped, New(Offline, nil)) {
		t.Fatal("expected errors.Is to match catalog error with same Code")
	}

	if errors.Is(wrapped, New(Auth, nil)) {
		t.Fatal("did not expect errors.Is to match different Code")
	}

	var extracted *Error
	if !errors.As(wrapped, &extracted) {
		t.Fatal("expected errors.As to extract *errs.Error")
	}
	if extracted.Code != Offline {
		t.Fatalf("expected extracted code %s, got %s", Offline, extracted.Code)
	}
	if extracted.Unwrap() != rootCause {
		t.Fatalf("expected Unwrap() to return rootCause")
	}
}

func TestFromHTTPMapping(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		bodyCode    string
		bodyMessage string
		wantCode    Code
		wantGeneral bool
	}{
		{
			name:        "401 unauthorized maps to Auth",
			status:      http.StatusUnauthorized,
			bodyCode:    "unauthorized",
			bodyMessage: "signature verification failed",
			wantCode:    Auth,
		},
		{
			name:        "401 timestamp drift maps to ClockSkew",
			status:      http.StatusUnauthorized,
			bodyCode:    "unauthorized",
			bodyMessage: "request timestamp outside acceptable window",
			wantCode:    ClockSkew,
		},
		{
			name:        "403 forbidden maps to Auth",
			status:      http.StatusForbidden,
			bodyCode:    "forbidden",
			bodyMessage: "access to project is forbidden",
			wantCode:    Auth,
		},
		{
			name:        "409 conflict maps to Conflict",
			status:      http.StatusConflict,
			bodyCode:    "version_conflict",
			bodyMessage: "remote version has changed",
			wantCode:    Conflict,
		},
		{
			name:        "413 quota maps to Quota",
			status:      http.StatusRequestEntityTooLarge,
			bodyCode:    "quota_exceeded",
			bodyMessage: "account storage quota exceeded",
			wantCode:    Quota,
		},
		{
			name:        "413 blob too large maps to TooLarge",
			status:      http.StatusRequestEntityTooLarge,
			bodyCode:    "blob_too_large",
			bodyMessage: "blob exceeds 1 MiB limit",
			wantCode:    TooLarge,
		},
		{
			name:        "500 internal server error maps to general error",
			status:      http.StatusInternalServerError,
			bodyCode:    "internal_error",
			bodyMessage: "database unavailable",
			wantGeneral: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New(tc.bodyMessage)
			err := FromHTTP(tc.status, tc.bodyCode, tc.bodyMessage, cause)
			if tc.wantGeneral {
				var catErr *Error
				if errors.As(err, &catErr) {
					t.Fatalf("expected non-catalog error for %d, got %v", tc.status, catErr.Code)
				}
				if got := ExitCode(err); got != 1 {
					t.Fatalf("expected exit code 1 for general error, got %d", got)
				}
				return
			}

			if !IsCode(err, tc.wantCode) {
				t.Fatalf("expected code %s, got err=%v", tc.wantCode, err)
			}
		})
	}
}

func TestAllCatalogCodesDocumentedInREADME(t *testing.T) {
	readmePath := filepath.Join("..", "..", "README.md")
	contentBytes, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readme := string(contentBytes)

	for _, code := range AllCodes() {
		if !strings.Contains(readme, string(code)) {
			t.Errorf("error code %s is not documented in README.md", code)
		}
	}
}
