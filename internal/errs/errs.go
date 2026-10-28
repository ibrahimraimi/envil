package errs

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Code string

const (
	ClockSkew     Code = "E_CLOCK_SKEW"
	Offline       Code = "E_OFFLINE"
	Auth          Code = "E_AUTH"
	Conflict      Code = "E_CONFLICT"
	Quota         Code = "E_QUOTA"
	TooLarge      Code = "E_TOO_LARGE"
	Rollback      Code = "E_ROLLBACK"
	StateTampered Code = "E_STATE_TAMPERED"
	Keychain      Code = "E_KEYCHAIN"
	NotInit       Code = "E_NOT_INITIALIZED"
)

type Entry struct {
	Message string
	Hint    string
}

var catalog = map[Code]Entry{
	ClockSkew: {
		Message: "Your system clock differs from the server by more than 5 minutes.",
		Hint:    "Enable automatic time sync, then retry. Run `envil doctor`.",
	},
	Offline: {
		Message: "Could not reach the Envil server.",
		Hint:    "Check your connection and `ENVIL_SERVER` setting. Your local files were not changed.",
	},
	Auth: {
		Message: "The server did not accept your credentials.",
		Hint:    "Run `envil doctor`. If you are on a new machine, run `envil recover`.",
	},
	Conflict: {
		Message: "A newer version exists than the one you started from.",
		Hint:    "Run `envil diff`, then restore or merge, then back up again.",
	},
	Quota: {
		Message: "Your storage limit was reached.",
		Hint:    "Delete old projects or versions, then retry.",
	},
	TooLarge: {
		Message: "The environment bundle is larger than the 1 MiB limit.",
		Hint:    "Remove large values or split projects.",
	},
	Rollback: {
		Message: "The server reported an older or divergent backup version for this project.",
		Hint:    "Nothing was changed. If you know this is expected, run again with --accept-server-state.",
	},
	StateTampered: {
		Message: "The local state file could not be verified.",
		Hint:    "Run `envil state reset` to rebuild it.",
	},
	Keychain: {
		Message: "Could not access the system keychain.",
		Hint:    "See `envil doctor` for fallback options.",
	},
	NotInit: {
		Message: "This directory is not set up.",
		Hint:    "Run `envil init`.",
	},
}

func AllCodes() []Code {
	return []Code{
		ClockSkew,
		Offline,
		Auth,
		Conflict,
		Quota,
		TooLarge,
		Rollback,
		StateTampered,
		Keychain,
		NotInit,
	}
}

func Lookup(code Code) (Entry, bool) {
	entry, ok := catalog[code]
	return entry, ok
}

type Error struct {
	Code    Code
	Message string
	Hint    string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func (e *Error) Is(target error) bool {
	var targetErr *Error
	if errors.As(target, &targetErr) {
		return e.Code == targetErr.Code
	}
	return false
}

func New(code Code, cause error) *Error {
	entry := catalog[code]
	return &Error{
		Code:    code,
		Message: entry.Message,
		Hint:    entry.Hint,
		Cause:   cause,
	}
}

func WithMessage(code Code, message string, cause error) *Error {
	entry := catalog[code]
	return &Error{
		Code:    code,
		Message: message,
		Hint:    entry.Hint,
		Cause:   cause,
	}
}

func NewRollback(serverVersion, localVersion int) *Error {
	msg := fmt.Sprintf(
		"Warning: the server reports version %d for this project, but this machine has already seen version %d. The server may be serving older data.",
		serverVersion,
		localVersion,
	)
	return WithMessage(Rollback, msg, nil)
}

func NewFork(version int) *Error {
	msg := fmt.Sprintf(
		"Warning: the server returned a different encrypted blob for version %d than this machine previously recorded. The backup may have been replaced.",
		version,
	)
	return WithMessage(Rollback, msg, nil)
}

func IsCode(err error, code Code) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code == code
}

func Format(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		if e.Hint != "" {
			return fmt.Sprintf("%s\n\n%s", e.Message, e.Hint)
		}
		return e.Message
	}
	return err.Error()
}

type UsageError struct {
	Err error
}

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var u *UsageError
	if errors.As(err, &u) {
		return 2
	}
	var e *Error
	if !errors.As(err, &e) {
		return 1
	}
	switch e.Code {
	case Rollback, StateTampered:
		return 3
	case Offline, ClockSkew, Auth, Quota:
		return 4
	default:
		return 1
	}
}

func FromHTTP(statusCode int, bodyCode, bodyMessage string, cause error) error {
	lowerCode := strings.ToLower(bodyCode)
	lowerMsg := strings.ToLower(bodyMessage)

	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		if lowerCode == "clock_skew" || strings.Contains(lowerMsg, "timestamp") || strings.Contains(lowerMsg, "clock") {
			return New(ClockSkew, cause)
		}
		return New(Auth, cause)
	case http.StatusConflict:
		return New(Conflict, cause)
	case http.StatusRequestEntityTooLarge:
		if lowerCode == "blob_too_large" || lowerCode == "too_large" || strings.Contains(lowerMsg, "1 mib") || strings.Contains(lowerMsg, "blob") {
			return New(TooLarge, cause)
		}
		return New(Quota, cause)
	default:
		if cause != nil {
			return cause
		}
		if bodyMessage != "" {
			return fmt.Errorf("server error (%d): %s", statusCode, bodyMessage)
		}
		return fmt.Errorf("server error (%d)", statusCode)
	}
}
