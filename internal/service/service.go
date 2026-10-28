package service

import (
	"context"
	"time"
)

type acceptServerStateKey struct{}

func WithAcceptServerState(ctx context.Context, accept bool) context.Context {
	return context.WithValue(ctx, acceptServerStateKey{}, accept)
}

func ShouldAcceptServerState(ctx context.Context) bool {
	val, ok := ctx.Value(acceptServerStateKey{}).(bool)
	return ok && val
}

type ProjectSummary struct {
	ID            string
	Name          string
	GitRemote     string
	LatestVersion int
	UpdatedAt     time.Time
	CreatedAt     time.Time
}

type VersionInfo struct {
	Number    int
	SizeBytes int
	CreatedAt time.Time
}

type DiffEntry struct {
	Filename string
	Key      string
	Status   string // "added", "removed", "changed", "same"
}

type CheckStatus int

const (
	StatusOK CheckStatus = iota
	StatusWarn
	StatusFail
	StatusSkipped
)

func (s CheckStatus) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	case StatusSkipped:
		return "SKIP"
	default:
		return "UNKNOWN"
	}
}

type Check struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"-"`
	Label   string      `json:"status"`
	Detail  string      `json:"detail"`
	Hint    string      `json:"hint,omitempty"`
	Message string      `json:"-"`
}

func NewCheck(name string, status CheckStatus, detail, hint string) Check {
	return Check{
		Name:    name,
		Status:  status,
		Label:   status.String(),
		Detail:  detail,
		Hint:    hint,
		Message: detail,
	}
}

type DoctorCheck = Check

type DoctorReport struct {
	Checks []Check `json:"checks"`
}

func (r DoctorReport) HasFailures() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return true
		}
	}
	return false
}

type Service interface {
	HasKeys(ctx context.Context) (bool, error)
	AccountFingerprint(ctx context.Context) (string, error)
	NewPhrase(ctx context.Context) (string, error)
	CompleteSetup(ctx context.Context, phrase string) error
	Recover(ctx context.Context, phrase string) error
	ListProjects(ctx context.Context) ([]ProjectSummary, error)
	History(ctx context.Context, projectID string) ([]VersionInfo, error)
	Backup(ctx context.Context, projectID string, files []string) (int, error)
	Diff(ctx context.Context, projectID string, version int) ([]DiffEntry, error)
	RevealDiffValue(ctx context.Context, projectID string, version int, filename, key string) (string, string, error)
	Restore(ctx context.Context, projectID string, version int, force bool) error
	ResetState(ctx context.Context, projectID string) error
	DeleteProject(ctx context.Context, projectID string) error
	DeleteAccount(ctx context.Context) error
	Doctor(ctx context.Context) (DoctorReport, error)
	LinkCurrentDirectory(ctx context.Context, dir, name string) (string, error)
	LoadProjectConfig(dir string) (string, error)
}
