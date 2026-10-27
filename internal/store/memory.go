package store

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MemoryStore struct {
	lock        sync.RWMutex
	accounts    map[[32]byte]*Account
	projects    map[uuid.UUID]*Project
	backups     map[uuid.UUID][]*Backup
	nextBackupID int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts: make(map[[32]byte]*Account),
		projects: make(map[uuid.UUID]*Project),
		backups:  make(map[uuid.UUID][]*Backup),
	}
}

func (s *MemoryStore) RegisterAccount(ctx context.Context, accountID [32]byte, publicKey []byte) (*Account, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if existingAccount, found := s.accounts[accountID]; found {
		return existingAccount, nil
	}

	account := &Account{
		ID:         accountID,
		PublicKey:  bytes.Clone(publicKey),
		QuotaBytes: DEFAULT_QUOTA_BYTES,
		CreatedAt:  time.Now().UTC(),
	}
	s.accounts[accountID] = account
	return account, nil
}

func (s *MemoryStore) GetAccount(ctx context.Context, accountID [32]byte) (*Account, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	account, found := s.accounts[accountID]
	if !found {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

func (s *MemoryStore) HasAccount(ctx context.Context, accountID [32]byte) (bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, found := s.accounts[accountID]
	return found, nil
}

func (s *MemoryStore) DeleteAccount(ctx context.Context, accountID [32]byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, found := s.accounts[accountID]; !found {
		return ErrAccountNotFound
	}

	delete(s.accounts, accountID)

	for projectID, project := range s.projects {
		if project.AccountID == accountID {
			delete(s.projects, projectID)
			delete(s.backups, projectID)
		}
	}
	return nil
}

func (s *MemoryStore) ListProjects(ctx context.Context, accountID [32]byte) ([]Project, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	projectsList := make([]Project, 0)
	for _, project := range s.projects {
		if project.AccountID == accountID && project.DeletedAt == nil {
			projectsList = append(projectsList, *project)
		}
	}
	return projectsList, nil
}

func (s *MemoryStore) UpsertProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte, metaBlob []byte) (*Project, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, accountFound := s.accounts[accountID]; !accountFound {
		return nil, ErrAccountNotFound
	}

	existingProject, found := s.projects[projectID]
	if found {
		if existingProject.AccountID != accountID {
			return nil, ErrUnauthorized
		}
		existingProject.MetaBlob = bytes.Clone(metaBlob)
		existingProject.DeletedAt = nil
		return existingProject, nil
	}

	newProject := &Project{
		ID:        projectID,
		AccountID: accountID,
		MetaBlob:  bytes.Clone(metaBlob),
		CreatedAt: time.Now().UTC(),
	}
	s.projects[projectID] = newProject
	return newProject, nil
}

func (s *MemoryStore) GetProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte) (*Project, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	project, found := s.projects[projectID]
	if !found || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}
	if project.AccountID != accountID {
		return nil, ErrUnauthorized
	}
	return project, nil
}

func (s *MemoryStore) SoftDeleteProject(ctx context.Context, projectID uuid.UUID, accountID [32]byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	project, found := s.projects[projectID]
	if !found || project.DeletedAt != nil {
		return ErrProjectNotFound
	}
	if project.AccountID != accountID {
		return ErrUnauthorized
	}

	now := time.Now().UTC()
	project.DeletedAt = &now
	return nil
}

func (s *MemoryStore) CreateBackup(ctx context.Context, projectID uuid.UUID, accountID [32]byte, expectedVersion int, blob []byte) (*Backup, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	project, found := s.projects[projectID]
	if !found || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}
	if project.AccountID != accountID {
		return nil, ErrUnauthorized
	}

	account := s.accounts[accountID]
	if err := s.checkQuotaUnderLock(account, len(blob)); err != nil {
		return nil, err
	}

	projectBackups := s.backups[projectID]
	latestVersion := 0
	if len(projectBackups) > 0 {
		latestVersion = projectBackups[len(projectBackups)-1].Version
	}

	if expectedVersion != latestVersion {
		return nil, ErrVersionConflict
	}

	s.nextBackupID++
	newBackup := &Backup{
		ID:        s.nextBackupID,
		ProjectID: projectID,
		Version:   latestVersion + 1,
		Blob:      bytes.Clone(blob),
		SizeBytes: len(blob),
		CreatedAt: time.Now().UTC(),
	}

	projectBackups = append(projectBackups, newBackup)
	if len(projectBackups) > DEFAULT_RETENTION {
		projectBackups = projectBackups[len(projectBackups)-DEFAULT_RETENTION:]
	}
	s.backups[projectID] = projectBackups

	return newBackup, nil
}

func (s *MemoryStore) checkQuotaUnderLock(account *Account, addedBytes int) error {
	var totalBytes int64
	for projectID, project := range s.projects {
		if project.AccountID != account.ID {
			continue
		}
		for _, backup := range s.backups[projectID] {
			totalBytes += int64(backup.SizeBytes)
		}
	}

	if totalBytes+int64(addedBytes) > account.QuotaBytes {
		return ErrQuotaExceeded
	}
	return nil
}

func (s *MemoryStore) ListBackups(ctx context.Context, projectID uuid.UUID, accountID [32]byte) ([]BackupHeader, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	project, found := s.projects[projectID]
	if !found || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}
	if project.AccountID != accountID {
		return nil, ErrUnauthorized
	}

	backupsList := s.backups[projectID]
	headers := make([]BackupHeader, len(backupsList))
	for index, backup := range backupsList {
		// Store in reverse so latest version is at index 0 (matching ORDER BY version DESC)
		headers[len(backupsList)-1-index] = BackupHeader{
			Version:   backup.Version,
			SizeBytes: backup.SizeBytes,
			CreatedAt: backup.CreatedAt,
		}
	}
	return headers, nil
}

func (s *MemoryStore) GetBackup(ctx context.Context, projectID uuid.UUID, accountID [32]byte, version int) (*Backup, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	project, found := s.projects[projectID]
	if !found || project.DeletedAt != nil {
		return nil, ErrProjectNotFound
	}
	if project.AccountID != accountID {
		return nil, ErrUnauthorized
	}

	backupsList := s.backups[projectID]
	if len(backupsList) == 0 {
		return nil, ErrBackupNotFound
	}

	if version == 0 {
		return backupsList[len(backupsList)-1], nil
	}

	for _, backup := range backupsList {
		if backup.Version == version {
			return backup, nil
		}
	}
	return nil, ErrBackupNotFound
}
