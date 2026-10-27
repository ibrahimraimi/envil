package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DEFAULT_QUOTA_BYTES = 5 * 1024 * 1024 // 5 MiB
	DEFAULT_RETENTION   = 20              // Keep last 20 versions
)

var (
	ErrAccountNotFound  = errors.New("account not found")
	ErrProjectNotFound  = errors.New("project not found")
	ErrVersionConflict  = errors.New("version conflict")
	ErrQuotaExceeded    = errors.New("storage quota exceeded")
	ErrBackupNotFound   = errors.New("backup not found")
	ErrUnauthorized     = errors.New("unauthorized access to project")
)

type Account struct {
	ID         [32]byte  `json:"id"`
	PublicKey  []byte    `json:"public_key"`
	QuotaBytes int64     `json:"quota_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

type Project struct {
	ID        uuid.UUID  `json:"id"`
	AccountID [32]byte   `json:"account_id"`
	MetaBlob  []byte     `json:"meta_blob"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type Backup struct {
	ID        int64     `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Version   int       `json:"version"`
	Blob      []byte    `json:"blob"`
	SizeBytes int       `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type BackupHeader struct {
	Version   int       `json:"version"`
	SizeBytes int       `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type Store interface {
	RegisterAccount(ctx context.Context, accountID [32]byte, publicKey []byte) (*Account, error)
	GetAccount(ctx context.Context, accountID [32]byte) (*Account, error)
	HasAccount(ctx context.Context, accountID [32]byte) (bool, error)
	DeleteAccount(ctx context.Context, accountID [32]byte) error

	ListProjects(ctx context.Context, accountID [32]byte) ([]Project, error)
	UpsertProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte, metaBlob []byte) (*Project, error)
	GetProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte) (*Project, error)
	SoftDeleteProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte) error

	CreateBackup(ctx context.Context, projectID uuid.UUID, accountID [32]byte, expectedVersion int, blob []byte) (*Backup, error)
	ListBackups(ctx context.Context, projectID uuid.UUID, accountID [32]byte) ([]BackupHeader, error)
	GetBackup(ctx context.Context, projectID uuid.UUID, accountID [32]byte, version int) (*Backup, error)
}
