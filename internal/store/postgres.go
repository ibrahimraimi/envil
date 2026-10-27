package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) RegisterAccount(ctx context.Context, accountID [32]byte, publicKey []byte) (*Account, error) {
	query := `
		INSERT INTO accounts (id, public_key, quota_bytes)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET public_key = EXCLUDED.public_key
		RETURNING id, public_key, quota_bytes, created_at
	`
	var account Account
	var rawID []byte

	err := s.pool.QueryRow(ctx, query, accountID[:], publicKey, DEFAULT_QUOTA_BYTES).
		Scan(&rawID, &account.PublicKey, &account.QuotaBytes, &account.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("register account in db: %w", err)
	}

	copy(account.ID[:], rawID)
	return &account, nil
}

func (s *PostgresStore) GetAccount(ctx context.Context, accountID [32]byte) (*Account, error) {
	query := `SELECT id, public_key, quota_bytes, created_at FROM accounts WHERE id = $1`
	var account Account
	var rawID []byte

	err := s.pool.QueryRow(ctx, query, accountID[:]).
		Scan(&rawID, &account.PublicKey, &account.QuotaBytes, &account.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("query account: %w", err)
	}

	copy(account.ID[:], rawID)
	return &account, nil
}

func (s *PostgresStore) HasAccount(ctx context.Context, accountID [32]byte) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM accounts WHERE id = $1)`
	var exists bool
	if err := s.pool.QueryRow(ctx, query, accountID[:]).Scan(&exists); err != nil {
		return false, fmt.Errorf("check account existence: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) DeleteAccount(ctx context.Context, accountID [32]byte) error {
	query := `DELETE FROM accounts WHERE id = $1`
	tag, err := s.pool.Exec(ctx, query, accountID[:])
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *PostgresStore) ListProjects(ctx context.Context, accountID [32]byte) ([]Project, error) {
	query := `
		SELECT id, account_id, meta_blob, created_at, deleted_at
		FROM projects
		WHERE account_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, query, accountID[:])
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var project Project
		var rawAccountID []byte
		if err := rows.Scan(&project.ID, &rawAccountID, &project.MetaBlob, &project.CreatedAt, &project.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan project row: %w", err)
		}
		copy(project.AccountID[:], rawAccountID)
		projects = append(projects, project)
	}

	return projects, rows.Err()
}

func (s *PostgresStore) UpsertProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte, metaBlob []byte) (*Project, error) {
	query := `
		INSERT INTO projects (id, account_id, meta_blob)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET meta_blob = EXCLUDED.meta_blob, deleted_at = NULL
		WHERE projects.account_id = $2
		RETURNING id, account_id, meta_blob, created_at, deleted_at
	`
	var project Project
	var rawAccountID []byte

	err := s.pool.QueryRow(ctx, query, projectID, accountID[:], metaBlob).
		Scan(&project.ID, &rawAccountID, &project.MetaBlob, &project.CreatedAt, &project.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("upsert project: %w", err)
	}

	copy(project.AccountID[:], rawAccountID)
	return &project, nil
}

func (s *PostgresStore) GetProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte) (*Project, error) {
	query := `
		SELECT id, account_id, meta_blob, created_at, deleted_at
		FROM projects
		WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`
	var project Project
	var rawAccountID []byte

	err := s.pool.QueryRow(ctx, query, projectID, accountID[:]).
		Scan(&project.ID, &rawAccountID, &project.MetaBlob, &project.CreatedAt, &project.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProjectNotFound
		}
		return nil, fmt.Errorf("get project: %w", err)
	}

	copy(project.AccountID[:], rawAccountID)
	return &project, nil
}

func (s *PostgresStore) SoftDeleteProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte) error {
	query := `
		UPDATE projects
		SET deleted_at = now()
		WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL
	`
	tag, err := s.pool.Exec(ctx, query, projectID, accountID[:])
	if err != nil {
		return fmt.Errorf("soft delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (s *PostgresStore) CreateBackup(ctx context.Context, projectID uuid.UUID, accountID [32]byte, expectedVersion int, blob []byte) (*Backup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.verifyProjectOwnershipTx(ctx, tx, projectID, accountID); err != nil {
		return nil, err
	}

	if err := s.checkQuotaTx(ctx, tx, accountID, len(blob)); err != nil {
		return nil, err
	}

	latestVersion, err := s.getLatestVersionTx(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}

	if latestVersion != expectedVersion {
		return nil, ErrVersionConflict
	}

	nextVersion := latestVersion + 1
	backup, err := s.insertBackupTx(ctx, tx, projectID, nextVersion, blob)
	if err != nil {
		return nil, err
	}

	if err := s.pruneOlderBackupsTx(ctx, tx, projectID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit backup tx: %w", err)
	}

	return backup, nil
}

func (s *PostgresStore) verifyProjectOwnershipTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, accountID [32]byte) error {
	query := `SELECT id FROM projects WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL FOR UPDATE`
	var foundID uuid.UUID
	err := tx.QueryRow(ctx, query, projectID, accountID[:]).Scan(&foundID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("verify project ownership: %w", err)
	}
	return nil
}

