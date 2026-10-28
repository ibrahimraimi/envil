package service

import (
	"context"
	"time"
)

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

type DoctorCheck struct {
	Name    string
	Status  string // "ok", "warning", "error"
	Message string
}

type DoctorReport struct {
	Checks []DoctorCheck
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
	DeleteProject(ctx context.Context, projectID string) error
	DeleteAccount(ctx context.Context) error
	Doctor(ctx context.Context) (DoctorReport, error)
	LinkCurrentDirectory(ctx context.Context, dir, name string) (string, error)
	LoadProjectConfig(dir string) (string, error)
}