func (s *PostgresStore) checkQuotaTx(ctx context.Context, tx pgx.Tx, accountID [32]byte, addedBytes int) error {
	quotaQuery := `SELECT quota_bytes FROM accounts WHERE id = $1`
	var quotaBytes int64
	if err := tx.QueryRow(ctx, quotaQuery, accountID[:]).Scan(&quotaBytes); err != nil {
		return fmt.Errorf("query account quota: %w", err)
	}

	usageQuery := `
		SELECT COALESCE(SUM(b.size_bytes), 0)
		FROM backups b
		JOIN projects p ON b.project_id = p.id
		WHERE p.account_id = $1
	`
	var currentUsage int64
	if err := tx.QueryRow(ctx, usageQuery, accountID[:]).Scan(&currentUsage); err != nil {
		return fmt.Errorf("query storage usage: %w", err)
	}

	if currentUsage+int64(addedBytes) > quotaBytes {
		return ErrQuotaExceeded
	}
	return nil
}

func (s *PostgresStore) getLatestVersionTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (int, error) {
	query := `SELECT COALESCE(MAX(version), 0) FROM backups WHERE project_id = $1`
	var version int
	if err := tx.QueryRow(ctx, query, projectID).Scan(&version); err != nil {
		return 0, fmt.Errorf("query latest version: %w", err)
	}
	return version, nil
}

func (s *PostgresStore) insertBackupTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, version int, blob []byte) (*Backup, error) {
	query := `
		INSERT INTO backups (project_id, version, blob, size_bytes)
		VALUES ($1, $2, $3, $4)
		RETURNING id, project_id, version, blob, size_bytes, created_at
	`
	var backup Backup
	err := tx.QueryRow(ctx, query, projectID, version, blob, len(blob)).
		Scan(&backup.ID, &backup.ProjectID, &backup.Version, &backup.Blob, &backup.SizeBytes, &backup.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert backup row: %w", err)
	}
	return &backup, nil
}

func (s *PostgresStore) pruneOlderBackupsTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	query := `
		DELETE FROM backups
		WHERE project_id = $1
		  AND version <= (
			SELECT MAX(version) - $2 FROM backups WHERE project_id = $1
		  )
	`
	if _, err := tx.Exec(ctx, query, projectID, DEFAULT_RETENTION); err != nil {
		return fmt.Errorf("prune old backups: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListBackups(ctx context.Context, projectID uuid.UUID, accountID [32]byte) ([]BackupHeader, error) {
	query := `
		SELECT b.version, b.size_bytes, b.created_at
		FROM backups b
		JOIN projects p ON b.project_id = p.id
		WHERE b.project_id = $1 AND p.account_id = $2 AND p.deleted_at IS NULL
		ORDER BY b.version DESC
	`
	rows, err := s.pool.Query(ctx, query, projectID, accountID[:])
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	defer rows.Close()

	headers := make([]BackupHeader, 0)
	for rows.Next() {
		var header BackupHeader
		if err := rows.Scan(&header.Version, &header.SizeBytes, &header.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan backup header: %w", err)
		}
		headers = append(headers, header)
	}

	return headers, rows.Err()
}

func (s *PostgresStore) GetBackup(ctx context.Context, projectID uuid.UUID, accountID [32]byte, version int) (*Backup, error) {
	query := `
		SELECT b.id, b.project_id, b.version, b.blob, b.size_bytes, b.created_at
		FROM backups b
		JOIN projects p ON b.project_id = p.id
		WHERE b.project_id = $1
		  AND p.account_id = $2
		  AND p.deleted_at IS NULL
		  AND (b.version = $3 OR ($3 = 0 AND b.version = (SELECT MAX(b2.version) FROM backups b2 WHERE b2.project_id = $1)))
	`
	var backup Backup
	err := s.pool.QueryRow(ctx, query, projectID, accountID[:], version).
		Scan(&backup.ID, &backup.ProjectID, &backup.Version, &backup.Blob, &backup.SizeBytes, &backup.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBackupNotFound
		}
		return nil, fmt.Errorf("get backup: %w", err)
	}
	return &backup, nil
}
